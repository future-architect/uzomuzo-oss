package integration

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/licenses"
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
// Override rules (per ADR-0019):
//   - Skip an analysis entirely when ProjectLicense AND RequestedVersionLicense
//     are both already canonical SPDX expressions (cheap pre-check before any
//     HTTP).
//   - Promote the first SPDX manifest license (in <licenses> document order) to
//     ProjectLicense when the current ProjectLicense is zero or non-standard.
//   - OR-join all SPDX manifest licenses into a single expression and write to
//     RequestedVersionLicense when the current value is zero or non-standard.
//   - Never overwrite a current canonical SPDX in either field; log a WARN with
//     "license_disagreement" when the manifest's primary disagrees with the
//     existing project license so we can audit later.
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
// will write anything, because actual writes also depend on what SPDX
// expressions the fetched manifest yields.
//
// Specifically: ProjectLicense is not a usable SPDX expression (zero,
// non-standard, or NOASSERTION), OR RequestedVersionLicense is not usable.
func needsManifestLicense(a *domain.Analysis) bool {
	if a == nil || a.Package == nil || a.Package.PURL == "" {
		return false
	}
	if !a.ProjectLicense.IsUsableSPDX() {
		return true
	}
	if !a.RequestedVersionLicense.IsUsableSPDX() {
		return true
	}
	return false
}

// applyManifestLicenses merges externally-derived licenses into the analysis,
// applying the override rules documented on enrichLicenseFromManifest. Used
// by both the manifest tier (Maven POM) and the ClearlyDefined.io tier; the
// override matrix is identical across sources so a single helper is reused.
//
// Returns true when at least one field on the analysis was written, false when
// the incoming licenses had no effect (e.g., all slots already occupied by
// canonical SPDX, or the incoming set is entirely non-SPDX and cannot improve
// the existing state). The bool is used by the ClearlyDefined.io tier to log
// per-coordinate telemetry separately from the Maven POM tier.
//
// When the source reports multiple SPDX entries (multi-licensed POMs or
// SPDX-expression operands from CD), the first SPDX entry in input order is
// promoted to ProjectLicense for deterministic selection. All SPDX entries
// are OR-joined into a single canonical expression and written to
// RequestedVersionLicense when the current value is zero or non-standard.
// Input order may reflect publisher convention for some manifest sources
// (e.g. Maven POMs list the primary license first), but SPDX expressions
// themselves do not imply a preferred license by operand order.
func applyManifestLicenses(a *domain.Analysis, lics []domain.ResolvedLicense) bool {
	if a == nil || len(lics) == 0 {
		return false
	}

	// Collect SPDX expressions in document order for OR-joining at the version level.
	spdxExprs := make([]string, 0, len(lics))
	rawParts := make([]string, 0, len(lics))
	var firstSPDX *domain.ResolvedLicense
	for i := range lics {
		if lics[i].IsUsableSPDX() {
			spdxExprs = append(spdxExprs, lics[i].Expression)
			if firstSPDX == nil {
				firstSPDX = &lics[i]
			}
		}
		if lics[i].Raw != "" {
			rawParts = append(rawParts, lics[i].Raw)
		}
	}

	wrote := false

	// ProjectLicense: replace when current is zero or non-standard. Disagreement
	// with an existing canonical SPDX is logged but not auto-resolved.
	if firstSPDX != nil {
		if !a.ProjectLicense.IsUsableSPDX() {
			a.ProjectLicense = *firstSPDX
			wrote = true
		} else if a.ProjectLicense.IsUsableSPDX() && !strings.EqualFold(a.ProjectLicense.Expression, firstSPDX.Expression) {
			slog.Warn("license_disagreement",
				"existing_source", a.ProjectLicense.Source,
				"existing", a.ProjectLicense.Expression,
				"incoming_source", firstSPDX.Source,
				"incoming", firstSPDX.Expression,
				"purl", a.Package.PURL)
		}
	} else if a.ProjectLicense.IsZero() {
		// Manifest had no SPDX but did report something — record the first non-standard.
		a.ProjectLicense = lics[0]
		wrote = true
	}

	// RequestedVersionLicense: replace when zero or non-standard. OR-join all
	// SPDX manifest entries into one canonical expression. Raw preserves the
	// per-entry concatenation for audit. The Source uses the same constant as
	// the first SPDX entry so callers can attribute provenance (Maven POM vs
	// ClearlyDefined.io).
	if !a.RequestedVersionLicense.IsUsableSPDX() {
		if len(spdxExprs) > 0 && firstSPDX != nil {
			joined := licenses.JoinExpressions(spdxExprs)
			rawConcat := strings.Join(rawParts, " "+licenseORSeparator+" ")
			a.RequestedVersionLicense = domain.ResolvedLicense{
				Expression: joined,
				Raw:        rawConcat,
				Source:     firstSPDX.Source,
			}
			wrote = true
		} else if a.RequestedVersionLicense.IsZero() && len(rawParts) > 0 {
			// All manifest entries were non-standard — preserve raw concatenation.
			a.RequestedVersionLicense = domain.ResolvedLicense{
				Expression: "",
				Raw:        strings.Join(rawParts, " "+licenseORSeparator+" "),
				Source:     lics[0].Source,
			}
			wrote = true
		}
	}

	return wrote
}
