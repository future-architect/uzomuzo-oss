package analysis

// Unified license source constants (domain-level closed set)
// Pattern: provider-scope-qualifier (qualifier sometimes omitted when obvious).
// Scope values:
//   - project: provenance relates to project-level metadata
//   - version: provenance relates to a specific version's metadata
//
// Special transformation sources:
//   - project-fallback: a project SPDX license applied to a requested version when that version only had non-SPDX / no data.
//   - derived-from-version: a single valid version SPDX license promoted to become the project license when project license was unknown or non-standard.
//
// Qualifiers:
//   - spdx: canonical SPDX identifier was supplied
//   - raw / nonstandard: original non-SPDX value retained (raw at version level, nonstandard at project level)
//
// Naming Rationale:
//   - Single cohesive namespace simplifies grepping (grep -F "LicenseSource"), avoids dual legacy prefixes (VersionLicenseSource* vs LicenseSource*),
//     and keeps future additions (e.g. Manual Override, Scanner detection) predictable.
//   - Distinguishing project vs version inside value strings (e.g. "depsdev-project-spdx") preserves any downstream analytics filtering requirement previously
//     satisfied by separate constant groups.
const (
	// deps.dev project-level SPDX license directly reported (normalized identifier)
	LicenseSourceDepsDevProjectSPDX = "depsdev-project-spdx"
	// deps.dev project-level non-SPDX / ambiguous placeholder (kept in Raw; Identifier empty)
	LicenseSourceDepsDevProjectNonStandard = "depsdev-project-nonstandard"

	// deps.dev version-level SPDX license (LicenseDetails.Spdx)
	LicenseSourceDepsDevVersionSPDX = "depsdev-version-spdx"
	// deps.dev version-level non-SPDX raw string (attempted normalization failed or not SPDX)
	LicenseSourceDepsDevVersionRaw = "depsdev-version-raw"

	// GitHub detected project-level SPDX (via repository license API)
	LicenseSourceGitHubProjectSPDX = "github-project-spdx"
	// GitHub detected project-level non-SPDX (spdxId empty/NOASSERTION or name not normalizable)
	LicenseSourceGitHubProjectNonStandard = "github-project-nonstandard"
	// GitHub detected version-level SPDX (reserved / future use)
	LicenseSourceGitHubVersionSPDX = "github-version-spdx"
	// GitHub detected version-level non-SPDX raw (reserved / future use)
	LicenseSourceGitHubVersionRaw = "github-version-raw"

	// Maven Central pom.xml <licenses> entry resolved to a canonical SPDX identifier
	// (matched via license <name> normalization or <url> via SPDX seeAlso/aliases).
	LicenseSourceMavenPOMSPDX = "maven-pom-spdx"
	// Maven Central pom.xml <licenses> entry that yielded a non-SPDX value (raw <name> or <url> kept; Identifier empty).
	LicenseSourceMavenPOMNonStandard = "maven-pom-nonstandard"

	// ClearlyDefined.io curated `licensed.declared` resolved to canonical SPDX
	// (single id, or one operand of an SPDX expression parsed via licenses.ParseExpression).
	LicenseSourceClearlyDefinedSPDX = "clearlydefined-spdx"
	// ClearlyDefined.io curated value that did not normalize to SPDX
	// (e.g. `LicenseRef-scancode-public-domain`, scancode-internal names like `Plexus`,
	// or operands of an SPDX expression that failed normalization).
	LicenseSourceClearlyDefinedNonStandard = "clearlydefined-nonstandard"

	// Project → Version fallback: apply known project SPDX license to versions that only had non-SPDX / no data.
	LicenseSourceProjectFallback = "project-fallback"
	// Version → Project promotion: single version SPDX license elevated to project when project license unknown/non-standard.
	LicenseSourceDerivedFromVersion = "derived-from-version"
)
