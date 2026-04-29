# ADR-0017: License Source Priority and Ecosystem-Native Manifest Fallback

## Status

Accepted (2026-04-29)

## Context

License coverage in our resolved data is severely uneven across ecosystems. On a 30k+ package downstream sample:

| Ecosystem | has_license % |
|---|---:|
| composer / golang / cargo / gem / npm | 74–89% |
| pypi | 62% |
| **maven** | **38%** |
| **nuget** | **35%** |

The pre-existing pipeline had two tiers:

1. **deps.dev** populates `Project.License` and `Version.LicenseDetails[].Spdx`.
2. **GitHub `licenseInfo`** (via `enrichProjectLicenseFromGitHub`) fills the gap when deps.dev returned empty / non-SPDX.

For Maven, NuGet, and PyPI specifically, the upstream reasons for missing data are well understood (issue #327): deps.dev does not always parse multi-license `<licenses>` POMs, ignores legacy NuGet `<licenseUrl>` mappings, and silently drops PyPI Trove `classifiers`. The package's own ecosystem manifest still reaches us (`repo_url` is present on most missing records), so the data exists — we just have not been reading it.

## Decision

Add a **third tier** to the license resolution chain: an inline ecosystem-native fallback that reads the package's own manifest. Implementation lands in `internal/infrastructure/integration/populate_manifest_license.go` and runs after deps.dev populate + GitHub enrichment + PyPI summary override.

### Why inline in `IntegrationService` and not via `AnalysisEnricher`

`AnalysisEnricher` (`internal/application/analysis_service.go:26-32`) is documented to mutate **only** `Analysis.EOL` and `Analysis.Error`. Reusing it for license writes would silently break a contract that existing consumers (e.g., the catalog enricher in the private repo) rely on. The hook also fires too late: it runs at Phase 2 of `ProcessBatchPURLs`, after lifecycle assessment may have already consumed license data.

Wiring the manifest fallback as a private function inside `IntegrationService` mirrors the established `enrichPyPISummary` pattern: parallel best-effort enrichment, ecosystem-gated, no new application-layer abstraction. There is exactly one consumer; introducing a `LicenseEnricher` interface or `ManifestLicenseFetcher` abstraction would be YAGNI.

### Override rules

| Existing `Source` | Manifest = SPDX | Manifest = non-SPDX |
|---|---|---|
| `IsZero()` | take it (`*-spdx`) | take it (`*-nonstandard`) |
| `*-nonstandard` / `*-raw` (any layer) | replace | no-op |
| Canonical SPDX (any layer) | **no-op** (log `license_disagreement` at WARN) | no-op |

Manifest data is allowed to override `*-nonstandard` results from any layer (deps.dev or GitHub) because it is the ecosystem's own authoritative source for *package* license. Manifest data is **never** allowed to override a canonical SPDX from any layer; disagreement is logged for audit but not auto-resolved, since auto-flipping risks regressions and the existing SPDX result is rarely wrong when present.

### Pre-fetch short-circuit

The enricher skips an analysis entirely (no HTTP) when both:

- `ProjectLicense.IsSPDX == true`
- Every `RequestedVersionLicenses` entry has `IsSPDX == true`

This keeps the marginal HTTP cost proportional to actual coverage gaps.

### Best-effort + rate-limit policy

Manifest fetches are best-effort. Per-coordinate failures (transport, 5xx, 429, decode) are logged at WARN as `license_manifest_fetch_failed` and the analysis is left untouched — affected packages remain `*-nonstandard` rather than being lost. Per-client in-memory caches deduplicate within a single scan.

The shared `httpclient` package currently fails fast on HTTP 429 without honoring `Retry-After`. Maven Central applies CDN-layer rate limits to anonymous traffic. If 429 becomes frequent in production, follow-up work will:

1. Teach `httpclient` to honor `Retry-After` and add 429 to the retry policy.
2. Add `MaxConcurrency` / `RequestInterval` to the Maven, NuGet, and PyPI clients (mirroring the GitHub client).

Neither is required for v1: the bounded shape of `enrichPyPISummary` has been adequate in production, and graceful degradation on 429 prevents data loss.

## Scope

This ADR covers the architectural shape. The first PR implements the Maven side only:

- 2 new domain constants (`LicenseSourceMavenPOMSPDX`, `LicenseSourceMavenPOMNonStandard`)
- New `maven.Client.FetchLicenses` method extending the existing `pomModel`
- A curated URL→SPDX table (`internal/domain/licenses/url_lookup.go`) shared with the future NuGet `<licenseUrl>` consumer
- Inline dispatcher in `populate_manifest_license.go`
- Integration tests through the dispatcher

NuGet `.nuspec` and PyPI metadata fallbacks land in follow-up PRs and reuse the same dispatcher pattern + URL table.

## Consequences

### Positive

- Lifts overall coverage from ~58% to ~70–75% (issue #327 estimate; +2,000–3,000 license assignments across maven/nuget alone).
- Each tier preserves a distinct `Source` constant, so downstream analytics can attribute coverage gains to the new tier without ambiguity.
- The dispatcher remains DDD-compliant (Infrastructure layer; Application layer untouched).

### Negative

- One additional HTTP request per Maven analysis whose license is currently missing or non-standard — bounded by per-client cache and the pre-fetch short-circuit.
- The closed enum of `LicenseSource*` constants grows by 6 over the course of the issue (2 per ecosystem × 3 ecosystems). Every consumer of the closed-set switch (CSV scenario classifier, `IsNonStandard()`) must be kept in sync; reviewed at this ADR boundary.
- Manifest-vs-upstream disagreements surface only as logs in v1. If they accumulate, v2 may need to expose them as a CSV column.

## Alternatives Considered

- **Reuse `AnalysisEnricher`**: rejected (contract violation; wrong phase).
- **New `LicenseEnricher` application-layer interface**: rejected (YAGNI; one consumer).
- **Extend `populateLicenses` directly**: rejected (couples ecosystem HTTP I/O to the deps.dev populate path; harder to gate ecosystem-by-ecosystem).
- **PyPI wheel/sdist METADATA parsing instead of JSON API**: rejected (50–200 KB downloads per package vs. the JSON API already exposing the same fields).
- **Auto-generate URL→SPDX table from SPDX `seeAlso`**: deferred to follow-up. v1 ships ~30 hand-curated entries covering apache.org, opensource.org, gnu.org, mozilla.org, eclipse.org, creativecommons.org, etc. — enough for >80% of legacy URLs.

## References

- Issue #327 — coverage data and proposed fallback chain
- `docs/license-resolution.md` — current license model + source constants table
- `internal/application/analysis_service.go:26-32` — `AnalysisEnricher` contract
- `internal/infrastructure/integration/populate_summary.go` — `enrichPyPISummary` pattern this dispatcher mirrors
- `internal/infrastructure/github/client.go:919-980` — existing GitHub override rules
