package integration

import (
	"context"
	"log/slog"
	"net/url"
	"sort"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/links"
)

// populateAnalysisFromBatchResult populates a domain.Analysis from a deps.dev BatchResult.
// Extracted for modularity & reuse across PURL and GitHub flows.
func (s *IntegrationService) populateAnalysisFromBatchResult(analysis *domain.Analysis, batchResult *depsdev.BatchResult) {
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
					finalName = group + ":" + pkgName
				case "packagist", "composer", "npm":
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
	s.populateLicenses(analysis, batchResult)
}

// populateLicenses enriches Analysis with project-level and requested-version license data.
// Requirements:
//   - ProjectLicense (single string) prefers deps.dev Project.License; (GitHub fallback TBD)
//   - RequestedVersionLicenses ([]string) collected only for explicitly requested version.
//     If version-specific licenses absent, fallback to ProjectLicense (single-element slice).
func (s *IntegrationService) populateLicenses(analysis *domain.Analysis, batchResult *depsdev.BatchResult) {
	if analysis == nil || batchResult == nil {
		return
	}
	// Project license (deps.dev project batch)
	// Policy: if deps.dev returns a non-SPDX placeholder we record Source=deps.dev-nonstandard with empty Identifier.
	if analysis.ProjectLicense.Identifier == "" && batchResult.Project != nil && strings.TrimSpace(batchResult.Project.License) != "" {
		if norm, isSPDX := domain.NormalizeLicenseIdentifier(batchResult.Project.License); norm != "" && !strings.EqualFold(norm, "NOASSERTION") {
			if isSPDX {
				analysis.ProjectLicense = domain.ResolvedLicense{Identifier: norm, Raw: batchResult.Project.License, IsSPDX: true, Source: domain.LicenseSourceDepsDevProjectSPDX}
			} else {
				analysis.ProjectLicense = domain.ResolvedLicense{Identifier: "", Raw: batchResult.Project.License, IsSPDX: false, Source: domain.LicenseSourceDepsDevProjectNonStandard}
			}
		}
	}
	// Requested version license collection
	if analysis.ReleaseInfo == nil || analysis.ReleaseInfo.RequestedVersion == nil || analysis.ReleaseInfo.RequestedVersion.Version == "" {
		return
	}
	if len(analysis.RequestedVersionLicenses) > 0 { // already populated
		return
	}
	requestedVersion := analysis.ReleaseInfo.RequestedVersion.Version
	// Attempt to reuse batchResult.Package when it aligns with requested version
	var licenses []domain.ResolvedLicense
	if batchResult.Package != nil && len(batchResult.Package.Versions) > 0 {
		// Robust selection mirrors repoURL logic: iterate over all returned versions to locate
		// the exact requestedVersion instead of assuming index 0. This makes behavior explicit
		// and resilient if deps.dev ever returns multiple versions for a versioned PURL.
		for i := range batchResult.Package.Versions {
			v := batchResult.Package.Versions[i]
			if v.VersionKey.Version == requestedVersion {
				licenses = buildResolvedLicensesFromVersion(&v)
				break
			}
		}
	}
	// If not found or empty, perform targeted fetch via deps.dev client (if versioned PURL known)
	if len(licenses) == 0 && s.depsdevClient != nil && analysis.Package != nil && analysis.Package.Version != "" {
		// Build versioned PURL from EffectivePURL (requested version), falling back to Package.PURL
		versioned := analysis.EffectivePURL
		if versioned == "" && analysis.Package != nil {
			versioned = analysis.Package.PURL
		}
		if fetched, err := s.depsdevClient.GetPackageVersionLicenses(context.Background(), versioned); err == nil && len(fetched) > 0 {
			for _, raw := range fetched {
				id, isSPDX := domain.NormalizeLicenseIdentifier(raw)
				if id == "" || strings.EqualFold(id, "NOASSERTION") {
					continue
				}
				src := domain.LicenseSourceDepsDevVersionSPDX
				if !isSPDX {
					src = domain.LicenseSourceDepsDevVersionRaw
				}
				licenses = append(licenses, domain.ResolvedLicense{Identifier: id, Raw: raw, IsSPDX: isSPDX, Source: src})
			}
		} else if err != nil {
			slog.Debug("requested_version_license_fetch_failed", "purl", versioned, "error", err)
		}
	}
	// Fallback to project license value (only if it has an actual value)
	if len(licenses) == 0 && analysis.ProjectLicense.Identifier != "" {
		licenses = []domain.ResolvedLicense{{Identifier: analysis.ProjectLicense.Identifier, Raw: analysis.ProjectLicense.Identifier, IsSPDX: true, Source: domain.LicenseSourceProjectFallback}}
	}
	analysis.RequestedVersionLicenses = licenses

	// when all version licenses fail SPDX recognition but project has valid SPDX.
	if analysis.ProjectLicense.Identifier != "" && len(analysis.RequestedVersionLicenses) > 0 && allVersionLicensesNonSPDX(analysis.RequestedVersionLicenses) {
		analysis.RequestedVersionLicenses = []domain.ResolvedLicense{{Identifier: analysis.ProjectLicense.Identifier, Raw: analysis.ProjectLicense.Identifier, IsSPDX: true, Source: domain.LicenseSourceProjectFallback}}
	}

	// Derive (promote) project-level license from a single requested-version license when eligible.
	// See promoteProjectLicenseFromVersion for detailed policy.
	promoteProjectLicenseFromVersion(analysis)
}

