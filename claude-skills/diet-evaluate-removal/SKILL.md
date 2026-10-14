---
name: diet-evaluate-removal
description: "Data-driven deep evaluation of whether a dependency is worth removing"
argument-hint: "<dependency name, e.g. github.com/pkg/errors>"
---

# Diet Removal Evaluation: $ARGUMENTS

Deep-dive evaluation of whether `$ARGUMENTS` is worth removing, using a 5-phase data-driven framework. This command picks up where `uzomuzo diet` leaves off -- diet gives you the priority ranking, this gives you the removal verdict.

**When to use**: After `uzomuzo diet` ranks a dependency and you need to decide whether to invest the effort. Works for any difficulty level -- trivial deps get a quick confirmation, hard deps get a concrete migration plan.

**Relationship to other skills**:
- `uzomuzo diet` tells you **ranking and difficulty** (automated, all dependencies at once)
- `/diet-evaluate-removal` tells you **whether it's worth removing** and **how** (LLM-powered, one dependency at a time)
- `/diet-assess-risk` tells you **how dangerous it is to keep** (LLM-powered, security focus)
- `/diet-remove` **executes the removal** (LLM-powered, implementation)

---

## Phase 1: Ingest Diet Data

Extract the target dependency's diet JSON entry. Prefer existing JSON over re-running diet.

**Option A -- existing diet JSON** (preferred):
```bash
jq '.dependencies[] | select(.name == "$ARGUMENTS")' diet.json
```

**Option B -- run diet now**:
```bash
uzomuzo diet --sbom bom.json --source . --format json | jq '.dependencies[] | select(.name == "$ARGUMENTS")'
```

### Check that coupling was actually measured

`difficulty` and `coupling_effort` are only meaningful when diet matched imports in the source tree. When source analysis is skipped, fails, or finds no import of any dependency (typically because `--source` points at the wrong directory; diet logs `no imports matched any dependency`), diet gives every entry without a `scope` `import_file_count = call_site_count = 0`, `coupling_effort = 0`, `difficulty = "trivial"`, and `is_unused = false` (`tool` and `runtime` entries get a placeholder `api_breadth = 1` and show `easy`). Here "trivial" means "not measured", not "easy to remove".

Detect it across the whole JSON, not from the one entry:

```bash
jq '[.dependencies[] | select(.scope == null)]
    | if length == 0 then "undetermined" else all(.import_file_count == 0 and (.is_unused | not)) end' diet.json
```

`true` means coupling was not measured. `"undetermined"` means every dependency has a `scope`, so this check cannot tell; look at the diet log for `no imports matched any dependency`. (`source_root` does not tell you: the CLI fills it with `.` by default.) Re-run diet with `--source` pointing at the project root the SBOM was generated from. If that is impossible, write "unmeasured" for Coupling Depth and Production Scope and say so in the verdict.

Example of the trap: `{"difficulty": "trivial", "coupling_effort": 0, "is_unused": false, "import_file_count": 0}` looks like a free removal, but if every other entry looks the same, nobody has looked at the code yet.

### Signals that are not in diet JSON

Diet JSON does not carry the OSV malicious-package check or the npm dormant-release marker. Get them from `uzomuzo scan` for the exact version in the SBOM:

```bash
uzomuzo scan "pkg:npm/example@1.2.3" --format json \
  | jq '.packages[0] | {lifecycle, reason, stable_version, malicious, malicious_check, dormant_release,
                       direct_advisory_count, transitive_advisory_count, max_cvss3_score}'
```

| Field | Meaning for this evaluation |
|-------|-----------------------------|
| `malicious` present | An OSV advisory names this version (or, with `scope: "package"`, the whole package) as malicious. Stop the evaluation -- see the first row of the Recommendation logic. |
| `malicious_check: "unknown"` | The malicious lookup failed. Say so in the verdict; do not treat it as clean. |
| `dormant_release` present | (npm) This version came out after a year with no releases. Not a finding, but a reason to confirm who published it before trusting it. The marker is shown only for 7 days after publication (`until`), so its absence on an older version proves nothing; for a version pinned recently, compare its publish date with the previous release yourself. |
| `reason` | Why scan chose the lifecycle label. Needed to interpret `Review Needed` (see Security Urgency below). |

### Required fields to extract

Display ALL of these from the diet JSON. Do NOT re-compute any of them.

