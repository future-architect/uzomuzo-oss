package integration

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/clearlydefined"
)

// enrichLicenseFromClearlyDefined is the fourth-tier license fallback,
// invoked after enrichLicenseFromManifest has run the ecosystem-native
// manifest tier (Maven POM today; NuGet / PyPI in follow-ups). It consults
// ClearlyDefined.io's curated license database for analyses that still
// have a missing or non-standard ProjectLicense, or whose
// RequestedVersionLicenses are entirely non-SPDX.
//
// Why this is its own dispatcher (and not folded into enrichLicenseFromManifest):
//   - The two sources have different operational characteristics (CD is a
//     curation database, not the package's own manifest) — keeping the
//     telemetry and event names separate lets operators triage hit/failure
//     rates per tier.
//   - CD covers many ecosystems (maven, npm, nuget, pypi, gem, cargo,
//     composer); the manifest tier is currently maven-only.
//   - Running this strictly *after* the manifest tier ensures CD never
//     overrides a deterministic upstream answer; CD is the last-resort
//     safety net per ADR-0018 ("Option C" in issue #354's design).
//
// Concurrency mirrors the manifest tier: a per-call semaphore caps in-flight
// HTTP requests, and the jobs map deduplicates by coordinate so identical
// (ecosystem, namespace, name, version) tuples issue exactly one CD call
// even when multiple analyses share them.
//
// Override behavior is identical to the manifest tier (applyManifestLicenses):
// canonical SPDX is never overwritten, *-nonstandard slots are filled, and
// the first SPDX leaf in CD's `licensed.declared` is promoted to ProjectLicense.
func (s *IntegrationService) enrichLicenseFromClearlyDefined(ctx context.Context, analyses map[string]*domain.Analysis) {
	if s.cdClient == nil || len(analyses) == 0 {
		return
	}

	parser := purl.NewParser()
	type cdKey struct{ ecosystem, namespace, name, version string }
	jobs := make(map[cdKey][]*domain.Analysis)
	for _, a := range analyses {
		if !needsManifestLicense(a) {
			continue
		}
		ecosystem := strings.ToLower(strings.TrimSpace(a.Package.Ecosystem))
		// Positive-list gate: skip ecosystems CD doesn't cover (Go modules,
		// Github Actions, generic, etc.) so we don't spawn goroutines that
		// would short-circuit inside FetchLicenses. Keeps the
		// license_clearlydefined_miss telemetry meaningful — a miss event
		// always represents a coordinate CD was actually queried for.
		if !clearlydefined.SupportsEcosystem(ecosystem) {
			continue
		}
		parsed, err := parser.Parse(a.Package.PURL)
		if err != nil {
			slog.Debug("license_clearlydefined_purl_parse_failed", "purl", a.Package.PURL, "error", err)
			continue
		}
		namespace := strings.TrimSpace(parsed.Namespace())
		name := strings.TrimSpace(parsed.Name())
		version := strings.TrimSpace(parsed.Version())
		if version == "" {
			version = strings.TrimSpace(resolvedVersion(a))
		}
		if name == "" || version == "" {
			continue
		}
		k := cdKey{ecosystem: ecosystem, namespace: namespace, name: name, version: version}
		jobs[k] = append(jobs[k], a)
	}
	if len(jobs) == 0 {
		return
	}

	const maxClearlyDefinedConcurrency = 10
	sem := make(chan struct{}, maxClearlyDefinedConcurrency)

	var wg sync.WaitGroup
dispatchLoop:
	for k, targets := range jobs {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break dispatchLoop
		}

		wg.Add(1)
		go func(k cdKey, targets []*domain.Analysis) {
			defer wg.Done()
			defer func() { <-sem }()
			lics, found, err := s.cdClient.FetchLicenses(ctx, k.ecosystem, k.namespace, k.name, k.version)
			if err != nil {
				event := "license_clearlydefined_fetch_failed"
				if common.IsRateLimitError(err) {
					event = "license_clearlydefined_rate_limited"
				}
				slog.Warn(event,
					"ecosystem", k.ecosystem,
					"namespace", k.namespace,
					"name", k.name,
					"version", k.version,
					"error", err)
				return
			}
			if !found || len(lics) == 0 {
				slog.Debug("license_clearlydefined_miss",
					"ecosystem", k.ecosystem,
					"namespace", k.namespace,
					"name", k.name,
					"version", k.version)
				return
			}
			var wrote bool
			for _, a := range targets {
				if applyManifestLicenses(a, lics) {
					wrote = true
				}
			}
			if wrote {
				slog.Debug("license_clearlydefined_hit",
					"ecosystem", k.ecosystem,
					"namespace", k.namespace,
					"name", k.name,
					"version", k.version,
					"licenses_count", len(lics))
			} else {
				slog.Debug("license_clearlydefined_no_change",
					"ecosystem", k.ecosystem,
					"namespace", k.namespace,
					"name", k.name,
					"version", k.version,
					"licenses_count", len(lics))
			}
		}(k, targets)
	}
	wg.Wait()
}
