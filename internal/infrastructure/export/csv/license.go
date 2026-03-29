package csv

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// ExportLicenses writes extended license analysis data to a CSV file.
//
// DDD Layer: Infrastructure (CSV export implementation)
// Columns (extended set, updated):
// original_purl,effective_purl,version_resolved,project_license_identifier,project_license_raw,project_license_source,project_license_is_spdx,project_license_is_zero,version_license_identifiers,version_license_raws,version_license_sources,version_license_count,version_licenses_all_non_spdx,version_licenses_any_composite_expr,project_vs_version_mismatch,licenses_all_missing_or_nonstandard,fallback_applied,derived_from_version,github_override_applied,license_resolution_scenario,error,registry_url,repository_url
func ExportLicenses(analyses map[string]*domain.Analysis, filename string) (err error) {
	file, err := os.Create(filename)
	if err != nil {
		return common.NewIOError("failed to create license CSV file", err).WithContext("filename", filename)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	w := csv.NewWriter(file)
	defer w.Flush()

	headers := []string{
		"original_purl", "effective_purl", "version_resolved",
		"project_license_identifier", "project_license_raw", "project_license_source", "project_license_is_spdx", "project_license_is_zero",
		"version_license_identifiers", "version_license_raws", "version_license_sources", "version_license_count", "version_licenses_all_non_spdx", "version_licenses_any_composite_expr",
		"project_vs_version_mismatch", "licenses_all_missing_or_nonstandard", "fallback_applied", "derived_from_version", "github_override_applied",
		"license_resolution_scenario", "error", "registry_url", "repository_url",
	}
	if err := w.Write(headers); err != nil {
		return common.NewIOError("failed to write license CSV headers", err)
	}

	// Stable deterministic ordering for readability
	keys := make([]string, 0, len(analyses))
	for k := range analyses {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		an := analyses[k]
		if an == nil {
			continue
		}

		pl := an.ProjectLicense
		vls := an.RequestedVersionLicenses
		vCount := len(vls)

		// Project level helpers
		projectIsZero := pl.IsZero()
		projectIsSPDX := pl.IsSPDX
		projectNonStandard := pl.IsNonStandard()

		// Version level aggregation
		identifiers := make([]string, 0, vCount)
		raws := make([]string, 0, vCount)
		sources := make([]string, 0, vCount)
		anyComposite := false
		allNonSPDX := true
		containsProjectID := false

		for _, vl := range vls {
			identifiers = append(identifiers, vl.Identifier)
			raws = append(raws, vl.Raw)
			sources = append(sources, vl.Source)
			if vl.IsSPDX {
				allNonSPDX = false
			}
			if compositeExpr(vl.Identifier) || compositeExpr(vl.Raw) {
				anyComposite = true
			}
			if projectIsSPDX && vl.Identifier == pl.Identifier {
				containsProjectID = true
			}
		}

		versionAllNonSPDX := vCount > 0 && allNonSPDX
		projectVsVersionMismatch := projectIsSPDX && vCount > 0 && !containsProjectID

		fallbackApplied := vCount == 1 && vls[0].Source == domain.LicenseSourceProjectFallback
		derived := pl.Source == domain.LicenseSourceDerivedFromVersion
		githubOverride := pl.Source == domain.LicenseSourceGitHubProjectSPDX || pl.Source == domain.LicenseSourceGitHubProjectNonStandard

		licensesAllMissingOrNonStandard := (projectIsZero || projectNonStandard) && (vCount == 0 || versionAllNonSPDX)

		scenario := classifyLicenseScenario(projectIsZero, projectIsSPDX, projectNonStandard, vCount, versionAllNonSPDX, containsProjectID, fallbackApplied, derived, githubOverride, projectVsVersionMismatch)

		errStr := ""
		if an.Error != nil {
			errStr = sanitizeError(an.Error.Error())
		}

		registryURL := ""
		if an.PackageLinks != nil {
			registryURL = an.PackageLinks.RegistryURL
		}
		repoURL := an.RepoURL

		record := []string{
			an.OriginalPURL,
			an.EffectivePURL,
			fmt.Sprintf("%t", an.IsVersionResolved()),
			pl.Identifier,
			pl.Raw,
			pl.Source,
			fmt.Sprintf("%t", projectIsSPDX),
			fmt.Sprintf("%t", projectIsZero),
			strings.Join(identifiers, ";"),
			strings.Join(raws, ";"),
			strings.Join(sources, ";"),
			fmt.Sprintf("%d", vCount),
			fmt.Sprintf("%t", versionAllNonSPDX),
			fmt.Sprintf("%t", anyComposite),
			fmt.Sprintf("%t", projectVsVersionMismatch),
			fmt.Sprintf("%t", licensesAllMissingOrNonStandard),
			fmt.Sprintf("%t", fallbackApplied),
			fmt.Sprintf("%t", derived),
			fmt.Sprintf("%t", githubOverride),
			scenario,
			errStr,
			registryURL,
			repoURL,
		}

		if err := w.Write(record); err != nil {
			return common.NewIOError("failed to write license CSV record", err).WithContext("purl", k)
		}
	}

	return nil
}

// compositeExpr detects if a license token contains composite logical expression markers.
func compositeExpr(s string) bool {
	if s == "" {
		return false
	}
	u := strings.ToUpper(s)
	return strings.Contains(u, " AND ") || strings.Contains(u, " OR ") || strings.Contains(u, "(") || strings.Contains(u, ")")
}

// classifyLicenseScenario assigns a scenario label (mutually exclusive, ordered rules).
func classifyLicenseScenario(projectZero, projectSPDX, projectNonStandard bool, vCount int, versionAllNonSPDX, containsProjectID, fallbackApplied, derived, githubOverride, mismatch bool) string {
	// High-priority explicit scenarios
	if fallbackApplied {
		return "fallback_applied"
	}
	if derived {
		return "derived_from_version"
	}
	if githubOverride && projectSPDX {
		return "github_override_spdx"
	}
	if githubOverride && projectNonStandard {
		return "github_override_nonstandard"
	}

	if projectZero && vCount == 0 {
		return "no_project_no_version"
	}
	if projectSPDX && vCount == 0 {
		return "project_spdx_no_version"
	}
	if projectNonStandard && vCount == 0 {
		return "project_nonstandard_no_version"
	}
	if !projectSPDX && !projectNonStandard && !projectZero && vCount == 0 {
		return "project_other_no_version"
	}

	if !projectSPDX && !projectNonStandard && !projectZero && vCount > 0 {
		return "project_other_with_versions"
	}

	if !projectSPDX && !projectNonStandard && projectZero && vCount > 0 {
		return "versions_only"
	}

	if projectSPDX && vCount > 0 && !mismatch && !versionAllNonSPDX && containsProjectID {
		return "project_spdx_version_all_spdx_consistent"
	}
	if projectSPDX && vCount > 0 && mismatch && !versionAllNonSPDX {
		return "project_spdx_version_all_spdx_mismatch"
	}

	if projectSPDX && vCount > 0 && !mismatch && versionAllNonSPDX {
		return "project_spdx_versions_all_nonspdx"
	}
	if projectSPDX && vCount > 0 && mismatch && versionAllNonSPDX {
		return "project_spdx_versions_all_nonspdx_mismatch"
	}

	if projectNonStandard && vCount > 0 && !versionAllNonSPDX {
		return "project_nonstandard_versions_mixed"
	}
	if projectNonStandard && vCount > 0 && versionAllNonSPDX {
		return "project_nonstandard_versions_all_nonspdx"
	}

	return "catch_all"
}

// sanitizeError removes newlines from error messages for CSV safety.
func sanitizeError(e string) string {
	return strings.ReplaceAll(strings.ReplaceAll(e, "\n", " "), "\r", " ")
}
