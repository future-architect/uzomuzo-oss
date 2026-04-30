# ADR-0018: ClearlyDefined.io as Fourth-Tier License Source

## Status

Accepted (2026-04-30)

## Context

ADR-0017 established a three-tier license-resolution chain: deps.dev → GitHub `licenseInfo` → ecosystem-native manifest (Maven POM today; NuGet `.nuspec`, PyPI metadata planned). PR #345 implemented the Maven tier and lifted overall license coverage measurably for that ecosystem, but residual gaps remain across maven / nuget / pypi.

Issue #354 measured ClearlyDefined.io's empirical hit rate against the residual "broken subset" (PURLs that survived all three tiers without a canonical SPDX result):

| Ecosystem | Sample | CD any-data | CD clean-SPDX |
|---|---:|---:|---:|
| Maven (broken subset) | 7 | 86% | 57% |
| NuGet | 15 | 73% | 67% |
| PyPI | 15 | 93% | 80% |

CD covers the residual gap because it is a curation database — Microsoft + GitHub + scancode-toolkit-backed, license-list-aware — rather than a publisher-emitted manifest. It captures multi-license expressions, parent-POM-resolved licenses, and legacy artifacts that direct manifest reads structurally cannot.

## Decision

Add CD as the **fourth and final tier** in the license-resolution chain. CD never overrides upstream canonical SPDX results; it only fills gaps where the first three tiers left ProjectLicense missing or non-standard.

### Chain order (after this ADR)

