package csv

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/licenses"
)

// ExportLicenses writes extended license analysis data to a CSV file.
//
// DDD Layer: Infrastructure (CSV export implementation)
//
// Data model: each ResolvedLicense holds a single SPDX expression
// (Expression), the upstream-original (Raw), and provenance (Source). See
// docs/adr/0019-license-expression-of-truth.md. Composite shapes (AND / OR
// / WITH / +) survive in the Expression string itself; the
// version_license_is_compound and version_license_leaf_count columns
// surface the structural breakdown without consumers having to parse the
// expression themselves.
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
	defer func() {
		w.Flush()
		if werr := w.Error(); werr != nil && err == nil {
			err = common.NewIOError("failed to flush license CSV writer", werr).
				WithContext("filename", filename)
		}
	}()

	headers := []string{
		"original_purl", "effective_purl", "version_resolved",
		"project_license_expression", "project_license_raw", "project_license_source",
		"project_license_is_spdx", "project_license_is_zero", "project_license_is_compound", "project_license_leaf_count",
		"version_license_expression", "version_license_raw", "version_license_source",
		"version_license_is_spdx", "version_license_is_zero", "version_license_is_compound", "version_license_leaf_count",
		"project_vs_version_mismatch", "licenses_all_missing_or_nonstandard", "fallback_applied", "derived_from_version", "github_override_applied",
		"license_resolution_scenario", "error", "registry_url", "repository_url",
	}
	if err := w.Write(headers); err != nil {
		return common.NewIOError("failed to write license CSV headers", err)
	}

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
		vl := an.RequestedVersionLicense

		projectIsZero := pl.IsZero()
		projectIsSPDX := pl.IsUsableSPDX()
		projectNonStandard := pl.IsNonStandard()
		projectIsNoAssertion := pl.Expression == "NOASSERTION"
		projectLeaves := leavesOf(pl.Expression)
		projectIsCompound := len(projectLeaves) > 1
		projectLeafCount := len(projectLeaves)

		versionIsZero := vl.IsZero()
		versionIsSPDX := vl.IsUsableSPDX()
		versionIsNoAssertion := vl.Expression == "NOASSERTION"
		versionLeaves := leavesOf(vl.Expression)
		versionIsCompound := len(versionLeaves) > 1
		versionLeafCount := len(versionLeaves)

		// project_vs_version_mismatch: both are recognized SPDX, but the
		// project's leaves are NOT a subset of the version's leaves. Set
		// membership replaces the previous Identifier-equality check —
		// supports compound versions like "MIT OR Apache-2.0" containing the
		// project's "MIT" leaf.
		projectVsVersionMismatch := projectIsSPDX && versionIsSPDX && !leafSetContainsAll(versionLeaves, projectLeaves)
		containsProjectID := projectIsSPDX && versionIsSPDX && leafSetContainsAll(versionLeaves, projectLeaves)

		fallbackApplied := vl.Source == domain.LicenseSourceProjectFallback
		derived := pl.Source == domain.LicenseSourceDerivedFromVersion
		githubOverride := pl.Source == domain.LicenseSourceGitHubProjectSPDX || pl.Source == domain.LicenseSourceGitHubProjectNonStandard

		// "Missing or non-standard" includes NOASSERTION at either level: it is
		// a recognized SPDX value but explicitly conveys "upstream refused to
		// assert", so for downstream policy / coverage tracking it is not a
		// usable license.
		projectMissingOrUnusable := projectIsZero || projectNonStandard || projectIsNoAssertion
		versionMissingOrUnusable := versionIsZero || vl.IsNonStandard() || versionIsNoAssertion
		licensesAllMissingOrNonStandard := projectMissingOrUnusable && versionMissingOrUnusable

		scenario := classifyLicenseScenario(scenarioInputs{
			projectZero:         projectIsZero,
			projectSPDX:         projectIsSPDX,
			projectNonStandard:  projectNonStandard,
			projectNoAssertion:  projectIsNoAssertion,
			versionZero:         versionIsZero,
			versionSPDX:         versionIsSPDX,
			versionNonStandard:  vl.IsNonStandard(),
			versionNoAssertion:  versionIsNoAssertion,
			containsProjectID:   containsProjectID,
			fallbackApplied:     fallbackApplied,
			derived:             derived,
			githubOverride:      githubOverride,
			projectVsVersionMismatch: projectVsVersionMismatch,
		})

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
			pl.Expression,
			pl.Raw,
			pl.Source,
			fmt.Sprintf("%t", projectIsSPDX),
			fmt.Sprintf("%t", projectIsZero),
			fmt.Sprintf("%t", projectIsCompound),
			fmt.Sprintf("%d", projectLeafCount),
			vl.Expression,
			vl.Raw,
			vl.Source,
			fmt.Sprintf("%t", versionIsSPDX),
			fmt.Sprintf("%t", versionIsZero),
			fmt.Sprintf("%t", versionIsCompound),
			fmt.Sprintf("%d", versionLeafCount),
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

