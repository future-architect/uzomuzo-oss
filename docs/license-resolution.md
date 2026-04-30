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
| `LicenseSourceMavenPOMSPDX` | `maven-pom-spdx` | Maven Central pom.xml `<licenses>` resolved to canonical SPDX (via `<name>` normalization or `<url>` lookup) |
| `LicenseSourceMavenPOMNonStandard` | `maven-pom-nonstandard` | Maven pom.xml `<licenses>` entry yielded a non-SPDX value (raw `<name>` or `<url>` preserved) |
| `LicenseSourceClearlyDefinedSPDX` | `clearlydefined-spdx` | ClearlyDefined.io curated `licensed.declared` resolved to canonical SPDX (single ID or operand of an SPDX expression) |
| `LicenseSourceClearlyDefinedNonStandard` | `clearlydefined-nonstandard` | ClearlyDefined.io value that did not normalize to SPDX (e.g. `LicenseRef-scancode-*`, scancode-internal names) |
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
7. **Ecosystem-native manifest fallback** (Maven only at present, NuGet/PyPI follow): if Project remains empty/non-standard or any version slice still lacks SPDX, fetch the package's own manifest (`pom.xml`) and apply its `<licenses>` declarations. SPDX results override `*-nonstandard` sources; canonical SPDX is never overwritten (disagreement is logged at WARN). See [Ecosystem-Native Fallback](#ecosystem-native-fallback) below.
8. **ClearlyDefined.io safety net** (cross-ecosystem): if Project is still empty/non-standard after the manifest tier, consult [ClearlyDefined.io](https://clearlydefined.io/)'s curated `licensed.declared`. SPDX expressions are split via `licenses.ParseExpression` so each operand becomes its own `ResolvedLicense`. Same override matrix as the manifest tier; canonical SPDX never overwritten. Score-gated at `licensed.score.declared >= 30`. See ADR-0018 for chain rationale.

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

## Ecosystem-Native Fallback

deps.dev and GitHub `licenseInfo` together cover most npm/Go/Cargo/Gem/Composer packages but leave a long tail unresolved for ecosystems whose authoritative license metadata lives in the package's own manifest. Observed coverage on a 30k+ package downstream sample:

| Ecosystem | Coverage before fallback |
|---|---:|
| composer / golang / cargo / gem / npm | 74–89% |
| pypi | 62% |
| **maven** | **38%** |
| **nuget** | **35%** |

The third-tier fallback fetches the package's own ecosystem manifest after deps.dev and GitHub enrichment have run.

| Ecosystem | Source | Status |
|---|---|---|
| Maven | `pom.xml` `<licenses>` from Maven Central | Implemented (`internal/infrastructure/maven/license.go`) |
| NuGet | `.nuspec` `<license>` / `<licenseUrl>` from `api.nuget.org` | Planned (follow-up PR) |
| PyPI | JSON API `info.license_expression` / `classifiers` / `info.license` | Planned (follow-up PR) |

### Maven `<licenses>` decision tree (per entry)

1. `<name>` normalized via `NormalizeLicenseIdentifier` → SPDX → emit `maven-pom-spdx`.
2. Else `<url>` looked up against the curated SPDX URL table (`internal/domain/licenses/url_lookup.go`, ~30 entries covering apache.org, opensource.org, gnu.org, mozilla.org, eclipse.org, creativecommons.org, etc.) → SPDX → emit `maven-pom-spdx`.
3. Else preserve `<name>` (or `<url>` if no name) as `Raw`, emit `maven-pom-nonstandard` with `Identifier` empty.

`<licenses>` may contain multiple entries — each is emitted as its own `ResolvedLicense`. The dispatcher in `internal/infrastructure/integration/populate_manifest_license.go` then picks the first SPDX entry as the candidate `ProjectLicense` and writes the full list to `RequestedVersionLicenses` when the existing slice is empty or all non-SPDX.

Parent POM inheritance is intentionally skipped in v1: the additional HTTP cost is rarely repaid (license declarations are typically per-artifact in Maven by convention). Revisit if telemetry shows >5% of misses are inheritance-bound.

### Override rules (any ecosystem)

| Existing `Source` | Manifest = SPDX | Manifest = non-SPDX |
|---|---|---|
| `IsZero()` | take it (`*-spdx`) | take it (`*-nonstandard`) |
| `*-nonstandard` / `*-raw` (any layer) | replace | no-op |
| Canonical SPDX (any layer) | no-op (log `license_disagreement` at WARN) | no-op |

Pre-fetch short-circuit: if `ProjectLicense.IsSPDX` AND every `RequestedVersionLicenses` entry is canonical SPDX, the enricher skips the analysis entirely without issuing any HTTP.

### Best-effort + rate-limit policy

The enricher is **best-effort**: per-coordinate fetch failures (transport, 5xx, decode errors) are logged at WARN level as `license_manifest_fetch_failed`; HTTP 429 responses log as `license_manifest_rate_limited` so they can be monitored independently. The analysis is left untouched in all cases — affected packages remain `*-nonstandard` rather than being lost. Within a single batch the dispatcher deduplicates by (groupId, artifactId, version) so identical coordinates issue exactly one HTTP request.

Maven Central applies CDN-layer rate limits to anonymous traffic. If 429s become frequent in production, follow-up work can add `MaxConcurrency` / `RequestInterval` controls to the Maven client (mirroring the GitHub client). For now the bounded fan-out of `enrichPyPISummary` provides equivalent shape without explicit caps.

## ClearlyDefined.io Safety Net

The fourth and final tier consults [ClearlyDefined.io](https://clearlydefined.io/), a Microsoft + GitHub-led, scancode-toolkit-backed curation database. It runs after the manifest tier on analyses that still lack a canonical SPDX identifier. Implemented in `internal/infrastructure/clearlydefined/client.go` and dispatched from `internal/infrastructure/integration/populate_clearlydefined_license.go`. Design rationale and chain placement documented in [ADR-0018](adr/0018-clearlydefined-integration.md).

### `licensed.declared` decision tree

CD's `declared` field has four observed shapes:

1. Single SPDX ID (`Apache-2.0`) → emit one `clearlydefined-spdx`.
2. SPDX expression (`Apache-2.0 AND EPL-2.0`, `CDDL-1.1 OR GPL-2.0-only WITH Classpath-exception-2.0`) → parsed via `licenses.ParseExpression`; each operand becomes its own `ResolvedLicense` (SPDX leaves → `clearlydefined-spdx`, non-SPDX leaves → `clearlydefined-nonstandard`).
3. `LicenseRef-scancode-*` → emit `clearlydefined-nonstandard` with the raw value preserved. The `LicenseRef-` prefix is SPDX's own way of saying "no canonical SPDX exists for this license"; conversion would invent data.
4. scancode-internal name (`Plexus`, etc.) → emit `clearlydefined-nonstandard`.

### Score gating

`licensed.score.declared >= 30` is required for CD to contribute. Lower scores indicate stale or uncurated entries; the empirical distribution from issue #354 shows everything in the 45–75 range is real, while 0 is genuinely-empty (`mysql-connector-java`).

### Override rules and caching

Override matrix is identical to the manifest tier (canonical SPDX never overwritten; `*-nonstandard` slots replaced; first SPDX leaf promoted to `ProjectLicense`). The dispatcher reuses `applyManifestLicenses`. CD responses are cached in-memory: 24h positive TTL (definitions are stable curation artifacts), 1h negative TTL (CD lazily curates new releases).

Per-coordinate fetch failures log as `license_clearlydefined_fetch_failed` (WARN), with HTTP 429 specifically tagged as `license_clearlydefined_rate_limited` for telemetry separation. Hits and misses are at DEBUG.

## Future Extensions (Planned / Optional)

- NuGet `.nuspec` and PyPI `info.*` license-source wiring (issue #327, follow-up PRs)
- Auto-generation of the URL→SPDX table from upstream SPDX `seeAlso` field via `cmd/uzomuzo update-spdx`
- Manual override channel (`manual-project-spdx`, etc.)
- SPDX exceptions table for `WITH` clause normalization (currently passes through verbatim)
- Confidence / scoring layer reintroduction
