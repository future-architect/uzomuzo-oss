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

### Required fields to extract

Display ALL of these from the diet JSON. Do NOT re-compute any of them.

| Field | What it tells you |
|-------|-------------------|
| `name` / `version` / `ecosystem` | Dependency identity |
| `purl` | Canonical package URL identifier |
| `scope` | Dependency scope (e.g., `"tool"` for build-time-only deps) |
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
| `is_unused` | Whether diet detected zero imports |
| `lifecycle` | Active / Stalled / Legacy-Safe / EOL-Confirmed / EOL-Effective / EOL-Scheduled / Archived / Review Needed |
| `has_vulnerabilities` / `vulnerability_count` / `max_cvss_score` | Security signals |
| `overall_score` | Composite health score (OpenSSF Scorecard) |
| `graph_impact` / `coupling_effort` / `health_risk` | Score components |

### Data quality check

Before proceeding, flag any of these patterns -- they indicate diet's coupling data may be unreliable:

- `scope: "tool"` + `import_file_count: 0` -- Expected for build-time-only tool deps. Not an IBNC pattern. These have zero runtime imports by design.
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

---

## Phase 4: 6-Axis Quantitative Scoring

Rate each axis High/Med/Low using the data gathered in Phases 1-3. Each axis is anchored to concrete data -- no guesswork.

| Axis | Data Source | High (favors removal) | Medium | Low (discourages removal) |
|------|-----------|------|--------|------|
| **Transitive Cleanup** | `exclusive_transitive` | >= 10 exclusive deps | 1-9 exclusive | 0 exclusive |
| **Production Scope** | Phase 2 classification | 0 production files (all test/CI/example) | < 50% production files | >= 50% production files |
| **Coupling Depth** | `coupling_effort`, `import_file_count`, `call_site_count` | `coupling_effort` < 0.25 (trivial/easy) | 0.25 - 0.6 (moderate) | >= 0.6 (hard, deeply wired) |
| **Replaceability** | Phase 3b symbol map | > 80% symbols have stdlib/existing-dep replacement | 50-80% replaceable | < 50%, or crypto/protocol involved |
| **Security Urgency** | `has_vulnerabilities`, `max_cvss_score`, `lifecycle` | CVSS >= 7.0, or lifecycle EOL-Confirmed/EOL-Effective/Archived | CVSS 4.0-6.9, or lifecycle Stalled/EOL-Scheduled | No vulns, lifecycle Active/Legacy-Safe |
| **Cascade Potential** | `exclusive_transitive`, project knowledge | Removing unblocks 3+ further removals | Unblocks 1-2 | Standalone, no cascade |

### Scoring overrides

- **Coupling Depth override**: When Phase 2 shows ALL import files are non-production (test/CI/example), override Coupling Depth to **High** regardless of `coupling_effort`. Test/CI refactoring is low-risk regardless of call count.
- **Transitive Cleanup caveat**: When `stays_as_indirect: true`, `exclusive_transitive` may overestimate actual cleanup -- some "exclusive" transitives may remain reachable via the indirect path. Note this uncertainty in the rationale.

### Scoring output

```
6-Axis Scores:
  Transitive Cleanup:  {H/M/L} -- {exclusive_transitive} exclusive deps ({reasoning})
  Production Scope:    {H/M/L} -- {N}/{total} files are production ({reasoning})
  Coupling Depth:      {H/M/L} -- effort={coupling_effort:.2f}, {call_site_count} calls across {api_breadth} APIs
  Replaceability:      {H/M/L} -- {N}/{total} symbols replaceable ({reasoning})
  Security Urgency:    {H/M/L} -- {lifecycle}, {vulnerability_count} vulns, max CVSS {max_cvss_score}
  Cascade Potential:   {H/M/L} -- {reasoning}
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
  lifecycle: {lifecycle}
  vulnerabilities: {vulnerability_count} (max CVSS {max_cvss_score})

**API Leakage**: {Yes (BREAKING CHANGE) / No}

**6-Axis Summary**:
  {axis}: {H/M/L}  (x6, from Phase 4)

**Recommendation**: REMOVE / DEFER / KEEP
**Effort**: Trivial (<1h) / Small (1-4h) / Medium (1-3d) / Large (1w+)
**Priority**: S (do now) / A (this sprint) / B (when convenient) / C (keep for now)
**Rationale**: {1-2 sentences -- why this recommendation, citing the key axis scores}
```

### Recommendation logic

Rules are evaluated **top-to-bottom; first match wins.** If multiple rows could match, the earlier row takes priority.

| Condition | Recommendation |
|-----------|---------------|
| `is_unused: true` | REMOVE, Trivial, Priority S |
| API leakage = Yes AND Security Urgency = High | REMOVE with BREAKING CHANGE warning -- security overrides API stability. Rationale must document the breaking change and recommend a major version bump or deprecation timeline. Escalate to `/diet-assess-risk` |
| API leakage = Yes | DEFER (needs major version bump or deprecation period) |
| Replaceability = Low (crypto/protocol) | KEEP -- find an alternative maintained library rather than self-implementing. If Security Urgency is also High, escalate to `/diet-assess-risk` for full risk analysis. Never recommend self-implementing crypto/protocol |
| All 6 axes High or Med | REMOVE |
| 1-2 axes Low, rest High/Med | REMOVE -- note the Low axes as caveats in rationale |
| >= 3 axes Low | KEEP |
| Security Urgency = High, others mixed | REMOVE (prioritize security). Suggest `/diet-assess-risk` for full analysis |
| `stays_as_indirect: true`, coupling low | REMOVE (still valuable: delegates version management, unblocks future cleanup) |

### Effort derivation

| Effort | Criteria |
|--------|----------|
| Trivial (<1h) | `is_unused: true`, OR 0 production files, OR all symbols stdlib-replaceable with <= 3 files |
| Small (1-4h) | <= 5 production files AND > 80% symbols replaceable |
| Medium (1-3d) | 6-15 production files, OR 50-80% symbols replaceable, OR API leakage requiring deprecation |
| Large (1w+) | > 15 production files, OR < 50% symbols replaceable, OR crypto/protocol replacement needed |

### When to recommend further analysis

- Security Urgency = High --> "Consider running `/diet-assess-risk {name}` for full data-flow analysis."
- Recommendation = REMOVE, Effort >= Medium --> "Use `/diet-remove {name} --pr` for guided implementation."
- Recommendation = REMOVE, external project --> "Use `/diet-remove {name} --repo {owner/repo}` to file an issue."

---

## Important rules

- **Start from diet's output.** Never re-compute fields that diet already provides.
- **Classify before scoring.** Usage classification (Phase 2) changes everything -- always do it before the 6-axis scoring.
- **API leakage is a gate, not a spectrum.** If exported types depend on this package, flag it immediately.
- **Be concrete about replacements.** Don't say "use stdlib" -- name the specific function, note the version requirement, show the migration pattern.
- **Respect IBNC patterns.** If `has_blank_import` is true and `call_site_count` is 0, the import IS the usage. Don't call it "unused."
- **This works for any language.** The `ecosystem` field in diet JSON tells you which language. Use the ecosystem-specific commands in Phase 3a.
- **One dependency at a time.** This skill is for deep single-dep analysis. For batch evaluation, run it in a loop.