| Field | What it tells you |
|-------|-------------------|
| `name` / `version` / `ecosystem` | Dependency identity |
| `purl` | Canonical package URL identifier |
| `scope` | Dependency scope: `"tool"` (build-time-only), `"runtime"` (reflection/ServiceLoader-loaded) |
| `rank` / `priority_score` | Where this dep sits in the removal priority list |
| `difficulty` | trivial / easy / moderate / hard |
| `exclusive_transitive` / `total_transitive` | Deps removed together / total transitive count |
| `stays_as_indirect` | Whether it remains after removing direct usage |
| `indirect_via` | Which other deps pull it in transitively |
| `import_file_count` | Number of files importing this dep |
| `call_site_count` | Total call sites across all files |
| `api_breadth` | Number of distinct APIs used |
| `symbols` | Exact API surface used (function/type names) |
| `import_files` | Exact file paths that import this dep |
| `has_blank_import` / `has_dot_import` / `has_wildcard_import` | Special import patterns |
| `is_unused` | Whether diet found zero imports (always `false` for `tool` / `runtime` scope, and for every entry when coupling was not measured) |
| `lifecycle` | Active / Stalled / Legacy-Safe / EOL-Confirmed / EOL-Effective / EOL-Scheduled / Archived / Review Needed |
| `has_vulnerabilities` / `vulnerability_count` / `max_cvss_score` | Advisories on the package's **latest release, including advisories in its dependencies** -- not on the version in your SBOM (see below) |
| `overall_score` | OpenSSF Scorecard overall score (0-10) |
| `graph_impact` / `coupling_effort` / `health_risk` | Score components (each 0.0-1.0) |

### How to read the health fields

- **Vulnerability fields describe the latest release, including its dependencies.** Diet takes them from the release uzomuzo picks as the newest (the stable release; failing that, the highest or pre-release version), not from the pinned version, and it counts advisories in that release's own dependencies too. Split them with scan's `direct_advisory_count` and `transitive_advisory_count`:
  - Direct advisories on the latest release mean "even upgrading does not fix these" -- a strong reason to remove or replace.
  - Transitive-only advisories (`direct_advisory_count` 0) are in a package this one depends on. They argue for a dependency bump upstream, not for removing this package. `max_cvss_score` may come from them.
  - For the version you actually ship, query OSV for that exact version (`osv-scanner`, `govulncheck`, `npm audit`, `pip-audit`). If the pinned version is vulnerable but the latest release is clean, the fix is an upgrade, not a removal.
