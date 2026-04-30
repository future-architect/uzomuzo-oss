package integration

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	commonlinks "github.com/future-architect/uzomuzo-oss/internal/common/links"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/licenses"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/links"
)

// populateAnalysisFromBatchResult populates a domain.Analysis from a deps.dev BatchResult.
// Extracted for modularity & reuse across PURL and GitHub flows.
func (s *IntegrationService) populateAnalysisFromBatchResult(ctx context.Context, analysis *domain.Analysis, batchResult *depsdev.BatchResult) {
	if batchResult == nil {
		return
	}
	if batchResult.Error != nil && analysis.Error == nil {
		analysis.Error = common.NewResourceNotFoundError(*batchResult.Error).WithContext("purl", analysis.EffectivePURL)
	}

	if analysis.PackageLinks == nil {
		analysis.PackageLinks = &domain.PackageLinks{}
	}
	if analysis.PackageLinks.RegistryURL == "" && analysis.Package != nil && analysis.Package.PURL != "" {
		parser := purl.NewParser()
		raw := analysis.Package.PURL
		if u, err := url.PathUnescape(raw); err == nil && u != "" {
			raw = u
		}
		if parsed, err := parser.Parse(raw); err == nil {
			pkgName := parsed.GetPackageName()
			group := parsed.Namespace()
			finalName := pkgName
			if group != "" {
				switch strings.ToLower(strings.TrimSpace(analysis.Package.Ecosystem)) {
				case "maven":
					finalName = commonlinks.JoinMavenName(group, pkgName)
				case "npm":
					finalName = commonlinks.JoinNpmName(group, pkgName)
				case "packagist", "composer":
					finalName = group + "/" + pkgName
				}
			}
			analysis.PackageLinks.RegistryURL = links.BuildPackageRegistryURL(analysis.Package.Ecosystem, finalName)
		}
	}

	if analysis.RepoURL == "" && batchResult.RepoURL != "" {
		analysis.RepoURL = batchResult.RepoURL
		if analysis.Repository == nil {
			analysis.Repository = &domain.Repository{}
		}
		analysis.Repository.URL = analysis.RepoURL
	}

	if batchResult.Package != nil && len(batchResult.Package.Versions) > 0 && analysis.RepoURL == "" {
		// Robust repo URL derivation:
		// Typical deps.dev behavior: when the input PURL already contains an explicit version (@x.y.z),
		// Package.Versions is a single-element slice containing only that version. Previous implementation
		// unconditionally used index 0. To avoid relying on that implicit contract (and future-proof for
		// potential multi-entry responses), we attempt to locate the exact requested version if we know it.
		// If we cannot find a precise match we fall back to the first element (best-effort, preserves legacy behavior).
		var selected *depsdev.Version
		if analysis.Package != nil && strings.TrimSpace(analysis.Package.Version) != "" {
			targetVer := strings.TrimSpace(analysis.Package.Version)
			for i := range batchResult.Package.Versions {
				if batchResult.Package.Versions[i].VersionKey.Version == targetVer {
					selected = &batchResult.Package.Versions[i]
					break
				}
			}
		}
		if selected == nil { // fallback to original 0-index behavior
			selected = &batchResult.Package.Versions[0]
		}
		if repoURL := depsdev.ExtractRepositoryURLFromLinks(selected.Links); repoURL != "" {
			analysis.RepoURL = repoURL
			if analysis.Repository == nil {
				analysis.Repository = &domain.Repository{}
			}
			analysis.Repository.URL = repoURL
		}
	}

	if batchResult.Project != nil {
		s.populateProjectScorecard(analysis, batchResult)
	} else {
		slog.Debug("no_project_data", "purl", batchResult.PURL)
	}
	s.populateReleaseInfo(analysis, batchResult)
	// Populate license information after release info (needs RequestedVersion PURL)
	s.populateLicenses(ctx, analysis, batchResult)
}

