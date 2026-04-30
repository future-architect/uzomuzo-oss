# License Resolution Model

[← Back to README.md](../README.md)

This document describes the current deterministic license acquisition / normalization / fallback / promotion logic implemented in the codebase.

## Core Domain Type: `ResolvedLicense`

Defined in `internal/domain/analysis/models.go`. Used at both the project
level (`Analysis.ProjectLicense`) and the requested-version level
(`Analysis.RequestedVersionLicense` — singular). The data model follows
SPDX 2.3 §10 (License Expressions); see
[ADR-0019](adr/0019-license-expression-of-truth.md) for the design
rationale.

| Field | Meaning | Notes |
|-------|---------|-------|
| `Expression` | Canonical SPDX expression string, `""`, or `"NOASSERTION"` | Compound shapes (AND / OR / WITH / +) survive in this single string. `""` means uzomuzo could not parse / recognize SPDX content; `"NOASSERTION"` preserves the SPDX 2.3 sentinel. |
| `Source` | Origin (closed set of constants) | See `license_sources.go` |
| `Raw` | Upstream-original string verbatim | Preserved for audit trail and SBOM `name` fallback when `Expression` is empty. NEVER overwritten by the normalizer. |
| `IsZero()` | Data is completely absent | All fields are empty |
| `IsNonStandard()` | Non-SPDX data exists but is not normalized | `Expression == ""` AND Source is `*-nonstandard` / `*-raw` |

## Normalization Rules

License expression normalization is centralized in
`internal/domain/licenses` and consumes the SPDX expression parser added in
PR #358:

1. **Single-string inputs** (e.g., deps.dev `Project.License`, GitHub
   `licenseInfo.name`, Maven POM `<name>`) go through
   `licenses.NormalizeExpression(raw) string`:
   - empty / whitespace → `""`
   - `"NOASSERTION"` / `"NONE"` (any case) → `"NOASSERTION"`
   - all-recognized leaves → canonical re-rendered expression
   - any non-SPDX leaf → `""` (caller keeps raw separately)
2. **Array inputs** (e.g., deps.dev `Version.Licenses`,
   `LicenseDetails[].Spdx`, Maven POM multi-`<license>`) go through
   `licenses.JoinExpressions(rawInputs) string` which OR-joins the
   normalized survivors. The implicit-OR convention matches Maven, npm,
   and PyPI multi-license semantics.
3. **Leaf-only API inputs** (e.g., GitHub `licenseInfo.spdxId`, which is a
   single SPDX ID by API contract) use the existing
   `domain.NormalizeLicenseIdentifier(raw) (string, bool)` — leaner than
   the parser-based path when the input is guaranteed-leaf.

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

1. **Project evaluation** (deps.dev batch): `Project.License` flows through
   `NormalizeExpression`. Recognized → `depsdev-project-spdx` (Expression
   set); non-SPDX → `depsdev-project-nonstandard` (Expression empty, Raw
   preserved).
2. **Requested version collection**:
   - `LicenseDetails[].Spdx` is preferred (filtered for non-empty entries)
   - Falls back to `Licenses[]` if `LicenseDetails` produced no candidates
   - Both arrays are OR-joined via `JoinExpressions` into a single
     canonical Expression. If at least one entry survives normalization,
     Source is `depsdev-version-spdx`; otherwise Expression is empty,
     Source is `depsdev-version-raw`, and Raw holds the OR-joined
     concatenation for audit.
3. **Project → Version fallback**: if the version expression is zero AND
   Project has SPDX → set `RequestedVersionLicense` from Project with
   Source `project-fallback`.
4. **Non-SPDX version override**: if version Expression is empty (raw-only)
   AND Project has SPDX → replace with `project-fallback` carrying the
   Project Expression.
5. **Version → Project promotion**: if Project is empty / non-standard AND
   the version Expression is **single-leaf** (parse and check
   `Root.License != nil`) AND the leaf is canonical SPDX → promote to
   Project with Source `derived-from-version`. **Compound expressions are
   not promoted** (a project-level SPDX claim must be unambiguous; see
   ADR-0019).