- **`Archived` is diet's own label.** Diet writes `Archived` whenever the source repository is archived, overriding the lifecycle label. Check scan's `stable_version` and its release date before concluding fixes stopped: packages that moved into a monorepo keep publishing from the new repository while the old one is archived. `uzomuzo scan` reports the same package as `Stalled` unless the registry also gives an explicit end-of-life signal ([ADR-0020](https://github.com/future-architect/uzomuzo-oss/blob/main/docs/adr/0020-archived-registry-liveness.md)). Archived alone does not mean "already broken".
- **`Review Needed` now has specific causes**, visible in scan's `reason`: too little data to classify (`Insufficient data for assessment`, `Commit data missing`); every published release is yanked ([ADR-0022](https://github.com/future-architect/uzomuzo-oss/blob/main/docs/adr/0022-all-releases-yanked-is-not-eol.md)); or (npm) a release burst across release lines after a long silence ([ADR-0026](https://github.com/future-architect/uzomuzo-oss/blob/main/docs/adr/0026-dormant-release-burst.md)). The last two are not "unknown" -- see Security Urgency in Phase 4.
- **`health_risk` is 1.0 for a malicious version**, but 1.0 can also come from EOL plus a high CVSS score. Use scan's `malicious` field, not `health_risk`, to tell them apart.
- **`coupling_effort` formula** (for explaining a difficulty label): `0.4 * L(import_file_count, 5) + 0.4 * L(call_site_count, 20) + 0.2 * L(api_breadth, 10)`, where `L(x, m)` is a logistic curve that reaches 0.5 at `x = m`. Labels: exactly `0` is trivial, `< 0.25` easy, `< 0.6` moderate, otherwise hard. Effort is exactly 0 only when the dependency is unused, is used through a blank import alone, or coupling was not measured -- so "trivial" never means "a few call sites".

### Data quality check

Before proceeding, flag any of these patterns -- they indicate diet's coupling data may be unreliable:

- `is_unused: true` -- `is_unused` only means diet found no import. Dependencies loaded through configuration, plugins, reflection, ServiceLoader, or build commands still show as unused unless diet knows them (it recognizes Go `tool` directives and a fixed list of Java runtime libraries such as JDBC drivers and logging backends). Before treating an unused dependency as removable, search configuration and build files for its name (`*.yml`, `*.properties`, `META-INF/services/*`, `spring.factories`, `package.json` scripts and config files, `Makefile`, CI workflows).
- `scope: "tool"` + `import_file_count: 0` -- Expected for build-time-only tool deps. Not an IBNC pattern. These have zero runtime imports by design.
- `scope: "runtime"` + `import_file_count: 0` -- Expected for runtime-mechanism deps (JDBC drivers, logging backends, Spring auto-config). Loaded via reflection/ServiceLoader/classpath, not static imports.
- `has_blank_import: true` + `call_site_count: 0` -- The blank import IS the usage (Go DB drivers, JS polyfills). Coupling is underestimated.
- `has_wildcard_import: true` -- All symbols are in scope. `api_breadth` may undercount.
- `has_dot_import: true` -- Broadly coupled; symbols lack package qualifier.
- `import_file_count > 0` + `call_site_count: 0` (without blank/dot/wildcard flags) -- Possible IBNC pattern (type-only usage, annotation processing, framework DI, config-driven plugins). Investigate before trusting "0 calls." Note: distinct from the blank-import pattern above -- blank imports typically produce `call_site_count = 1`, not 0.

---

## Phase 2: Usage Classification

Classify each entry in `import_files` by its role in the project. This is the single most important step -- it determines the **effective difficulty** of removal.

### Classification categories

| Category | Path patterns | Impact |
|----------|--------------|--------|
| **Production** | `src/`, `lib/`, `internal/`, `pkg/`, `cmd/`, `app/` (not under test/) | Full migration required |
| **Test** | `*_test.go`, `test/`, `tests/`, `__tests__/`, `spec/`, `*.test.ts` | Can delete or swap independently |
| **CI / Infrastructure** | `.github/`, `scripts/`, `ci/`, `Makefile`, `build/` | Often trivially replaceable |
| **Example / Demo** | `examples/`, `example/`, `demo/`, `sample/` | Can delete or update separately |
| **Generated** | Files with `// Code generated` header, `*_gen.go`, `*.pb.go` | Re-run generator with replacement |
| **Vendored** | `vendor/`, `node_modules/`, `third_party/` | Exclude from analysis -- copies of the dep, not usage |

### Output format

```
Usage Classification for {name}:
  Production:     {N} files  [{file list}]
  Test:           {N} files  [{file list}]
  CI/Infra:       {N} files  [{file list}]
  Example:        {N} files  [{file list}]
  Generated:      {N} files  [{file list}]

  Effective scope: {N} production files (out of {total} total)
```

### Effective difficulty adjustment

| diet difficulty | All files are test/CI/example | Adjusted difficulty |
|----------------|------------------------------|---------------------|
| hard | Yes | easy (no production impact) |
| moderate | Yes | trivial |
| easy/trivial | -- | unchanged |

If ALL import files are non-production, note: **"Zero production code impact."**

For `scope: "runtime"` dependencies, `import_files` is empty but the dependency runs in production. Classify the files that wire it instead (configuration, connection strings, `META-INF/services/*`, `Class.forName` calls) as Production, and never treat an empty list as "zero production files" for Production Scope or Effort.

---

## Phase 3: Feasibility Analysis

Two checks that diet cannot automate: API leakage (a binary gate) and symbol replaceability (requires domain knowledge).

### 3a. API Leakage Check (GATE)

**Critical question**: Do any types from this dependency appear in EXPORTED identifiers?

- If **yes** -- removing it is a **breaking change** for downstream consumers. Flag this immediately. The verdict must account for the breaking change cost.
- If **no** -- internal swap, no API impact.

Look for the package's types in **exported function signatures** (parameter types, return types), **exported struct field types**, and **exported interface method signatures**. Internal usage within function bodies is NOT API leakage.

Check approach by ecosystem:

| Ecosystem | What to check |
|-----------|---------------|
| Go | Exported func signatures, struct fields, and interface methods that reference the dep's types. Search non-test files for the dep's package name in type positions. |
| npm/TS | `export ... from '{pkg}'` re-exports, or exported types/interfaces using the dep's types. |
| Python | Re-exports in `__init__.py` files: `from {pkg} import ...` that are part of the public API. |
| Java/Maven | Public class signatures, method parameters, return types, or field types referencing the dep's packages in non-test source sets. |

### 3b. Symbol Migration Map

Using the `symbols` list from diet, classify each symbol's replaceability. This is where the LLM adds the most value.

```
Symbol Migration Map for {name}:

| Symbol | Category | Replacement | Notes |
|--------|----------|-------------|-------|
| {sym1} | stdlib   | {specific function} | Available since {version} |
| {sym2} | existing-dep | {dep already in project} | Already in go.mod/package.json |
| {sym3} | self-impl | {N}-line implementation | Non-crypto, well-defined |
| {sym4} | no-replacement | -- | Crypto/protocol, keep or find alternative lib |

Summary: {N}/{total} stdlib, {N} existing-dep, {N} self-impl, {N} no-replacement.
```

**Rules for symbol classification**:
- **stdlib**: Prefer this. Name the specific function (e.g., `os.UserHomeDir()`, `slices.SortFunc()`). Note the minimum language version required.
- **existing-dep**: A dependency already in the project provides this. No new dependency added.
- **self-impl**: Only for non-crypto, non-protocol, non-security functionality. Estimate lines of code including tests.
- **no-replacement**: The symbol has no viable alternative. This blocks removal unless the feature can be dropped.

**Special cases**:
- If `is_unused: true` -- skip this step. The dep can simply be deleted.
- If `has_blank_import: true` and `call_site_count <= 1` -- the import itself is the usage (driver registration, polyfill). Check what `init()` does; the replacement is typically a different driver package, not a code change. Score Replaceability as High if a drop-in alternative driver/polyfill exists, Low if not.
- If `symbols` is empty but `import_file_count > 0` (without blank/dot/wildcard flags) -- diet's coupling analysis may be incomplete. Manually inspect the import files to determine actual API usage before scoring Replaceability. Note the gap in the rationale.

### 3c. Verification Cost

With a coding agent doing the edits, rewriting call sites is rarely the expensive part of a removal. The expensive part is proving the replacement behaves the same. That cost depends on whether the code that calls the dependency is already under test.

For each **production** file from Phase 2, check whether a test exercises it (a sibling `*_test.go`, `test_*.py`, `*.test.ts`, `src/test/...` class, or a test that imports the file's package/module). Record:

```
Verification Cost for {name}:
  Production import files with tests:    {N}  [{file list}]
  Production import files without tests: {N}  [{file list}]
  Behavior-sensitive symbols:            {symbols whose replacement may differ at edge cases -- parsing, formatting, time, network addresses, error wrapping, encoding}
```

Example: three production files call `iplib.NewNet4(...)`, and none has a test. Replacing it with `net/netip` takes minutes to type, but `/31` and `/32` prefixes behave differently between the two libraries, so the removal must start by writing tests against the current behavior. That is where the time goes, and Effort below accounts for it.

---

## Phase 4: 6-Axis Quantitative Scoring

Rate each axis High/Med/Low using the data gathered in Phases 1-3. Each axis is anchored to concrete data -- no guesswork.

| Axis | Data Source | High (favors removal) | Medium | Low (discourages removal) |
|------|-----------|------|--------|------|
| **Transitive Cleanup** | `exclusive_transitive` | >= 10 exclusive deps | 1-9 exclusive | 0 exclusive |
| **Production Scope** | Phase 2 classification | 0 production files (all test/CI/example) | < 50% production files | >= 50% production files |
| **Coupling Depth** | `coupling_effort`, `import_file_count`, `call_site_count` | `coupling_effort` < 0.25 (trivial/easy) | 0.25 - 0.6 (moderate) | >= 0.6 (hard, deeply wired) |
| **Replaceability** | Phase 3b symbol map | > 80% symbols have stdlib/existing-dep replacement | 50-80% replaceable | < 50%, or crypto/protocol involved |
| **Security Urgency** | `has_vulnerabilities`, `max_cvss_score`, `lifecycle`, scan `direct_advisory_count` / `reason` / `dormant_release` | Direct advisories on the latest release with CVSS >= 7.0 (no upstream fix), or lifecycle EOL-Confirmed/EOL-Effective, or scan `reason` starting `All releases yanked on`, or `dormant_release.kind = "burst_across_lines"` (whatever the label) | Direct advisories with CVSS 4.0-6.9, or lifecycle Stalled/EOL-Scheduled/Archived, or `Review Needed` for lack of data, or `dormant_release.kind = "fresh"` | No direct advisories on the latest release, or only ones with CVSS below 4.0 (transitive-only counts here), lifecycle Active/Legacy-Safe. A direct advisory with no CVSS score counts as Medium until its severity is looked up |
| **Cascade Potential** | `exclusive_transitive`, project knowledge | Removing unblocks 3+ further removals | Unblocks 1-2 | Standalone, no cascade |

### Scoring overrides

- **Coupling Depth override**: When Phase 2 shows ALL import files are non-production (test/CI/example), override Coupling Depth to **High** regardless of `coupling_effort`. Test/CI refactoring is low-risk regardless of call count.
- **Transitive Cleanup caveat**: When `stays_as_indirect: true`, `exclusive_transitive` may overestimate actual cleanup -- some "exclusive" transitives may remain reachable via the indirect path. Note this uncertainty in the rationale.
- **Unmeasured coupling**: When Phase 1 found that coupling was not measured, write "unmeasured" for Coupling Depth and Production Scope instead of a rating. Do not let a "trivial" label from an unmeasured run count as High.
- **Security Urgency is about removal, not upgrade**: If the pinned version has advisories but the latest release is clean, the remedy is an upgrade. Rate Security Urgency from the latest release's **direct** advisories, and mention the upgrade in the rationale. When both direct and transitive advisories exist, `max_cvss_score` may belong to a transitive one; look up the direct advisories' scores on OSV before rating.

### Scoring output

```
6-Axis Scores:
  Transitive Cleanup:  {H/M/L} -- {exclusive_transitive} exclusive deps ({reasoning})
  Production Scope:    {H/M/L} -- {N}/{total} files are production ({reasoning})
  Coupling Depth:      {H/M/L} -- effort={coupling_effort:.2f}, {call_site_count} calls across {api_breadth} APIs
  Replaceability:      {H/M/L} -- {N}/{total} symbols replaceable ({reasoning})
  Security Urgency:    {H/M/L} -- {lifecycle}, {vulnerability_count} vulns, max CVSS {max_cvss_score}
  Cascade Potential:   {H/M/L} -- {reasoning}

Verification Cost (feeds Effort, not an axis):
  {N}/{production files} production import files have tests; behavior-sensitive symbols: {list or "none"}
```

---

## Phase 5: Verdict

Synthesize all phases into a structured recommendation. Use the exact field names from diet JSON.

```
### Verdict: {name}@{version}

**Diet Data**:
  rank: #{rank}
  priority_score: {priority_score:.3f}
  difficulty: {difficulty}
  ecosystem: {ecosystem}

**Graph**:
  exclusive_transitive: {exclusive_transitive}
  total_transitive: {total_transitive}
  stays_as_indirect: {stays_as_indirect}
  indirect_via: {indirect_via or "none"}

**Coupling**:
  {import_file_count} files ({N} production, {N} test, {N} other)
  {call_site_count} calls across {api_breadth} APIs
  symbols: {symbols count} ({N} stdlib-replaceable, {N} self-impl, {N} no-replacement)

**Health**:
  lifecycle: {lifecycle} {-- scan reason: {reason} if Review Needed}
  vulnerabilities on latest release: {vulnerability_count} ({direct_advisory_count} direct, {transitive_advisory_count} via its dependencies; max CVSS {max_cvss_score})
  vulnerabilities on pinned version: {from OSV, or "not checked"}
  malicious check: {clean / flagged: {advisory_id} / unknown}
  dormant release: {none / {silent_days} days of silence before this version}

**API Leakage**: {Yes (BREAKING CHANGE) / No}
**Verification Cost**: {N}/{production files} production import files have tests

**6-Axis Summary**:
  {axis}: {H/M/L}  (x6, from Phase 4)

**Recommendation**: REMOVE / DEFER / KEEP / UPGRADE / REPLACE VERSION NOW
**Effort**: Trivial (<1h) / Small (1-4h) / Medium (1-3d) / Large (1w+)
**Priority**: S (do now) / A (this sprint) / B (when convenient) / C (keep for now)
**Rationale**: {1-2 sentences -- why this recommendation, citing the key axis scores}
```

### Recommendation logic

Rules are evaluated **top-to-bottom; first match wins.** If multiple rows could match, the earlier row takes priority.

| Condition | Recommendation |
|-----------|---------------|
| scan `malicious` present | REPLACE VERSION NOW, Priority S. This is incident response, not a removal decision: move to a version the advisory does not name (or, for `scope: "package"`, remove the package), and run `/diet-assess-risk` to judge what the malicious code could have reached. Skip the rest of this table |
| `is_unused: true`, and a search of configuration and build files finds no reference (see Data quality check) | REMOVE, Trivial, Priority S |
| Pinned version has advisories, the pinned version is older than the latest release, an OSV check of the latest release (with the dependency versions it resolves to in your project) no longer shows them, and lifecycle is Active or Legacy-Safe | UPGRADE, Priority A. Stop here; name in the rationale any axis that would still argue for removal later |
| API leakage = Yes AND Security Urgency = High | REMOVE with BREAKING CHANGE warning -- security overrides API stability. Rationale must document the breaking change and recommend a major version bump or deprecation timeline. Escalate to `/diet-assess-risk` |
| API leakage = Yes | DEFER (needs major version bump or deprecation period) |
| Replaceability = Low (crypto/protocol) | KEEP -- find an alternative maintained library rather than self-implementing. If Security Urgency is also High, escalate to `/diet-assess-risk` for full risk analysis. Never recommend self-implementing crypto/protocol |
| Coupling Depth or Production Scope is "unmeasured" | DEFER -- re-run diet with the correct `--source`, or read the import sites by hand, then evaluate again. Exception: if Security Urgency is High, REMOVE as for the High-urgency row below and note the missing coupling data |
| All 6 axes High or Med | REMOVE |
| 1-2 axes Low, rest High/Med | REMOVE -- note the Low axes as caveats in rationale |
| Security Urgency = High, others mixed | REMOVE (prioritize security). Suggest `/diet-assess-risk` for full analysis |
| `stays_as_indirect: true`, coupling low | REMOVE (still valuable: delegates version management, unblocks future cleanup) |
| >= 3 axes Low | KEEP |

### Effort derivation

| Effort | Criteria |
|--------|----------|
| Trivial (<1h) | `is_unused: true`, OR 0 production files, OR all symbols stdlib-replaceable with <= 3 files |
| Small (1-4h) | <= 5 production files AND > 80% symbols replaceable |
| Medium (1-3d) | 6-15 production files, OR 50-80% symbols replaceable, OR API leakage requiring deprecation |
| Large (1w+) | > 15 production files, OR < 50% symbols replaceable, OR crypto/protocol replacement needed |

After picking a row, **raise Effort one level** when more than half of the production import files have no tests and the used symbols include behavior-sensitive ones (Phase 3c). Those tests must be written before the swap, and they take longer than the swap itself. Do not lower Effort for having an agent do the edits -- the time estimate is for the whole removal including review and verification.

### When to recommend further analysis

- Security Urgency = High --> "Consider running `/diet-assess-risk {name}` for full data-flow analysis."
- Recommendation = REMOVE, Effort >= Medium --> "Use `/diet-remove {name} --pr` for guided implementation."
- Recommendation = REMOVE, external project --> "Use `/diet-remove {name} --repo {owner/repo}` to file an issue."

---

## Important rules

- **Start from diet's output.** Never re-compute fields that diet already provides. But check that coupling was measured (Phase 1) before trusting `difficulty`, and fetch the malicious and dormant-release signals from `uzomuzo scan`, because diet JSON does not carry them.
- **Know which version a number describes.** Diet's vulnerability fields describe the latest release and include its dependencies' advisories; the version you ship may be better or worse.
- **Classify before scoring.** Usage classification (Phase 2) changes everything -- always do it before the 6-axis scoring.
- **API leakage is a gate, not a spectrum.** If exported types depend on this package, flag it immediately.
- **Be concrete about replacements.** Don't say "use stdlib" -- name the specific function, note the version requirement, show the migration pattern.
- **Respect IBNC patterns.** If `has_blank_import` is true and `call_site_count` is 0, the import IS the usage. Don't call it "unused."
- **This works for any language.** The `ecosystem` field in diet JSON tells you which language. Use the ecosystem-specific commands in Phase 3a.
- **One dependency at a time.** This skill is for deep single-dep analysis. For several dependencies, run one evaluation per dependency -- in parallel subagents if your environment has them -- and merge the verdict blocks afterwards. Keep each evaluation independent; a shared context tends to copy one dependency's conclusions onto the next.