1. **deps.dev** — `Project.License` and `Version.LicenseDetails[].Spdx`.
2. **GitHub `licenseInfo`** — `enrichProjectLicenseFromGitHub` when deps.dev returned empty / non-SPDX.
3. **Ecosystem-native manifest** — Maven POM `<licenses>` (PR #345). NuGet / PyPI tiers are deferred per the umbrella tracker's post-CD measurement plan.
4. **ClearlyDefined.io** — this ADR. Cross-ecosystem curation database; final safety net.

### Why "Option C" (last-tier safety net) over A or B

The issue evaluated three placements:

- **A**: between deps.dev and GitHub
- **B**: after manifest fallback but before any version-level promotion
- **C**: strictly last, after every other tier

Option C was selected because:

1. **Authority preservation.** deps.dev, GitHub, and the package's own POM each represent first-party or near-first-party data. CD's curation pipeline is high quality but indirect (scancode-toolkit + manual curators); we never want curated data to silently overwrite a publisher's own SPDX declaration.
2. **Empirical measurement consistency.** Putting CD at the end means coverage gains are measurable as "what CD adds on top of everything else," which is the exact question issue #354's broken-subset measurement answers.
3. **Cleanup discipline.** If CD's curation drifts from upstream, the symptom is a `license_disagreement` log on a CD value losing to an earlier-tier SPDX — not a wrong answer in production data.

### Override matrix

The override rules match those of the manifest tier (PR #345) and reuse the same `applyManifestLicenses` helper:

| Existing `ProjectLicense.Source` | CD value is SPDX | CD value is non-SPDX |
|---|---|---|
| `IsZero()` | take it (`clearlydefined-spdx`) | take it (`clearlydefined-nonstandard`) |
| `*-nonstandard` (any layer) | replace with CD SPDX | no-op |
| Canonical SPDX (any layer) | **no-op**; log `license_disagreement` for audit | no-op |

`RequestedVersionLicenses` follows the same pattern: replace the slice when empty or entirely non-SPDX; otherwise leave intact.

### Score threshold: 30

CD's `licensed.score.declared` indicates curation confidence per coordinate. The empirical sample showed:

| Score range | Coordinates |
|---|---|
| 0 | `mysql-connector-java` (no usable data) |
| 45–48 | `xml-apis`, `jetty-server`, `javax.persistence` |
| 60–61 | most |
| 75 | `jaxen` |

A threshold of **30** accepts everything except the genuinely-empty case (mysql, score=0). 40+ would also drop the 45–48 tier (which carries useful data, just incomplete metadata). Going lower than 30 admits effectively-blank entries that waste downstream attention.

The threshold is a domain-level constant (`internal/infrastructure/clearlydefined/client.go:minDeclaredScore`) rather than a config flag — per project conventions, configuration knobs are added only when they must vary per deployment, which this does not.

### `licensed.declared` decision tree

CD's `declared` field has four observed shapes (issue #354 measurement):

1. **Single SPDX ID** (`Apache-2.0`) — emit one `clearlydefined-spdx`.
2. **SPDX expression** (`Apache-2.0 AND EPL-2.0`, `CDDL-1.1 OR GPL-2.0-only WITH Classpath-exception-2.0`) — parse via `licenses.ParseExpression` (PR #358); each operand becomes its own `ResolvedLicense`. SPDX leaves are `clearlydefined-spdx`; non-SPDX leaves are `clearlydefined-nonstandard`.
3. **`LicenseRef-scancode-*`** — non-SPDX by construction (the `LicenseRef-` prefix is SPDX's own way of saying "no canonical SPDX exists for this license"). Emit `clearlydefined-nonstandard` with the raw value preserved; do not attempt conversion.
4. **scancode-internal name** (e.g. `Plexus`) — when ParseExpression's leaf is non-SPDX, emit `clearlydefined-nonstandard`. The future `aliases.custom.yml` fallback can promote some of these to SPDX in a later iteration.

### Maven POM tier remains alongside CD

The umbrella tracker's "Decision points requiring human input" asked whether to drop PR #345's Maven POM tier once CD is in place. Decision: **keep it**.

- The POM tier is **deterministic** (we read the publisher's manifest directly) and provides an audit trail ("we read the manifest ourselves") that compliance reviews require.
- CD is **lazily curated** — brand-new releases may not appear in the database for hours-to-days. The POM tier covers the time CD is catching up.
- The two tiers complement each other: when both produce SPDX, the POM result wins (earlier tier); when CD has more entries (multi-license expressions the POM only listed once), it doesn't override but is logged for future enrichment opportunities.

A planned `internal/vuls-saas/uzomuzo-catalog` measurement will quantify each tier's marginal contribution and inform whether any tier becomes redundant later.

## Implementation

### Package layout

```
internal/infrastructure/clearlydefined/
  client.go         — Client struct, NewClient, FetchLicenses
  client_test.go    — httptest-driven coverage of decision tree, cache, errors
```

Mirrors the existing maven package's shape (`infrastructure/maven/client.go` + `license.go`) for review consistency.

### Wire shape

`internal/infrastructure/integration/populate_clearlydefined_license.go` houses `enrichLicenseFromClearlyDefined`, called from `purl_batch.go` immediately after `enrichLicenseFromManifest`. The method:

- Skips when the injected `cdClient` is nil (CD is opt-in for library users; the application-layer factories `NewAnalysisServiceFromConfig` and `NewFetchServiceFromConfig` wire it eagerly).
- Reuses the `needsManifestLicense` predicate so eligibility stays consistent with the manifest tier.
- Fans out fetches under its own semaphore (`maxClearlyDefinedConcurrency = 10`) so the budget does not contend with Maven Central.
- Deduplicates by `(ecosystem, namespace, name, version)` so identical coordinates issue exactly one CD call per batch.
- Applies CD results via the same `applyManifestLicenses` helper as the manifest tier.

### Telemetry

CD-specific event names (snake_case, distinct from manifest tier per the architect review):

- `license_clearlydefined_hit` (DEBUG) — successful response with at least one license written.
- `license_clearlydefined_miss` (DEBUG) — 404 or empty / below-threshold response.
- `license_clearlydefined_fetch_failed` (WARN) — transport / decode / 5xx after retry exhaustion.
- `license_clearlydefined_rate_limited` (WARN) — distinct branch when the underlying httpclient surfaces a rate-limit error.
- `license_disagreement` (WARN) — shared with the manifest tier; fires when CD's SPDX disagrees with an earlier-tier canonical SPDX.

### Caching

- 24-hour positive TTL: CD definitions are stable curation artifacts; long TTL is safe for batch scans.
- 1-hour negative TTL for 404s: CD lazily curates new releases; we don't want to retry every 404 on every analysis sharing the coordinate within a single batch, but a fresh release may appear in a later run.
- `sync.RWMutex` for the cache — read-heavy workload (most analyses share coordinates).

### Non-goals

This ADR scopes CD to **license** resolution only. Specifically:

- **EOL / vulnerability gating**: CD has facets (`security`, `licensing`, `attribution`) but uzomuzo's EOL evaluator stays driven by deps.dev + ecosystem-specific rules (advisories live in different infrastructure). No coupling.
- **Repository URL discovery**: deps.dev, GitHub, and the existing maven SCM extraction handle this. CD's `coordinates` block is not consulted.
- **Author / copyright attribution**: CD has facets for these but they are out of scope for the license-coverage initiative driving this ADR.

## Consequences

### Positive

- Coverage uplift: based on issue #354's empirical sample, CD adds ~57–80% additional SPDX rescue on the post-manifest broken subset. The umbrella tracker's post-merge measurement will quantify the actual production impact across maven / nuget / pypi.
- Cross-ecosystem reach: a single integration covers all six ecosystems CD supports, replacing what would otherwise be five separate per-ecosystem manifest fallbacks.
- Compound-license preservation: CD frequently returns SPDX expressions (`Apache-2.0 OR EPL-2.0`); PR #358's expression parser turns these into proper multi-leaf `ResolvedLicense` slices, capturing dual-licensing that single-tier sources collapse.

### Negative

- Adds a new external dependency to the license path (`api.clearlydefined.io`). The httpclient's retry / rate-limit handling absorbs transient failures; the dispatcher tolerates per-coordinate fetch failures gracefully (best-effort: log WARN, continue).
- The `LicenseSource*` closed enum grows by 2 (`LicenseSourceClearlyDefinedSPDX`, `LicenseSourceClearlyDefinedNonStandard`); every consumer of the closed-set switch (`IsNonStandard`, CSV scenario classifier) had to be updated alongside.
- Score threshold and chain position are policy choices that may need revisiting if CD's curation drifts. ADR amendments would document any change.

## Alternatives Considered

- **Do nothing; rely on more per-ecosystem manifest fallbacks (NuGet `.nuspec`, PyPI metadata).** Rejected — CD's measured hit rate exceeds direct manifest reads on the residual broken subset for every ecosystem CD covers, and adding three more per-ecosystem readers would triple the integration / test surface for marginal gains.
- **Use CD before deps.dev (Option A).** Rejected — CD's lazy curation lags new releases by hours-to-days; deps.dev is the authoritative first-party source for >70% of packages today. Demoting deps.dev would degrade coverage on fresh releases.
- **Drop the Maven POM tier in favor of CD (per the umbrella tracker's open question).** Rejected — see "Maven POM tier remains alongside CD" above. Audit trail and CD lag both argue for keeping it.
- **Bake CD's score threshold into a config flag.** Rejected per `project-conventions.md` "Mandatory Pre-Addition Checklist" — operators have no realistic reason to vary 30 across deployments.

## References

- Issue #327 — umbrella tracker, license-coverage gap measurement
- Issue #354 — CD integration design, empirical hit-rate sample
- ADR-0017 — License Source Priority and Ecosystem-Native Manifest Fallback
- PR #345 — Maven POM third-tier implementation
- PR #358 — SPDX expression parser AST (consumed by CD's compound-expression path)
- `internal/domain/analysis/license_sources.go` — closed-enum definitions
- `internal/infrastructure/integration/populate_manifest_license.go` — `applyManifestLicenses`, `needsManifestLicense` reuse
- `internal/infrastructure/integration/populate_clearlydefined_license.go` — this ADR's dispatcher
- `internal/infrastructure/clearlydefined/client.go` — Client implementation
- ClearlyDefined.io public API: <https://api.clearlydefined.io>
