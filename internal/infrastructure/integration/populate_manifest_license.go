package integration

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// enrichLicenseFromManifest is the third-tier license fallback: when deps.dev
// (Project + Version) and GitHub `licenseInfo` have both failed to yield a
// canonical SPDX, it consults the package's own ecosystem manifest (Maven POM
// in this PR; .nuspec / PyPI metadata wired in follow-up PRs).
//
// DDD Layer: Infrastructure (parallel best-effort enrichment, mirroring the
// WaitGroup-only fan-out used by enrichPyPISummary). Concurrency is unbounded
// (one goroutine per unique manifest coordinate); deduplication within a single
// scan comes from the jobs map that groups analyses by Maven coordinate.
//
// Override rules:
//   - Skip an analysis entirely when ProjectLicense is already canonical SPDX
//     AND every RequestedVersionLicenses entry is canonical SPDX (cheap pre-check
//     before any HTTP).
//   - For each manifest license, write to RequestedVersionLicenses when the slice
//     is empty or composed entirely of non-SPDX entries.
//   - Promote the first SPDX manifest license to ProjectLicense when the current
//     ProjectLicense is zero or non-standard.
//   - Never overwrite a current canonical SPDX in either field; log a WARN with
//     "license_disagreement" when the manifest disagrees so we can audit later.
//
// Best-effort: per-coordinate fetch failures are logged at WARN level
// ("license_manifest_fetch_failed") and the analysis is left untouched.
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
			continue
		}
		group := strings.TrimSpace(parsed.Namespace())
		artifact := strings.TrimSpace(parsed.Name())
		version := strings.TrimSpace(parsed.Version())
		if group == "" || artifact == "" || version == "" {
			continue
		}
		k := mavenKey{group: group, artifact: artifact, version: version}
		jobs[k] = append(jobs[k], a)
	}
	if len(jobs) == 0 {
		return
	}

	var wg sync.WaitGroup
	for k, targets := range jobs {
		wg.Add(1)
		go func(k mavenKey, targets []*domain.Analysis) {
			defer wg.Done()
			lics, found, err := s.mavenClient.FetchLicenses(ctx, k.group, k.artifact, k.version)
			if err != nil {
				slog.Warn("license_manifest_fetch_failed",
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
// manifest-level license fallback. This predicate is aligned with
// applyManifestLicenses: it returns true only when the apply function would
// actually write something, avoiding wasted HTTP fetches.
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

// applyManifestLicenses merges manifest-derived licenses into the analysis,
// applying the override rules documented on enrichLicenseFromManifest.
func applyManifestLicenses(a *domain.Analysis, lics []domain.ResolvedLicense) {
	if a == nil || len(lics) == 0 {
		return
	}

	// Pick the best entry: prefer SPDX over non-standard, taking the first match
	// in either case. Order in the manifest is treated as authoritative.
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
				"purl", purlForLog(a))
		}
	} else if a.ProjectLicense.IsZero() {
		// Manifest had no SPDX but did report something — record the first non-standard.
		a.ProjectLicense = lics[0]
	}

	// RequestedVersionLicenses: replace when empty or all non-SPDX, otherwise leave
	// the existing canonical-SPDX slice intact (manifest cannot beat clean upstream
	// SPDX at version level).
	if len(a.RequestedVersionLicenses) == 0 || allVersionLicensesNonSPDX(a.RequestedVersionLicenses) {
		a.RequestedVersionLicenses = append([]domain.ResolvedLicense(nil), lics...)
	}
}

// purlForLog returns a best-effort PURL string for log evidence. Falls back to
// EffectivePURL or OriginalPURL when Package.PURL is empty.
func purlForLog(a *domain.Analysis) string {
	if a == nil {
		return ""
	}
	if a.Package != nil && a.Package.PURL != "" {
		return a.Package.PURL
	}
	if a.EffectivePURL != "" {
		return a.EffectivePURL
	}
	return a.OriginalPURL
}