// populateLicenses enriches Analysis with project-level and requested-version
// license data using SPDX expression strings.
//
// Data model: see ResolvedLicense / Analysis godoc and
// docs/adr/0019-license-expression-of-truth.md. The key invariants:
//   - Expression is canonical SPDX or "" or "NOASSERTION" (never half-normalized)
//   - Raw preserves the upstream string for single-source inputs and may be a
//     normalized/concatenated audit string when multiple upstream entries are merged
//   - Multiple upstream entries collapse to one OR-joined expression (not a slice)
func (s *IntegrationService) populateLicenses(ctx context.Context, analysis *domain.Analysis, batchResult *depsdev.BatchResult) {
	if analysis == nil || batchResult == nil {
		return
	}
	// Project license (deps.dev project batch)
	if analysis.ProjectLicense.IsZero() && batchResult.Project != nil && strings.TrimSpace(batchResult.Project.License) != "" {
		analysis.ProjectLicense = buildProjectLicense(batchResult.Project.License)
	}
	// Requested version license collection
	if analysis.ReleaseInfo == nil || analysis.ReleaseInfo.RequestedVersion == nil || analysis.ReleaseInfo.RequestedVersion.Version == "" {
		return
	}
	if !analysis.RequestedVersionLicense.IsZero() { // already populated
		return
	}
	requestedVersion := analysis.ReleaseInfo.RequestedVersion.Version
	// 1. Reuse batchResult.Package when it aligns with requested version.
	var versionLicense domain.ResolvedLicense
	if batchResult.Package != nil && len(batchResult.Package.Versions) > 0 {
		for i := range batchResult.Package.Versions {
			v := batchResult.Package.Versions[i]
			if v.VersionKey.Version == requestedVersion {
				versionLicense = buildVersionLicenseFromDepsDev(&v)
				break
			}
		}
	}
	// 2. Targeted fetch via deps.dev client when batch did not carry the version.
	if versionLicense.IsZero() && s.depsdevClient != nil && analysis.Package != nil && analysis.Package.Version != "" {
		versioned := analysis.EffectivePURL
		if versioned == "" {
			versioned = analysis.Package.PURL
		}
		if fetched, err := s.depsdevClient.GetPackageVersionLicenses(ctx, versioned); err == nil && len(fetched) > 0 {
			versionLicense = buildVersionLicenseFromRawList(fetched)
		} else if err != nil {
			slog.Debug("requested_version_license_fetch_failed", "purl", versioned, "error", err)
		}
	}
	// 3. Fallback to project license expression when version-specific data is empty.
	if versionLicense.IsZero() && analysis.ProjectLicense.IsUsableSPDX() {
		versionLicense = domain.ResolvedLicense{
			Expression: analysis.ProjectLicense.Expression,
			Raw:        analysis.ProjectLicense.Raw,
			Source:     domain.LicenseSourceProjectFallback,
		}
	}
	analysis.RequestedVersionLicense = versionLicense

	// 4. Replace a non-SPDX (raw-only) version expression with project SPDX when available.
	if analysis.ProjectLicense.IsUsableSPDX() && analysis.RequestedVersionLicense.Expression == "" && !analysis.RequestedVersionLicense.IsZero() {
		analysis.RequestedVersionLicense = domain.ResolvedLicense{
			Expression: analysis.ProjectLicense.Expression,
			Raw:        analysis.ProjectLicense.Raw,
			Source:     domain.LicenseSourceProjectFallback,
		}
	}

	// 5. Promote a single-leaf version expression to project-level when eligible.
	promoteProjectLicenseFromVersion(analysis)
}

// buildProjectLicense classifies a deps.dev Project.License string into a
// ResolvedLicense. NOASSERTION is preserved as the SPDX sentinel; non-SPDX
// values are recorded with Expression="" and Source=...NonStandard.
func buildProjectLicense(raw string) domain.ResolvedLicense {
	expr := licenses.NormalizeExpression(raw)
	if expr == "" {
		return domain.ResolvedLicense{Expression: "", Raw: raw, Source: domain.LicenseSourceDepsDevProjectNonStandard}
	}
	return domain.ResolvedLicense{Expression: expr, Raw: raw, Source: domain.LicenseSourceDepsDevProjectSPDX}
}