6. **GitHub enrichment**: if Project is still empty / non-standard, use
   `licenseInfo.spdxId` (preferred — leaf-only) then `licenseInfo.name`.
7. **Ecosystem-native manifest fallback** (Maven only at present;
   NuGet/PyPI follow): if Project remains empty/non-standard or
   RequestedVersionLicense lacks SPDX, fetch the package's own manifest
   (`pom.xml`) and apply its `<licenses>` declarations. Multiple manifest
   entries are OR-joined into a single Expression. SPDX results override
   `*-nonstandard` sources; canonical SPDX is never overwritten
   (disagreement is logged at WARN). See [Ecosystem-Native Fallback](#ecosystem-native-fallback).
8. **ClearlyDefined.io safety net** (cross-ecosystem): if Project is still
   empty/non-standard after the manifest tier, consult
   [ClearlyDefined.io](https://clearlydefined.io/)'s curated
   `licensed.declared`. SPDX expressions flow through the same
   `NormalizeExpression` / `JoinExpressions` path; the resulting Expression
   is OR-joined when multiple operands survive. Same override matrix as the
   manifest tier; canonical SPDX never overwritten. Score-gated at
   `licensed.score.declared >= 30`. See ADR-0018 for chain rationale and
   ADR-0019 for the Expression model.

## Promotion and Fallback Conditions

| Action | Trigger | Result Source | Safety Guard |
|--------|---------|---------------|--------------|
| Version → Project promotion | Project is zero or non-standard AND Version Expression is single-leaf canonical SPDX | `derived-from-version` | Compound expressions (`MIT OR Apache-2.0`) are NOT promoted |
| Project → Version fallback | Version Expression is empty AND Project has canonical SPDX | `project-fallback` | Compound version expressions are preserved (not overridden) |

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

## Compound Expression Handling

Multiple license declarations from upstream (e.g. deps.dev's
`Version.Licenses` array, or Maven POMs with multiple `<license>` blocks)
are OR-joined into a single canonical SPDX expression at normalization
time:

- `["MIT", "Apache-2.0"]` → `"MIT OR Apache-2.0"`
- `["MIT", "MIT OR Apache-2.0"]` → `"MIT OR Apache-2.0"` (parser flattens)
- `["MIT", "ProprietaryFoo"]` → `"MIT"` (non-SPDX entries drop entry-wise)
- `["NOASSERTION", "MIT"]` → `"MIT"` (NOASSERTION dropped from mixed sets)
- `["NOASSERTION"]` → `"NOASSERTION"` (preserved when alone)

`AND`, `WITH`, `+` semantics survive in the Expression string when present
in the upstream input. Consumers needing leaf-level identity (set
membership, policy checks) should call
`licenses.ParseExpression(expr).Leaves()`.

## Non-SPDX ("nonstandard") Criteria

`Expression == ""` AND `Source` is one of:

- deps.dev project license cannot be normalized → `depsdev-project-nonstandard`
- deps.dev version array fully fails normalization → `depsdev-version-raw`
- GitHub `licenseInfo` spdxId is empty / NOASSERTION and name cannot be
  normalized → `github-project-nonstandard`
- Maven POM `<licenses>` entries cannot be normalized → `maven-pom-nonstandard`

## NOASSERTION Handling (per SPDX 2.3 §A.1.5)

`Expression == "NOASSERTION"` preserves the SPDX sentinel — distinct from
`Expression == ""` which means "no upstream data". NOASSERTION is
recognized as a usable SPDX value and is **not** non-standard. It is **not**
promoted to project-level (a project-level claim must be a real license,
not a refusal-to-claim). When mixed into a multi-entry upstream array, it
is dropped from the OR-join because choosing among `"NOASSERTION OR MIT"`
adds no information beyond `"MIT"`.

## Error / Edge Cases

| Case | Behavior |
|------|----------|
| Request version fetch failure | Version licenses remain empty → fallback evaluation proceeds |
| All non-SPDX + Project also non-SPDX | Preserved as-is (no destructive replacement) |
| Promotion completed before GitHub enrichment | Subsequent GitHub SPDX does not overwrite (determinism) |
| Reserved GitHub version-level sources | Currently unused; future extension point |

## License State Matrix

### ProjectLicense States

| Expression | Raw | Source | Meaning |
|------------|-----|--------|---------|
| `""` | `""` | `""` | Pure absence: deps.dev project empty, no GitHub, no promotion |
| `""` | `non-standard...` | `depsdev-project-nonstandard` | deps.dev non-SPDX placeholder (GitHub absent or also non-SPDX) |
| `""` | `Some Custom Text` | `github-project-nonstandard` | GitHub non-SPDX (spdxId empty/NOASSERTION or cannot normalize) |
| SPDX (e.g., `MIT`) | original (e.g., `mit`) | `depsdev-project-spdx` | deps.dev SPDX; raw preserves original casing |
| SPDX | original | `github-project-spdx` | GitHub SPDX filled the gap |
| SPDX (single-leaf only) | original | `derived-from-version` | Single-leaf Version Expression promoted (Project was previously empty/non-standard); compound expressions are not promoted |
| (empty or non-SPDX) | (various) | `(empty)` / nonstandard | Promotion skipped (compound version Expression or no SPDX leaf) |

Notes:

1. `project-fallback` does not appear at the Project level (direction is Project → Version only)
2. `IsNonStandard()` covers `*-nonstandard` / `*-raw` sources
3. `Raw` preserves upstream display/audit value even when SPDX-normalized
4. `Expression` may be a compound SPDX expression (e.g., `"GPL-2.0-only WITH Classpath-exception-2.0"`) when upstream provides one; only single-leaf expressions are eligible for project-level promotion

### RequestedVersionLicense States

| Expression | Raw | Source | Meaning |
|------------|-----|--------|---------|
| `""` | `""` | `""` | No Version data; Project absent/non-SPDX and fallback not triggered |
| SPDX (single or compound) | original | `depsdev-version-spdx` | From `Version.LicenseDetails[].Spdx` (OR-joined when upstream provides multiple) |
| `""` | OR-joined raw | `depsdev-version-raw` | Upstream array fully failed normalization; Raw preserves audit trail |
| SPDX | original | `project-fallback` | Version empty / all non-SPDX + Project has SPDX |
| compound SPDX | originals | `depsdev-version-spdx` | Multiple upstream entries OR-joined into single canonical Expression |
| `""` | originals | `depsdev-version-raw` | If Project has SPDX → replaced with single `project-fallback`; otherwise preserved as-is |

Flow summary: (1) SPDX-priority collection → (2) `JoinExpressions` OR-joins survivors into a single canonical Expression → (3) if empty / Expression-empty & Project has SPDX → `project-fallback` → (4) if Project empty/non-standard & Version Expression is single-leaf canonical SPDX → promotion (`derived-from-version`).

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

`<licenses>` may contain multiple entries — they are OR-joined into a single canonical SPDX Expression via `JoinExpressions`. The dispatcher in `internal/infrastructure/integration/populate_manifest_license.go` writes the joined Expression to `ProjectLicense` (when single-leaf and Project is empty/non-standard) and to `RequestedVersionLicense` when the existing version Expression is empty or non-SPDX.

Parent POM inheritance is intentionally skipped in v1: the additional HTTP cost is rarely repaid (license declarations are typically per-artifact in Maven by convention). Revisit if telemetry shows >5% of misses are inheritance-bound.

### Override rules (any ecosystem)

| Existing `Source` | Manifest = SPDX | Manifest = non-SPDX |
|---|---|---|
| `IsZero()` | take it (`*-spdx`) | take it (`*-nonstandard`) |
| `*-nonstandard` / `*-raw` (any layer) | replace | no-op |
| Canonical SPDX (any layer) | no-op (log `license_disagreement` at WARN) | no-op |

Pre-fetch short-circuit: if both `ProjectLicense.Expression` and `RequestedVersionLicense.Expression` carry a non-empty, non-`NOASSERTION` SPDX value, the enricher skips the analysis entirely without issuing any HTTP.

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
