# License Resolution Model

[← Back to README.md](../README.md)

This document describes the current deterministic license acquisition / normalization / fallback / promotion logic implemented in the codebase.

## Core Domain Type: `ResolvedLicense`

Defined in `internal/domain/analysis/models.go`. Used for both the project level (`Analysis.ProjectLicense`) and request version level (`Analysis.RequestedVersionLicenses`).

| Field | Meaning | Notes |
|-------|---------|-------|
| `Identifier` | Normalized official SPDX identifier (empty if non-SPDX) | Normalized by `NormalizeLicenseIdentifier`; `NOASSERTION` is discarded |
| `Raw` | Original upstream string | Preserved for auditing even when SPDX is recognized |
| `Source` | Origin (closed set of constants) | See `license_sources.go` |
| `IsSPDX` | Recognized as official SPDX | Excludes `NOASSERTION` |
| `IsZero()` | Data is completely absent | All fields are empty / false |
| `IsNonStandard()` | Non-SPDX data exists but is not normalized | Source is `*-nonstandard` / `*-raw` |

## Normalization Rules

1. Trim whitespace (skip if empty)
2. Normalize via `NormalizeLicenseIdentifier` (case / alias handling)
3. Discard if empty or `NOASSERTION`
4. Set `IsSPDX=true` only when officially matched

## Source Constants (`license_sources.go`)

| Constant | Value | When Assigned |
|----------|-------|---------------|
| `LicenseSourceDepsDevProjectSPDX` | `depsdev-project-spdx` | deps.dev Project.License is official SPDX |
| `LicenseSourceDepsDevProjectNonStandard` | `depsdev-project-nonstandard` | deps.dev Project.License is non-SPDX |
| `LicenseSourceDepsDevVersionSPDX` | `depsdev-version-spdx` | Version.LicenseDetails[].Spdx is official SPDX |
| `LicenseSourceDepsDevVersionRaw` | `depsdev-version-raw` | Version.Licenses[] raw value cannot be normalized |
| `LicenseSourceGitHubProjectSPDX` | `github-project-spdx` | GitHub repository licenseInfo SPDX (fills existing gap) |
| `LicenseSourceGitHubProjectNonStandard` | `github-project-nonstandard` | GitHub license is non-SPDX (empty/NOASSERTION spdxId or cannot normalize) |
| `LicenseSourceGitHubVersionSPDX` | `github-version-spdx` | Reserved (unused) |
| `LicenseSourceGitHubVersionRaw` | `github-version-raw` | Reserved (unused) |
| `LicenseSourceProjectFallback` | `project-fallback` | Project SPDX copied to Version lacking SPDX / having only non-SPDX |
| `LicenseSourceDerivedFromVersion` | `derived-from-version` | Single Version SPDX promoted to project license |

## Resolution Flow (Overview)

1. Project evaluation (deps.dev batch): SPDX → `depsdev-project-spdx`; otherwise → `depsdev-project-nonstandard`
2. Request version candidate collection:
   - `LicenseDetails[].Spdx` prioritized (deduplicated, SPDX wins over raw)
   - Falls back to raw `Licenses[]` (non-SPDX) if none
3. Version set empty & Project has SPDX → add single `project-fallback` entry
4. All Version entries are non-SPDX & Project has SPDX → replace with single `project-fallback`
5. Project empty/non-standard & Version has unique SPDX → promote to Project (`derived-from-version`)
6. GitHub enrichment: if Project is still empty/non-standard, use GitHub license (SPDX or non-standard)

## Promotion and Fallback Conditions

| Action | Trigger | Result Source | Safety Guard |
|--------|---------|---------------|--------------|
| Version → Project promotion | Project is zero or non-standard AND Version SPDX is unique | `derived-from-version` | Not executed if multiple SPDXs |
| Project → Version fallback | Version empty or all non-SPDX AND Project has SPDX | `project-fallback` | Not executed if Version has 1+ existing SPDX |

## Helper Semantics

| Scenario | `IsZero()` | `IsNonStandard()` |
|----------|-----------|-------------------|
| Completely absent | true | false |
| deps.dev project non-SPDX | false | true |
| GitHub project non-SPDX | false | true |
| version raw non-SPDX | false | true |
| project-fallback SPDX | false | false |
| derived-from-version SPDX | false | false |
| Official SPDX (deps.dev / GitHub) | false | false |

