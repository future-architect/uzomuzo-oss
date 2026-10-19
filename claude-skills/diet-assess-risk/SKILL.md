---
name: diet-assess-risk
description: "Deep-dive security risk analysis for a dependency flagged by uzomuzo diet"
argument-hint: "<PURL or module path, or 'top N'>"
---

# Diet Risk Assessment: $ARGUMENTS

Analyze the security risk of keeping a dependency identified by `uzomuzo diet`. This command performs deep data-flow analysis that diet's automated scoring cannot do.

**When to use**: After `uzomuzo diet` flags a dependency as EOL/Stalled/Archived/Review Needed, or scan flags a malicious or dormant-release version. Dependencies diet reports as unused (`is_unused: true`, with coupling actually measured) usually just need removing; a "trivial" label alone is not enough, because it is also what unmeasured coupling looks like.

**Relationship to diet skills**:
- `uzomuzo diet` tells you **what** to remove and **how hard** (automated, broad)
- `/diet-assess-risk` tells you **how dangerous it is to keep** (LLM-powered, deep) -- **this skill**
- `/diet-evaluate-removal` tells you **whether removal is worth it** and **how to replace** (LLM-powered, per-dep)
- `/diet-remove` **executes the removal** (issue or PR)

Do NOT assess replacement options or removal feasibility here -- that is `/diet-evaluate-removal`'s job. This skill answers one question: **"If we keep this dependency unchanged for the next 12 months, what is the realistic security exposure?"**

## Phase 0: Input normalization

Determine input mode and normalize to a diet entry before analysis.

### Mode A: Diet JSON available

Extract the target dependency's entry from the diet JSON:

```bash
uzomuzo diet --sbom bom.json --source . --format json | jq '.dependencies[] | select(.name == "TARGET")'
```

Or read from a saved diet JSON file. All fields are available.

### Mode B: PURL only (no diet JSON)

Run `uzomuzo scan <purl>` to get health signals. Mark coupling fields (`import_file_count`, `call_site_count`, `api_breadth`, `symbols`, `import_files`) as "unavailable". Proceed with Phase 1 Steps 1-2 and Phase 3 only. Skip Phase 1 Steps 3-4 (no coupling data to filter) and Phase 2 (no source code). When classifying threat profile in Step 2, treat absent `import_file_count` as "assume > 0" (conservative — do not default to SUPPLY-CHAIN-ONLY). In the verdict, omit the "Security-relevant coupling" line and note "Source coupling data unavailable" in Limitations.

### Mode C: "top N"

Sort diet entries by `health_risk * (1 - coupling_effort)` descending (both fields are 0.0-1.0 normalized). This prioritizes dependencies that are both unhealthy and actionable — high health risk combined with lower coupling effort gives the best risk-reduction ROI. Run Phase 0-3 for each of the top N entries (N is user-specified, default 5), then produce the summary table.

If coupling was not measured -- every entry without a `scope` has `import_file_count = 0` and `is_unused = false`. That happens when source analysis is skipped, fails, or matches no import because `--source` points at the wrong directory; the top-level `source_root` cannot tell you, because the CLI fills it with `.` by default. Then every `coupling_effort` is 0 and this sort degenerates to `health_risk` alone. Say so above the summary table, and treat each entry as Mode B for coupling.

Each entry's assessment is independent. If your environment can run subagents, assess the N entries in parallel, one subagent per dependency, each returning only its verdict block; then build the summary table from the returned blocks. Separate contexts also stop one dependency's data flows from leaking into another's scenarios.

### Signals that are not in diet JSON (all modes)

Diet JSON does not carry the OSV malicious-package check or the npm dormant-release marker, and its vulnerability fields describe the package's latest release (including advisories in that release's own dependencies) rather than the version you ship. Before the early exits, fetch these for the exact version in the SBOM:

```bash
uzomuzo scan "pkg:npm/example@1.2.3" --format json \
  | jq '.packages[0] | {lifecycle, reason, stable_version, malicious, malicious_check, dormant_release,
                       direct_advisory_count, transitive_advisory_count, max_cvss3_score}'
```

uzomuzo runs the malicious-package check only for npm, PyPI, crates.io, Go, and Maven. For other ecosystems (NuGet, GitHub Actions, ...) neither `malicious` nor `malicious_check` appears, which means "not checked", not "clean".

