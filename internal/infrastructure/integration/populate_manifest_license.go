package integration

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// enrichLicenseFromManifest is the third-tier license fallback: when deps.dev
// (Project + Version) and GitHub `licenseInfo` have both failed to yield a
// canonical SPDX, it consults the package's own ecosystem manifest. This PR
// wires Maven only; NuGet `.nuspec` and PyPI metadata fallbacks land in
// follow-up PRs and will extend this dispatcher with a per-ecosystem switch.
//
// DDD Layer: Infrastructure (parallel best-effort enrichment, mirroring the
// WaitGroup-only fan-out used by enrichPyPISummary). Concurrency is bounded
// by a semaphore (maxManifestFetchConcurrency). Within a single batch, the
// jobs map deduplicates by (groupId, artifactId, version) so identical
// coordinates issue exactly one POM lookup even when multiple analyses share
// them.
//
// Override rules:
//   - Skip an analysis entirely when ProjectLicense is already canonical SPDX
//     AND RequestedVersionLicenses is non-empty and every entry is canonical
//     SPDX (cheap pre-check before any HTTP).
//   - For each manifest license, write to RequestedVersionLicenses when the slice
//     is empty or composed entirely of non-SPDX entries.
//   - Promote the first SPDX manifest license (in <licenses> document order) to
//     ProjectLicense when the current ProjectLicense is zero or non-standard.
//   - Never overwrite a current canonical SPDX in either field; log a WARN with
//     "license_disagreement" when the manifest disagrees so we can audit later.
//
// Best-effort: per-coordinate fetch failures are logged at WARN as
// "license_manifest_fetch_failed" and the analysis is left untouched. HTTP 429
// responses surface as "license_manifest_rate_limited" (distinct event name) so
// production telemetry can monitor rate-limit pressure separately.
func (s *IntegrationService) enrichLicenseFromManifest(ctx context.Context, analyses map[string]*domain.Analysis) {
	if s.mavenClient == nil || len(analyses) == 0 {
		return
	}

	parser := purl.NewParser()
	type mavenKey struct{ group, artifact, version string }
	jobs := make(map[mavenKey][]*domain.Analysis)
	for _, a := range analyses {
		if !needsManifestLicense(a) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(a.Package.Ecosystem), "maven") {
			continue
		}
		parsed, err := parser.Parse(a.Package.PURL)
		if err != nil {
			slog.Debug("license_manifest_purl_parse_failed", "purl", a.Package.PURL, "error", err)
			continue
		}
		group := strings.TrimSpace(parsed.Namespace())
		artifact := strings.TrimSpace(parsed.Name())
		version := strings.TrimSpace(parsed.Version())
		if version == "" {
			version = strings.TrimSpace(resolvedVersion(a))
		}
		if group == "" || artifact == "" || version == "" {
			continue
		}
		k := mavenKey{group: group, artifact: artifact, version: version}
		jobs[k] = append(jobs[k], a)
	}
	if len(jobs) == 0 {
		return
	}

	const maxManifestFetchConcurrency = 10
	sem := make(chan struct{}, maxManifestFetchConcurrency)

	var wg sync.WaitGroup
dispatchLoop:
	for k, targets := range jobs {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break dispatchLoop
		}

		wg.Add(1)
		go func(k mavenKey, targets []*domain.Analysis) {
			defer wg.Done()
			defer func() { <-sem }()
			lics, found, err := s.mavenClient.FetchLicenses(ctx, k.group, k.artifact, k.version)
			if err != nil {
				event := "license_manifest_fetch_failed"
				if common.IsRateLimitError(err) {
					event = "license_manifest_rate_limited"
				}
				slog.Warn(event,
					"ecosystem", "maven",
					"group_id", k.group,
					"artifact_id", k.artifact,
					"version", k.version,
					"error", err)
				return
			}
			if !found || len(lics) == 0 {
				return
			}
			for _, a := range targets {
				applyManifestLicenses(a, lics)
			}
		}(k, targets)
	}
	wg.Wait()
}

// needsManifestLicense returns true when an analysis is a viable target for
// manifest-level license fallback based on its current state.
//
// This predicate identifies analyses that are eligible for a manifest fetch to
// fill missing or non-standard license data, helping avoid obviously wasted
// HTTP requests. A true result does not guarantee that applyManifestLicenses
// will write anything, because actual writes also depend on what SPDX licenses
// the fetched manifest yields.
//
// Specifically: ProjectLicense is zero or non-standard, OR the version-license
// slice is empty or composed entirely of non-SPDX entries.
func needsManifestLicense(a *domain.Analysis) bool {
	if a == nil || a.Package == nil || a.Package.PURL == "" {
		return false
	}
	if a.ProjectLicense.IsZero() || a.ProjectLicense.IsNonStandard() {
		return true
	}
	if len(a.RequestedVersionLicenses) == 0 || allVersionLicensesNonSPDX(a.RequestedVersionLicenses) {
		return true
	}
	return false
}

// applyManifestLicenses merges externally-derived licenses into the analysis,
// applying the override rules documented on enrichLicenseFromManifest. Used
// by both the manifest tier (Maven POM) and the ClearlyDefined.io tier; the
// override matrix is identical across sources so a single helper is reused.
//
// When the source reports multiple SPDX entries (multi-licensed POMs or
// SPDX-expression operands from CD), the first SPDX entry in input order is
// promoted to ProjectLicense. The full list — including any subsequent SPDX
// entries — is written to RequestedVersionLicenses when that slice is empty
// or entirely non-SPDX. Order is treated as authoritative: Maven publishers
// list the primary license first by convention; SPDX expressions place the
// preferred license first in the operand sequence.
func applyManifestLicenses(a *domain.Analysis, lics []domain.ResolvedLicense) {
	if a == nil || len(lics) == 0 {
		return
	}

	var bestSPDX *domain.ResolvedLicense
	for i := range lics {
		if lics[i].IsSPDX {
			bestSPDX = &lics[i]
			break
		}
	}

	// ProjectLicense: replace when current is zero or non-standard. Disagreement
	// with an existing canonical SPDX is logged but not auto-resolved.
	if bestSPDX != nil {
		if a.ProjectLicense.IsZero() || a.ProjectLicense.IsNonStandard() {
			a.ProjectLicense = *bestSPDX
		} else if a.ProjectLicense.IsSPDX && !strings.EqualFold(a.ProjectLicense.Identifier, bestSPDX.Identifier) {
			slog.Warn("license_disagreement",
				"existing_source", a.ProjectLicense.Source,
				"existing", a.ProjectLicense.Identifier,
				"manifest_source", bestSPDX.Source,
				"manifest", bestSPDX.Identifier,
				"purl", a.Package.PURL)
		}
	} else if a.ProjectLicense.IsZero() {
		// Manifest had no SPDX but did report something — record the first non-standard.
		a.ProjectLicense = lics[0]
	}

	// RequestedVersionLicenses: replace when empty, or when all existing entries
	// are non-SPDX AND the manifest provides at least one SPDX license (replacing
	// non-standard with non-standard is a no-op per the override matrix).
	if len(a.RequestedVersionLicenses) == 0 {
		a.RequestedVersionLicenses = append([]domain.ResolvedLicense(nil), lics...)
	} else if allVersionLicensesNonSPDX(a.RequestedVersionLicenses) && bestSPDX != nil {
		a.RequestedVersionLicenses = append([]domain.ResolvedLicense(nil), lics...)
	}
}