// extractNormalizedVersionLicenses extracts and normalizes SPDX license identifiers from a deps.dev Version.
// Priority order:
//  1. Version.LicenseDetails[].Spdx (preferred SPDX identifiers)
//  2. Fallback to Version.Licenses slice if no SPDX entries found
//
// Behavior:
//   - Trims whitespace
//   - Normalizes via domain.NormalizeLicenseIdentifier
//   - Drops empty / "NOASSERTION" / non-normalizable entries
//   - Deduplicates and returns a sorted slice

// promoteProjectLicenseFromVersion encapsulates the logic for elevating a single requested-version SPDX license
// to project-level when:
//  1. ProjectLicense is still empty AND
//  2. Exactly one requested-version license exists AND
//  3. ProjectLicenseSource is either:
//     a) domain.LicenseSourceDepsDevProjectNonStandard (deps.dev gave a non-standard placeholder), OR
//     b) "" (no project-level source at all) AND
//  4. That single license normalizes to a canonical SPDX (not NOASSERTION).
//
// Outcome: ProjectLicense is set to the normalized identifier and source becomes derived-version.
// Idempotent: safe to call multiple times; it exits early once ProjectLicense is set.
func promoteProjectLicenseFromVersion(a *domain.Analysis) {
	if a == nil {
		return
	}
	if a.ProjectLicense.Identifier != "" { // already set to SPDX or derived
		return
	}
	if len(a.RequestedVersionLicenses) != 1 {
		return
	}
	if !(a.ProjectLicense.IsZero() || a.ProjectLicense.IsNonStandard()) {
		return
	}
	raw := a.RequestedVersionLicenses[0].Identifier
	norm, isSPDX := domain.NormalizeLicenseIdentifier(raw)
	if norm == "" || !isSPDX || strings.EqualFold(norm, "NOASSERTION") {
		return
	}
	a.ProjectLicense = domain.ResolvedLicense{Identifier: norm, Raw: raw, IsSPDX: true, Source: domain.LicenseSourceDerivedFromVersion}
}

// allVersionLicensesNonSPDX returns true if every VersionLicense is non-SPDX (or NOASSERTION equivalent).
func allVersionLicensesNonSPDX(licenses []domain.ResolvedLicense) bool {
	if len(licenses) == 0 {
		return false
	}
	for _, vl := range licenses {
		if vl.IsSPDX && !strings.EqualFold(vl.Identifier, "NOASSERTION") {
			return false
		}
	}
	return true
}

// buildResolvedLicensesFromVersion converts deps.dev version license details into ResolvedLicense slice.
func buildResolvedLicensesFromVersion(v *depsdev.Version) []domain.ResolvedLicense {
	if v == nil {
		return nil
	}
	set := make(map[string]domain.ResolvedLicense)
	add := func(raw string, src string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		id, isSPDX := domain.NormalizeLicenseIdentifier(raw)
		if id == "" || strings.EqualFold(id, "NOASSERTION") {
			return
		}
		if existing, ok := set[id]; ok {
			// prefer SPDX source over raw if duplicate id appears
			if existing.Source == domain.LicenseSourceDepsDevVersionRaw && src == domain.LicenseSourceDepsDevVersionSPDX {
				set[id] = domain.ResolvedLicense{Identifier: id, Raw: raw, IsSPDX: isSPDX, Source: src}
			}
			return
		}
		set[id] = domain.ResolvedLicense{Identifier: id, Raw: raw, IsSPDX: isSPDX, Source: src}
	}
	for _, d := range v.LicenseDetails {
		if d.Spdx != "" {
			add(d.Spdx, domain.LicenseSourceDepsDevVersionSPDX)
		}
	}
	if len(set) == 0 { // fallback to raw licenses
		for _, l := range v.Licenses {
			add(l, domain.LicenseSourceDepsDevVersionRaw)
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]domain.ResolvedLicense, 0, len(set))
	for _, vl := range set {
		out = append(out, vl)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identifier < out[j].Identifier })
	return out
}