Then check the pinned version's advisories with an OSV-backed tool (`osv-scanner`, `govulncheck`, `npm audit`, `pip-audit`). Record both: "advisories on the pinned version" and "advisories still present on the latest release" (diet's `vulnerability_count`, split by scan into `direct_advisory_count` and `transitive_advisory_count`). Direct advisories on the latest release mean upgrading does not help; transitive-only ones live in a package this one depends on, and the data-flow analysis below should target that package's role, not this one's.

### Early exits

- If scan returns a `malicious` object: **Stop the normal flow.** Report "Malicious version (advisory {advisory_id}) -- Remove urgently." Data-flow analysis is the wrong question here: malicious packages usually act when installed or imported, not through the call sites diet counts. Instead list where this version was installed (developer machines, CI runners, build images, production) and which secrets those environments hold, because those are what to rotate. With `scope: "package"`, every version is affected; with `scope: "version"`, moving to an unaffected version is enough. Link the advisory.
- If `is_unused` is `true` (and not malicious): `is_unused` only means diet found no import. Dependencies loaded through configuration, plugins, reflection, ServiceLoader, or build commands still show as unused unless diet knows them (it recognizes Go `tool` directives and a fixed list of Java runtime libraries such as JDBC drivers and logging backends). Before treating an unused dependency as removable, search for its name in the source -- including `import type` / `export type ... from` lines, which diet ignores, and places diet does not read (a `build/`, `dist/`, or `target/` directory next to the manifest of a tool that writes there, such as `dist/` next to `package.json` or `target/` next to `pom.xml`; call sites in files over 1 MiB (only their imports are read); file types it does not parse) -- and in configuration and build files (`*.yml`, `*.properties`, `META-INF/services/*`, `spring.factories`, `package.json` scripts and config files, `Makefile`, CI workflows). If the search finds wiring, do not stop: treat the dependency as `scope: "runtime"` from here on and read the wiring files in Phase 2. If it finds nothing, report "Unused in source -- no call-site data flow. Remove it." Add one line on install-time exposure (the first bullet of Scenario B) -- an installed but unused package can still run code on every machine that installs it, which is a reason to remove it rather than to ignore it. Then stop.
- If `lifecycle` is `Active`, `has_vulnerabilities` is `false`, the pinned version has no advisories, scan shows no `malicious` and no `dormant_release`, `malicious_check` is not `"unknown"`, the ecosystem is one the malicious check covers, and the pinned version did not come out after a long silence: **Stop.** (The `dormant_release` marker is shown only for 7 days after publication, so for an older pin compare its publish date with the previous release's yourself.) Report "Maintained, no known vulnerabilities. No immediate risk." This skill is for unhealthy dependencies.

## Phase 1: Threat surface inventory

This phase uses **only diet JSON fields** -- no code reading. It classifies the threat surface and produces scoping decisions for Phase 2.

### Step 1: Record diet facts

Extract the full diet entry. These fields will appear verbatim in the verdict's "Diet Facts" section:

| Field | JSON key | Use |
|-------|----------|-----|
| Identity | `purl`, `name`, `version`, `ecosystem` | Package identification |
| Ranking | `rank`, `priority_score`, `overall_score` | Diet's automated ranking (`rank` is position among direct deps) |
| Health | `health_risk`, `lifecycle`, `has_vulnerabilities`, `vulnerability_count`, `max_cvss_score` | Maintenance status; vulnerability fields are for the latest release and include its dependencies' advisories |
| Graph | `graph_impact`, `exclusive_transitive`, `total_transitive` | Blast radius |
| Coupling | `coupling_effort`, `difficulty`, `import_file_count`, `call_site_count`, `api_breadth` | Integration depth |
| Coupling detail | `symbols`, `import_files` | Specific APIs used and files importing them (Phase 2 starting points) |
| Persistence | `stays_as_indirect`, `indirect_via` | Whether risk is eliminable |
| Import style | `has_blank_import`, `has_dot_import`, `has_wildcard_import` | Coupling accuracy caveats |
| Scope | `scope` | `"tool"` = build-time only (Go `tool` directive); `"runtime"` = loaded by reflection, ServiceLoader, or classpath resources rather than imported (JDBC drivers, logging backends); absent for normal deps |
| From scan | `malicious`, `malicious_check`, `dormant_release`, `reason` | Fetched in Phase 0; not part of diet JSON |

Note: Only `scope`, `indirect_via`, and `import_files` may be absent from a diet entry. Numeric and boolean fields are always present, so `0` and `false` are real values -- except when coupling was not measured (see Mode C), where the zeros mean "unknown".

### Step 2: Classify threat profile

Assign a threat profile based on diet data:

Rows are evaluated top to bottom; the first match wins. "Vulnerable" below means advisories on the pinned version or on the latest stable release.

| Profile | Criteria | Phase 2 strategy |
|---------|----------|-----------------|
| **COMPROMISED** | scan returned `malicious` | Handled by the early exit in Phase 0 |
| **SUSPECT-RELEASE** | `dormant_release` present (shown for 7 days after publication), OR scan's `reason` starts with `Possible hijacked release`, OR you found from the release history that the pinned version came out after a year or more without releases and its publisher and contents are not yet verified | Phase 2 on security-critical files; Scenario B is the primary scenario. For npm, check who published the version (`npm view <package>@<version> _npmUser`) against earlier releases and diff the package contents against the release before the silence. If `dormant_release.removed` is `true`, the version was unpublished, so `npm view <package>@<version>` fails; compare against the release before the silence and plan to stay on it |
| **WITHDRAWN** | scan's `reason` starts with `All releases yanked on` | Not a takeover signal: the registry offers no version to normal resolution, so a fresh install or lockfile update can break. Phase 2 is optional; the verdict is about what stops building, plus any advisories |
| **CRITICAL-EXPOSURE** | Vulnerable AND `lifecycle` in {EOL-Confirmed, EOL-Effective, EOL-Scheduled, Archived, Stalled} AND (`import_file_count > 0` OR `scope = "runtime"`) | Full Phase 2 on all security-critical files |
| **LATENT-RISK** | `lifecycle` in {EOL-Confirmed, EOL-Effective, EOL-Scheduled, Archived, Stalled} AND (`import_file_count > 0` OR `scope = "runtime"`) AND not vulnerable | Phase 2 on security-critical files only |
| **SUPPLY-CHAIN-ONLY** | `is_unused = true` AND the Phase 0 search found nothing, OR `scope = "tool"` AND no production file imports it | Skip Phase 2 call-site reading -- but still assess install-time and build-time execution in Scenario B |
| **MONITORING** | `lifecycle` is Active, Legacy-Safe, or Review Needed (lack of data), but vulnerable | Phase 2 optional -- assess known CVE impact |
| **INCOMPLETE** | `malicious_check = "unknown"`, or the ecosystem is not covered by the malicious check, or `lifecycle = Review Needed` because data is missing (`Insufficient data for assessment`, `Commit data missing`), or the pinned version's advisories could not be checked, and nothing above matched | Do not call the package healthy. Report what could not be checked and re-run the failed lookup before deciding |
| **LOW-RISK** | Any combination not matching above | Produce minimal assessment noting the dependency is healthy or benign |

When coupling was not measured (see Mode C), `is_unused` is `false` and `import_file_count` is 0 for every entry. Do not read those zeros as "no data flow"; treat the dependency as Mode B (coupling unavailable, assume > 0).

Lifecycle values produced by diet: `Active`, `Stalled`, `Legacy-Safe`, `EOL-Confirmed`, `EOL-Effective`, `EOL-Scheduled`, `Archived`, `Review Needed`. Two of them need care:

- `Archived` is diet's own label for "the source repository is archived"; it overrides the lifecycle label. `uzomuzo scan` reports the same package as `Stalled` unless the registry also signals end of life ([ADR-0020](https://github.com/future-architect/uzomuzo-oss/blob/main/docs/adr/0020-archived-registry-liveness.md)). Check scan's `stable_version` and its release date: packages that moved into a monorepo keep publishing while the old repository is archived. Archived alone does not mean the code is known bad.
- `Review Needed` covers three different situations, told apart by scan's `reason`: too little data to classify (`Insufficient data for assessment`, `Commit data missing`); every release yanked ([ADR-0022](https://github.com/future-architect/uzomuzo-oss/blob/main/docs/adr/0022-all-releases-yanked-is-not-eol.md)); or, on npm, a release burst across release lines after a long silence -- how past takeovers of quiet npm packages have looked ([ADR-0026](https://github.com/future-architect/uzomuzo-oss/blob/main/docs/adr/0026-dormant-release-burst.md)).

### Step 3: Scope Phase 2 targets

Filter `symbols[]` and `import_files[]` to identify security-relevant subsets.

**Security-relevant symbol patterns** (language-neutral, case-insensitive substring match):

```
Auth, Cred, Secret, Token, Key, Encrypt, Decrypt, Sign, Verify, Cert,
TLS, SSL, Hash, Password, Session, Cookie, Policy, ACL, Permission,
Role, Assume, Identity, Provider, IAM, STS,
Connect, Dial, Transport, Listen, Serve, Client, Request, Response,
SQL, Query, Exec, Database, Store, Cache, Write, Put, Delete,
Marshal, Unmarshal, Decode, Encode, Parse, Deserialize
```

**Security-critical file path patterns** (substring match on `import_files[]`):

```
auth/, credential/, secret/, crypto/, security/, acl/, policy/,
logical/, secrets/, iam/, rotation/, roles/, permission/,
transport/, client/, server/, handler/, middleware/, gateway/,
storage/, database/, physical/, cache/, session/, login/,
api/, endpoint/, route/, grpc/, http/
```

These patterns are **heuristics for prioritization, not hard filters**. A file that matches no pattern is not "safe" — it is merely lower priority. Any file in `import_files[]` could contain security-relevant code regardless of its path name.

Record:
- **Priority 1 (high-confidence security)**: files matching path patterns above
- **Priority 2 (remaining)**: all other files in `import_files[]`

When the security-relevant subset exceeds 50% of `api_breadth`, the dependency is likely an infrastructure/security SDK where nearly the entire API is security-relevant. In that case, treat all symbols as in-scope (do not filter by name) and rely solely on file prioritization for Phase 2 ordering.

### Step 4: Note coupling accuracy caveats

If any of these flags are set, note them -- they affect Phase 2 accuracy:

| Flag | Implication |
|------|------------|
| `has_dot_import` | Symbols used without package prefix -- `symbols[]` may be **undercounted**. Actual API surface is likely broader. |
| `has_wildcard_import` | Same: `from x import *` / `import static x.*` means undercounted symbols. |
| `has_blank_import` | Import for its side effect (Go `import _ "pkg"`, JS `import 'pkg'`), usually to register something -- a SQL driver, codec, or handler -- that then receives data through other APIs (a registered SQL driver sees every query and the database credentials). Trace where the registered component is used instead of assuming data flow is limited. The flag can also appear next to ordinary calls. |

If `scope` is `"tool"`: diet sets this when the module appears in a Go `tool` directive. That proves tooling use, not the absence of runtime use: another package of the same module can still be imported by production code (e.g. `golang.org/x/tools/cmd/stringer` as a tool and `golang.org/x/tools/go/packages` in `main`). If `import_files` lists production files, assess them as runtime imports. Otherwise supply chain risk applies at build time (a compromised tool could inject code into the build), but there is no runtime data flow.

If `scope` is `"runtime"`: The dependency is loaded without a static import -- a JDBC driver named in a connection URL, a logging backend found on the classpath, a Spring auto-configuration. `import_files` will be empty, but the code runs in production and often sees sensitive data (a JDBC driver sees every query and the database password). For Phase 2, find where it is wired: configuration files (`application.yml`, `*.properties`, `META-INF/services/*`, `spring.factories`), connection strings, and `Class.forName` calls.

## Phase 2: Targeted code inspection

**Skip this phase entirely if:**
- Threat profile is SUPPLY-CHAIN-ONLY. Skip Steps 1-2 and the call-site bullets of Step 3, but still write the install-time bullet of Scenario B (Step 3) and Step 4
- No source code is available (Mode B input)
- `import_file_count` is 0 and `scope` is not `"runtime"` (for `"runtime"`, read the wiring found in Phase 1 Step 4 instead of `import_files`)

For each skip case, add a "Limitations" note to the verdict explaining what could not be assessed and why.

### Scoping strategy

Read files in priority order (Priority 1 first, then Priority 2). **Stop when you have enough evidence to construct credible attack scenarios for both Scenario A and Scenario B** (see Step 3 below). You have enough evidence when you can answer all four questions in Step 2 (Data IN, Data OUT, Security boundary, Failure mode) for at least the most security-critical call sites.

**Reading order:**
1. Priority 1 files (from Phase 1 Step 3) — read these first
2. Priority 2 files — read in order until you have enough evidence, or all files are read
3. For any files not read, produce a statistical summary: "N files in auth paths, M in infrastructure, K in tests"

**Stopping criteria — you have enough evidence when:**
- You can name the concrete data types flowing IN and OUT of the dependency
- You can describe at least one realistic attack scenario grounded in observed data flows
- Reading more files would add volume but not change the risk verdict

If you stop before reading all files, record which files were read and which were not in the verdict's "Limitations" section. This is not a failure — it is an explicit scope decision.

### Step 1: Classify import sites by security impact

For each file read, classify it:

- **Security-critical**: handles auth, credentials, secrets, crypto, storage of sensitive data, network transport, policy/ACL decisions
- **Infrastructure**: config loading, logging, metrics, CLI argument parsing, build tooling
- **Peripheral**: tests, documentation, examples, code generation, benchmarks

### Step 2: Trace data flows at security-critical sites

For each security-critical import location, determine:

1. **Data IN**: What data is passed TO this dependency? (credentials, secrets, user input, config values, plaintext?)
2. **Data OUT**: What data comes FROM this dependency? (connections, sessions, decoded secrets, query results?)
3. **Security boundary**: Is there encryption, validation, or auth gating around the call site?
4. **Failure mode**: If this dependency silently returned wrong data, what would break? Would anyone notice?

Quote specific file paths and line numbers. Do not speculate -- if you cannot determine something from the code, say "undetermined from static analysis."

### Step 3: Construct attack scenarios

Build scenarios from the actual data flows observed in Step 2, not from generic templates.

**Scenario A -- Unpatched vulnerability**:
- Does the dependency have known CVEs? (check `has_vulnerabilities`, `vulnerability_count`, `max_cvss_score` from diet)
- If known CVEs exist: What specific data flows are exposed by them?
- If no known CVEs: Given the data flows observed, what category of vulnerability (RCE, data leak, auth bypass, DoS) would have the highest impact if discovered?

**Scenario B -- Supply chain compromise** (a malicious version is published, typically through a stolen maintainer account):
- **Install-time and build-time execution**: Does the package get to run code before any of your call sites do? npm `preinstall`/`install`/`postinstall` scripts, Python source distributions that run `setup.py`, Maven/Gradle plugins, Go `tool` directives and `go generate`. Some recent npm compromises, such as the self-spreading Shai-Hulud worm in 2025, did all their work in an install script, stealing npm tokens, GitHub tokens, and cloud keys from developer machines and CI runners. This applies even when `import_file_count = 0`. Name the environments that install this package and what secrets they hold.
- If this package **exfiltrated data via IN flows**: What could an attacker steal? (Based on actual Data IN from Step 2)
- If this package **altered OUT flows**: What would break? What would be silent?
- Is the attack **detectable?** A crash is detectable. Subtly wrong auth decisions or silently leaked credentials are not.
- **Takeover likelihood**: An unmaintained package with a single maintainer is easier to take over than an active one, and a takeover after a long silence is what scan's dormant-release marker watches for. Use `lifecycle`, `overall_score`, and `dormant_release` here; do not invent a probability.

### Step 4: Identify mitigating factors

These factors affect **risk severity** (how dangerous is keeping this dependency), not removal feasibility (how hard is it to remove). Removal planning belongs in `/diet-evaluate-removal`.

For each applicable factor, cite the evidence from the code:

- Upstream encryption before data reaches this package
- Downstream validation after data leaves this package
- Package pinned by lockfile integrity (go.sum, package-lock.json, pip --require-hashes). This stops a swapped artifact for the same version; it does not stop a new malicious version from arriving through a routine lockfile update
- Install scripts disabled where it is installed (`npm ci --ignore-scripts`, pnpm 10 or later, which does not run dependency build scripts unless allowed -- check the pnpm version and `onlyBuiltDependencies` before counting this, `pip install --only-binary :all:`). Removes the install-time part of Scenario B
- Dependency updates held back by a minimum release age or manual review (e.g., Renovate `minimumReleaseAge`, pnpm `minimumReleaseAge`). Most malicious versions are named in an advisory within days of publication, though not all are removed from the registry
- Provenance on the pinned version. npm provenance records which repository and workflow built the package; PyPI's default publish attestations record which trusted publisher uploaded it, which is not the same as where it was built. Read the attestation actually present. Either kind it does not prove the code is benign, and it does not help when the attacker can run the release workflow itself (e.g., with a stolen GitHub token). A version that lacks provenance when earlier ones had it is a warning sign
- Package is small enough to audit -- judged from the package's own code and dependencies after reading them, not from `api_breadth`, which only counts the symbols your code uses (one call into a large SDK gives `api_breadth` 1)
- Usage is behind a build tag / feature flag that limits exposure
- Test-only usage (files in `*_test.go`, `test/`, `__tests__/`, `tests/`)
- `scope: "tool"` with no production file importing it -- build-time only, no runtime exposure

## Phase 3: Risk verdict

### Single dependency verdict

```
### {package_name} -- Risk: {CRITICAL|HIGH|MEDIUM|LOW}

#### Diet Facts (from automated analysis)

| Metric | Value |
|--------|-------|
| PURL | `{purl}` ({name} {version}, {ecosystem}) |
| Rank | #{rank} of {summary.total_direct} direct deps |
| Lifecycle | {lifecycle} ([registry link]) |
| Known CVEs (latest release) | {vulnerability_count} ({direct_advisory_count} direct, {transitive_advisory_count} via its dependencies) {(max CVSS: {max_cvss_score}) if present, else "(CVSS unavailable)"} |
| Known CVEs (pinned version) | {count and IDs from OSV, or "not checked"} |
| Malicious check | {clean / flagged: {advisory_id} ({scope}) / unknown -- lookup failed} |
| Dormant release | {none / {silent_days} days of silence, published {published_at}} *(npm only; omit row otherwise)* |
| Priority / Overall | {priority_score} / {overall_score} |
| Difficulty | {difficulty} (coupling_effort: {coupling_effort}) |
| Coupling | {import_file_count} files, {call_site_count} calls, {api_breadth} APIs |
| Graph impact | {graph_impact} (exclusive: {exclusive_transitive}, total: {total_transitive}) |
| Health risk | {health_risk} |
| Stays as indirect | {stays_as_indirect} {-- via {indirect_via} if true} |
| Import caveats | {list any true flags: has_dot_import, has_wildcard_import, has_blank_import} *(omit row if all false)* |
| Scope | {scope} *(omit row if absent)* |

#### New Findings (from this assessment)

**Threat profile**: {SUSPECT-RELEASE | WITHDRAWN | INCOMPLETE | CRITICAL-EXPOSURE | LATENT-RISK | SUPPLY-CHAIN-ONLY | MONITORING | LOW-RISK}

**Files inspected**: {N} of {import_file_count} files read ({M} Priority 1, {K} Priority 2)

**Data flow summary**:

| Direction | Data type | Security relevance |
|-----------|-----------|-------------------|
| IN | {concrete data observed} | {impact if compromised} |
| OUT | {concrete data observed} | {impact if tampered} |

**Scenario A (Unpatched vulnerability)**: {concrete scenario based on observed data flows}

**Scenario B (Supply chain compromise)**: {concrete scenario -- what could be stolen or altered}

**Mitigating factors**:
- {factor with code evidence}

**Limitations**: {what could not be assessed -- e.g., "51+ files; sampled 10 security-critical files only"}

**Verdict**: {1-2 sentence risk summary}
**Recommended posture**: {one of the following}
```

#### Posture definitions

| Posture | Meaning |
|---------|---------|
| **Remove urgently** | Malicious version, active exploitation risk, or critical data exposure. Run `/diet-remove` now (for a malicious version: move off it first, then rotate the secrets listed in the verdict). |
| **Plan removal** | Significant risk that grows over time. Run `/diet-evaluate-removal` to plan. |
| **Monitor** | Low current risk but degrading health. Re-assess quarterly. |
| **Accept with documentation** | Risk is understood and mitigated. Document the decision and rationale. |

### Multiple dependencies summary table (top N mode)

After individual verdicts, produce a summary:

| Package | Lifecycle | Diet Priority | Difficulty | Threat Profile | Risk | CVEs | Posture |
|---------|-----------|--------------|------------|---------------|------|------|---------|

## Primary Source Links

The verdict MUST include verifiable primary source links. Never claim "EOL", "Archived", or "has CVEs" without evidence.

### Required links

| Claim | Required link |
|-------|--------------|
| Lifecycle status | Package registry page (see PURL-to-URL table below) |
| Known CVEs | NVD (`nvd.nist.gov/vuln/detail/<CVE>`), GitHub Advisory (`github.com/advisories/<GHSA>`), or OSV (`osv.dev/vulnerability/<ID>`) |
| Malicious version | The advisory scan names (`malicious.reference`), or `osv.dev/vulnerability/<MAL-ID>` |
| Dormant release | The npm version page (`https://www.npmjs.com/package/<name>/v/<version>`) and the previous release's date |
| Scorecard | `https://scorecard.dev/viewer/?uri=github.com/<org>/<repo>` |
| Repository archived | GitHub repo URL (shows archived banner) |
| Last commit date | `https://github.com/<org>/<repo>/commits/<branch>` |

### PURL-to-registry URL

| Ecosystem | URL pattern |
|-----------|-------------|
| `pkg:npm` | `https://www.npmjs.com/package/<name>` |
| `pkg:golang` | `https://pkg.go.dev/<namespace/name>` |
| `pkg:pypi` | `https://pypi.org/project/<name>` |
| `pkg:maven` | `https://central.sonatype.com/artifact/<namespace>/<name>` |
| `pkg:cargo` | `https://crates.io/crates/<name>` |
| `pkg:nuget` | `https://www.nuget.org/packages/<name>` |
| `pkg:githubactions` | `https://github.com/<namespace>/<name>` |

## Language-specific reference

The core analysis (Phases 0-3) is language-neutral. Use this table for language-specific details:

| Aspect | Go | Python | JavaScript/TypeScript | Java |
|--------|-----|--------|----------------------|------|
| Build constraints | `//go:build` tags | N/A | N/A | Maven profiles |
| Side-effect imports | `import _ "pkg"` | `try/except ImportError` | `import 'pkg'` (no binding), `require('pkg')` | Static initializer blocks |
| Wildcard imports | `import . "pkg"` | `from x import *` | N/A (namespace `import * as ns` -- symbols tracked via `ns.` prefix) | `import static x.*` |
| Lockfile integrity | `go.sum` hash verification | `pip --require-hashes` | `package-lock.json` integrity field | No standard lockfile: pinned versions plus checksum verification you configure explicitly (`maven-enforcer-plugin` alone does not verify artifact hashes) |
| Vulnerability scanner | `govulncheck` | `pip-audit`, `safety` | `npm audit` | `dependency-check`, OWASP |
| Test file patterns | `*_test.go` | `test_*.py`, `tests/` | `*.test.js`, `__tests__/` | `*Test.java`, `src/test/` |

## Important rules

- **Start from diet's output.** Do not re-discover what diet already computed. The `symbols[]` and `import_files[]` fields are your starting points for Phase 2.
- **Fill diet's gaps from scan.** Malicious-version and dormant-release signals, and the advisories on the pinned version, are not in diet JSON. A verdict that skipped them must say so.
- **Install time counts.** "Zero imports" removes call-site data flows, not the package's chance to run code on every machine that installs it.
- **Read in priority order, stop when you have enough.** Phase 1 Step 3 gives you the reading order. Stop when you can construct credible attack scenarios — not when you hit an arbitrary file count.
- **Be precise.** Quote specific file paths and line numbers. "The auth module uses this" is too vague.
- **Do not speculate.** If you cannot determine something from the code, say "undetermined from static analysis."
- **Do not overstate risk.** If data is encrypted before reaching the package, the supply chain scenario changes. Say so.
- **Focus on silent attacks.** Crashes are detectable. Subtle behavior changes (wrong auth decisions, leaked credentials, altered responses) are the real threat.
- **Do not assess replacement options.** That is `/diet-evaluate-removal`'s job. This skill assesses the risk of the status quo.
- **Always include primary source links.** Every lifecycle claim, CVE reference, and scorecard score must have a clickable URL.
- **State limitations explicitly.** If you sampled 10 of 51 files, or skipped Phase 2 because no source was available, say so in the verdict.
- **`stays_as_indirect` matters.** If the dependency remains as an indirect dep after removal, note that removing direct usage reduces but does not eliminate the risk.