// buildVersionLicenseFromDepsDev builds a ResolvedLicense for the requested
// version from a deps.dev Version, OR-joining all upstream license entries
// into a single SPDX expression. Priority order: LicenseDetails[].Spdx →
// fallback to Licenses[]. The function never returns a slice — see ADR-0019.
func buildVersionLicenseFromDepsDev(v *depsdev.Version) domain.ResolvedLicense {
	if v == nil {
		return domain.ResolvedLicense{}
	}
	rawDetails := make([]string, 0, len(v.LicenseDetails))
	for _, d := range v.LicenseDetails {
		if s := strings.TrimSpace(d.Spdx); s != "" {
			rawDetails = append(rawDetails, s)
		}
	}
	if len(rawDetails) > 0 {
		return buildVersionLicenseFromRawList(rawDetails)
	}
	if len(v.Licenses) == 0 {
		return domain.ResolvedLicense{}
	}
	return buildVersionLicenseFromRawList(v.Licenses)
}

// buildVersionLicenseFromRawList constructs a ResolvedLicense for the
// requested version from any list of upstream license strings, by OR-joining
// them into one canonical SPDX expression. When normalization recognizes at
// least one entry, the result carries Source=DepsDevVersionSPDX (per the
// "SPDX entry survival wins" rule in ADR-0019). When every entry fails
// normalization, the result carries Expression="" and Source=DepsDevVersionRaw,
// with Raw holding the joined upstream concatenation for audit.
func buildVersionLicenseFromRawList(rawList []string) domain.ResolvedLicense {
	rawConcat := strings.Join(filterNonEmpty(rawList), " "+licenseORSeparator+" ")
	if rawConcat == "" {
		return domain.ResolvedLicense{}
	}
	if expr := licenses.JoinExpressions(rawList); expr != "" {
		return domain.ResolvedLicense{
			Expression: expr,
			Raw:        rawConcat,
			Source:     domain.LicenseSourceDepsDevVersionSPDX,
		}
	}
	return domain.ResolvedLicense{
		Expression: "",
		Raw:        rawConcat,
		Source:     domain.LicenseSourceDepsDevVersionRaw,
	}
}

// licenseORSeparator is the canonical operator used to concatenate upstream
// license entries into the Raw audit string. Mirrors the SPDX OR operator so
// that Expression and Raw are visually congruent for the canonical case.
const licenseORSeparator = "OR"

func filterNonEmpty(s []string) []string {
	out := make([]string, 0, len(s))
	for _, e := range s {
		if t := strings.TrimSpace(e); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// promoteProjectLicenseFromVersion elevates a single-leaf version expression
// to project-level when ProjectLicense is not a usable SPDX expression (zero,
// non-standard, or NOASSERTION). Compound version expressions
// ("MIT OR Apache-2.0") are NOT promoted: a project-level claim must be
// unambiguous, and a project's own LICENSE file may pick a single leaf from
// the dual-license set in ways the upstream package metadata does not capture.
//
// Idempotent: safe to call multiple times; exits early once ProjectLicense is
// set to a usable SPDX expression.
func promoteProjectLicenseFromVersion(a *domain.Analysis) {
	if a == nil {
		return
	}
	if a.ProjectLicense.IsUsableSPDX() {
		return // already set to a usable canonical SPDX expression
	}
	expr := a.RequestedVersionLicense.Expression
	if expr == "" || expr == "NOASSERTION" {
		return
	}
	parsed := licenses.ParseExpression(expr)
	if parsed.Root == nil || parsed.Root.License == nil {
		return // compound — not a safe project-level claim
	}
	if parsed.Root.License.Identifier == "" {
		return // defensive: should not happen for canonical Expression
	}
	a.ProjectLicense = domain.ResolvedLicense{
		Expression: expr,
		Raw:        a.RequestedVersionLicense.Raw,
		Source:     domain.LicenseSourceDerivedFromVersion,
	}
}