## Multi-License Handling (Version)

- Deduplicated by normalized identifier (map)
- Duplicate raw + SPDX prioritizes SPDX source
- Sorted by `Identifier` for stable output
- When multiple SPDXs remain: neither promotion nor fallback intervenes

## Non-SPDX ("nonstandard") Criteria

Any of the following:

- deps.dev project license cannot be normalized
- GitHub `licenseInfo` spdxId is empty/`NOASSERTION` and name cannot be normalized
- Version has no SPDX and raw entries cannot be normalized

## NOASSERTION Handling

`NOASSERTION` (case-insensitive) is treated as absent: discarded and does not trigger promotion / fallback conditions.

## Error / Edge Cases

| Case | Behavior |
|------|----------|
| Request version fetch failure | Version licenses remain empty → fallback evaluation proceeds |
| All non-SPDX + Project also non-SPDX | Preserved as-is (no destructive replacement) |
| Promotion completed before GitHub enrichment | Subsequent GitHub SPDX does not overwrite (determinism) |
| Reserved GitHub version-level sources | Currently unused; future extension point |

## License State Matrix

### ProjectLicense States

| Identifier | Raw | Source | Meaning |
|------------|-----|--------|---------|
| `""` | `""` | `""` | Pure absence: deps.dev project empty, no GitHub, no promotion |
| `""` | `non-standard...` | `depsdev-project-nonstandard` | deps.dev non-SPDX placeholder (GitHub absent or also non-SPDX) |
| `""` | `Some Custom Text` | `github-project-nonstandard` | GitHub non-SPDX (spdxId empty/NOASSERTION or cannot normalize) |
| SPDX (e.g., `MIT`) | original (e.g., `mit`) | `depsdev-project-spdx` | deps.dev SPDX; raw preserves original casing |
| SPDX | original | `github-project-spdx` | GitHub SPDX filled the gap |
| SPDX | original | `derived-from-version` | Single Version SPDX promoted (Project was previously empty/non-standard) |
| (empty or non-SPDX) | (various) | `(empty)` / nonstandard | Promotion skipped due to multiple/mixed Version SPDXs |

Notes:

1. `project-fallback` does not appear at the Project level (direction is Project → Version only)
2. `IsNonStandard()` covers `*-nonstandard` / `*-raw` sources
3. `Raw` preserves upstream display/audit value even when SPDX-normalized

### RequestedVersionLicenses States

| Identifier | Raw | Source | Meaning |
|------------|-----|--------|---------|
| (empty slice) | – | – | No Version data; Project absent/non-SPDX and fallback not triggered |
| SPDX | original | `depsdev-version-spdx` | From `Version.LicenseDetails[].Spdx` |
| non-SPDX token | original | `depsdev-version-raw` | Raw license could not be normalized to SPDX |
| SPDX | same | `project-fallback` | Version empty/all non-SPDX + Project has SPDX |
| multiple SPDXs | originals | `depsdev-version-spdx` | Multiple candidates preserved; no fallback/promotion |
| all non-SPDX | originals | `depsdev-version-raw` | If Project has SPDX → replaced with single fallback; otherwise as-is |

Flow summary: (1) SPDX-priority collection → (2) dedup/normalize → (3) if empty/all non-SPDX & Project has SPDX → fallback → (4) if Project empty/non-standard & Version SPDX is unique → promotion.

### Helper Quick Reference

| Case | `IsZero()` | `IsNonStandard()` |
|------|------------|-------------------|
| Empty (all fields) | true | false |
| deps.dev non-SPDX project | false | true |
| GitHub non-SPDX project | false | true |
| deps.dev version raw non-SPDX | false | true |
| project-fallback SPDX | false | false |
| derived-from-version SPDX | false | false |
| Official SPDX | false | false |

Callers should use intention helpers instead of branching on `Source` directly.

## Future Extensions (Planned / Optional)

- SPDX expression parsing / validation
- Additional sources (registry manifests, LICENSE file hashes, SBOM import)
- Manual override channel (`manual-project-spdx`, etc.)
- Confidence / scoring layer reintroduction