// leavesOf returns the canonical SPDX identifiers of every leaf in the
// expression, in document order. Returns nil for empty input and for the
// NOASSERTION sentinel (no leaves to expose; NOASSERTION conveys "upstream
// refused to assert" rather than naming any license).
func leavesOf(expr string) []string {
	if expr == "" || expr == "NOASSERTION" {
		return nil
	}
	parsed := licenses.ParseExpression(expr)
	leaves := parsed.Leaves()
	if len(leaves) == 0 {
		return nil
	}
	out := make([]string, 0, len(leaves))
	for _, l := range leaves {
		out = append(out, l.Identifier)
	}
	return out
}

// leafSetContainsAll reports whether every needle is present in haystack.
// Empty needle returns true (vacuous truth — used to short-circuit the
// project-vs-version check when the project has no leaves to test).
func leafSetContainsAll(haystack, needles []string) bool {
	if len(needles) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(haystack))
	for _, h := range haystack {
		if h != "" {
			set[h] = struct{}{}
		}
	}
	for _, n := range needles {
		if n == "" {
			return false // a needle with no canonical ID can never match
		}
		if _, ok := set[n]; !ok {
			return false
		}
	}
	return true
}

// scenarioInputs collects the boolean flags the scenario classifier branches
// on. Promoting these to a struct (instead of positional args) prevents
// argument-order regressions when new flags are added — the issue that
// triggered finding H1's NOASSERTION miss.
type scenarioInputs struct {
	projectZero, projectSPDX, projectNonStandard, projectNoAssertion bool
	versionZero, versionSPDX, versionNonStandard, versionNoAssertion bool
	containsProjectID, fallbackApplied, derived, githubOverride      bool
	projectVsVersionMismatch                                         bool
}

// classifyLicenseScenario assigns a scenario label (mutually exclusive, ordered rules).
//
// NOASSERTION is treated as its own classification axis: a license with
// Expression == "NOASSERTION" is recognized SPDX (so IsUsableSPDX is false,
// IsNonStandard is also false), and would otherwise fall through every
// branch into "catch_all". The dedicated NOASSERTION branches keep operator
// triage actionable — operators care which side asserted nothing.
func classifyLicenseScenario(in scenarioInputs) string {
	// High-priority explicit scenarios
	if in.fallbackApplied {
		return "fallback_applied"
	}
	if in.derived {
		return "derived_from_version"
	}
	if in.githubOverride && in.projectSPDX {
		return "github_override_spdx"
	}
	if in.githubOverride && in.projectNonStandard {
		return "github_override_nonstandard"
	}

	// NOASSERTION-explicit branches before the generic project/version matrix.
	if in.projectNoAssertion && in.versionNoAssertion {
		return "noassertion_both"
	}
	if in.projectNoAssertion {
		return "noassertion_project"
	}
	if in.versionNoAssertion {
		return "noassertion_version"
	}

	if in.projectZero && in.versionZero {
		return "no_project_no_version"
	}
	if in.projectSPDX && in.versionZero {
		return "project_spdx_no_version"
	}
	if in.projectNonStandard && in.versionZero {
		return "project_nonstandard_no_version"
	}
	if !in.projectSPDX && !in.projectNonStandard && !in.projectZero && in.versionZero {
		return "project_other_no_version"
	}

	if !in.projectSPDX && !in.projectNonStandard && !in.projectZero && !in.versionZero {
		return "project_other_with_versions"
	}

	if !in.projectSPDX && !in.projectNonStandard && in.projectZero && !in.versionZero {
		return "versions_only"
	}

	if in.projectSPDX && in.versionSPDX && !in.projectVsVersionMismatch && in.containsProjectID {
		return "project_spdx_version_all_spdx_consistent"
	}
	if in.projectSPDX && in.versionSPDX && in.projectVsVersionMismatch {
		return "project_spdx_version_all_spdx_mismatch"
	}

	if in.projectSPDX && in.versionNonStandard {
		return "project_spdx_versions_all_nonspdx"
	}

	if in.projectNonStandard && in.versionSPDX {
		return "project_nonstandard_versions_mixed"
	}
	if in.projectNonStandard && in.versionNonStandard {
		return "project_nonstandard_versions_all_nonspdx"
	}

	return "catch_all"
}

// sanitizeError removes newlines from error messages for CSV safety.
func sanitizeError(e string) string {
	return strings.ReplaceAll(strings.ReplaceAll(e, "\n", " "), "\r", " ")
}
