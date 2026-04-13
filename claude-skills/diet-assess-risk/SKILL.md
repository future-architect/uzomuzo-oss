---
name: diet-assess-risk
description: "Deep-dive security risk analysis for a dependency flagged by uzomuzo diet"
argument-hint: "<PURL or module path, or 'top N'>"
---

# Diet Risk Assessment: $ARGUMENTS

Analyze the security risk of keeping a dependency identified by `uzomuzo diet`. This command performs deep data-flow analysis that diet's automated scoring cannot do.

**When to use**: After `uzomuzo diet` flags a dependency as EOL/Stalled/Archived with non-trivial source coupling. Trivial (0 imports) dependencies don't need this -- just remove them.

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

### Early exits

- If `is_unused` is `true`: **Stop.** Report "Unused dependency -- no data flow risk. Remove it." No further analysis needed.
- If `lifecycle` is `Active` and `has_vulnerabilities` is `false`: **Stop.** Report "Maintained, no known vulnerabilities. No immediate risk." This skill is for unhealthy dependencies.

## Phase 1: Threat surface inventory

This phase uses **only diet JSON fields** -- no code reading. It classifies the threat surface and produces scoping decisions for Phase 2.

### Step 1: Record diet facts

Extract the full diet entry. These fields will appear verbatim in the verdict's "Diet Facts" section:

| Field | JSON key | Use |
|-------|----------|-----|
| Identity | `purl`, `name`, `version`, `ecosystem` | Package identification |
| Ranking | `rank`, `priority_score`, `overall_score` | Diet's automated ranking (`rank` is position among all deps) |
| Health | `health_risk`, `lifecycle`, `has_vulnerabilities`, `vulnerability_count`, `max_cvss_score` | Maintenance and vulnerability status |
| Graph | `graph_impact`, `exclusive_transitive`, `total_transitive` | Blast radius |
| Coupling | `coupling_effort`, `difficulty`, `import_file_count`, `call_site_count`, `api_breadth` | Integration depth |
| Coupling detail | `symbols`, `import_files` | Specific APIs used and files importing them (Phase 2 starting points) |
| Persistence | `stays_as_indirect`, `indirect_via` | Whether risk is eliminable |
| Import style | `has_blank_import`, `has_dot_import`, `has_wildcard_import` | Coupling accuracy caveats |
| Scope | `scope` | `"tool"` deps have different risk profile (build-time only); absent for normal deps |

Note: Fields with `omitempty` in the JSON schema (`has_vulnerabilities`, `vulnerability_count`, `max_cvss_score`, `overall_score`, `scope`, `indirect_via`, `import_files`) may be absent from the JSON. Treat absent booleans as `false` and absent numbers as `0`.

### Step 2: Classify threat profile

Assign a threat profile based on diet data:

| Profile | Criteria | Phase 2 strategy |
|---------|----------|-----------------|
| **CRITICAL-EXPOSURE** | `has_vulnerabilities=true` AND `lifecycle` in {EOL-Confirmed, EOL-Effective, EOL-Scheduled, Archived, Stalled} AND `import_file_count > 0` | Full Phase 2 on all security-critical files |
| **LATENT-RISK** | `lifecycle` in {EOL-Confirmed, EOL-Effective, EOL-Scheduled, Archived, Stalled} AND `import_file_count > 0` AND `has_vulnerabilities=false` | Phase 2 on security-critical files only |
| **SUPPLY-CHAIN-ONLY** | `import_file_count = 0` OR `is_unused = true` | Skip Phase 2 -- risk is graph/supply-chain, not data flow |
| **MONITORING** | `lifecycle` is Active, Legacy-Safe, or Review Needed, but `has_vulnerabilities=true` | Phase 2 optional -- assess known CVE impact |
| **LOW-RISK** | Any combination not matching above (e.g., `Legacy-Safe` or `Review Needed` with no vulns) | Produce minimal assessment noting the dependency is healthy or benign |

Lifecycle values produced by diet: `Active`, `Stalled`, `Legacy-Safe`, `EOL-Confirmed`, `EOL-Effective`, `EOL-Scheduled`, `Archived`, `Review Needed`.

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

Record:
- **Security-relevant symbols**: filtered subset of `symbols[]`
- **Security-critical files**: filtered subset of `import_files[]`
- **Remaining files**: everything else (for statistical summary only)

When the security-relevant subset exceeds 50% of `api_breadth`, the dependency is likely an infrastructure/security SDK where nearly the entire API is security-relevant. In that case, treat all symbols as in-scope (do not filter by name) and focus Phase 2 scoping entirely on the file path classification from above. The file path filter becomes the primary mechanism for managing token budget.

### Step 4: Note coupling accuracy caveats

If any of these flags are set, note them -- they affect Phase 2 accuracy:

| Flag | Implication |
|------|------------|
| `has_dot_import` | Symbols used without package prefix -- `symbols[]` may be **undercounted**. Actual API surface is likely broader. |
| `has_wildcard_import` | Same: `from x import *` / `import static x.*` means undercounted symbols. |
| `has_blank_import` | Side-effect-only import (Go `import _ "pkg"`, JS `import 'pkg'`). Only `init()` / module side effects execute. Check what they do, but data flow is limited. |

If `scope` is `"tool"`: This is a build-time/tooling dependency (e.g., Go `tool` directive). It does not execute at runtime. Supply chain risk applies (compromised tool could inject malicious code at build time), but runtime data flow risk is absent.

## Phase 2: Targeted code inspection

**Skip this phase entirely if:**
- Threat profile is SUPPLY-CHAIN-ONLY
- No source code is available (Mode B input)
- `import_file_count` is 0

For each skip case, add a "Limitations" note to the verdict explaining what could not be assessed and why.

### Scoping strategy

Do NOT read all files. Use the file count and API breadth to select a strategy:

| `import_file_count` | `api_breadth` | Strategy |
|---------------------|---------------|----------|
| 1-5 | any | Read all importing files |
| 6-15 | any | Read security-critical files (from Phase 1 Step 3). Grep remaining files for security-relevant symbols. |
| 16-50 | 1-20 | Read security-critical files. Grep remaining files for security-relevant symbols. Summarize non-critical files statistically. |
| 16-50 | 21+ | Read security-critical files only. Produce a statistical summary for the rest: "N files in auth paths, M in infrastructure, K in tests." |
| 51+ | any | Read at most 10 security-critical files. Everything else is statistical summary. State the sampling limitation in the verdict. |

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

**Scenario B -- Supply chain compromise**:
- If this package **exfiltrated data via IN flows**: What could an attacker steal? (Based on actual Data IN from Step 2)
- If this package **altered OUT flows**: What would break? What would be silent?
- Is the attack **detectable?** A crash is detectable. Subtly wrong auth decisions or silently leaked credentials are not.

### Step 4: Identify mitigating factors

These factors affect **risk severity** (how dangerous is keeping this dependency), not removal feasibility (how hard is it to remove). Removal planning belongs in `/diet-evaluate-removal`.

For each applicable factor, cite the evidence from the code:

- Upstream encryption before data reaches this package
- Downstream validation after data leaves this package
- Package pinned by lockfile integrity (go.sum, package-lock.json, pip --require-hashes)
- Package is small and auditable (check `api_breadth` -- under 10 is auditable)
- Usage is behind a build tag / feature flag that limits exposure
- Test-only usage (files in `*_test.go`, `test/`, `__tests__/`, `tests/`)
- `scope: "tool"` -- build-time only, no runtime exposure

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
| Known CVEs | {vulnerability_count} {(max CVSS: {max_cvss_score}) if present, else "(CVSS unavailable)"} |
| Priority / Overall | {priority_score} / {overall_score} |
| Difficulty | {difficulty} (coupling_effort: {coupling_effort}) |
| Coupling | {import_file_count} files, {call_site_count} calls, {api_breadth} APIs |
| Graph impact | {graph_impact} (exclusive: {exclusive_transitive}, total: {total_transitive}) |
| Health risk | {health_risk} |
| Stays as indirect | {stays_as_indirect} {-- via {indirect_via} if true} |
| Import caveats | {list any true flags: has_dot_import, has_wildcard_import, has_blank_import} *(omit row if all false)* |
| Scope | {scope} *(omit row if absent)* |

#### New Findings (from this assessment)

**Threat profile**: {CRITICAL-EXPOSURE | LATENT-RISK | SUPPLY-CHAIN-ONLY | MONITORING | LOW-RISK}

**Security-relevant coupling**: {count of security-critical files from Phase 1 Step 3} of {import_file_count} files in security-critical paths, {count of security-relevant symbols from Phase 1 Step 3} of {api_breadth} APIs are security-sensitive

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
| **Remove urgently** | Active exploitation risk or critical data exposure. Run `/diet-remove` now. |
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

## Language-specific reference

The core analysis (Phases 0-3) is language-neutral. Use this table for language-specific details:

| Aspect | Go | Python | JavaScript/TypeScript | Java |
|--------|-----|--------|----------------------|------|
| Build constraints | `//go:build` tags | N/A | N/A | Maven profiles |
| Side-effect imports | `import _ "pkg"` | `try/except ImportError` | `import 'pkg'` (no binding), `require('pkg')` | Static initializer blocks |
| Wildcard imports | `import . "pkg"` | `from x import *` | N/A (namespace `import * as ns` -- symbols tracked via `ns.` prefix) | `import static x.*` |
| Lockfile integrity | `go.sum` hash verification | `pip --require-hashes` | `package-lock.json` integrity field | `maven-enforcer-plugin` |
| Vulnerability scanner | `govulncheck` | `pip-audit`, `safety` | `npm audit` | `dependency-check`, OWASP |
| Test file patterns | `*_test.go` | `test_*.py`, `tests/` | `*.test.js`, `__tests__/` | `*Test.java`, `src/test/` |

## Important rules

- **Start from diet's output.** Do not re-discover what diet already computed. The `symbols[]` and `import_files[]` fields are your starting points for Phase 2.
- **Scope aggressively.** You cannot read 140 API symbols across 31 files. Filter to security-relevant subsets first using Phase 1 Step 3.
- **Be precise.** Quote specific file paths and line numbers. "The auth module uses this" is too vague.
- **Do not speculate.** If you cannot determine something from the code, say "undetermined from static analysis."
- **Do not overstate risk.** If data is encrypted before reaching the package, the supply chain scenario changes. Say so.
- **Focus on silent attacks.** Crashes are detectable. Subtle behavior changes (wrong auth decisions, leaked credentials, altered responses) are the real threat.
- **Do not assess replacement options.** That is `/diet-evaluate-removal`'s job. This skill assesses the risk of the status quo.
- **Always include primary source links.** Every lifecycle claim, CVE reference, and scorecard score must have a clickable URL.
- **State limitations explicitly.** If you sampled 10 of 51 files, or skipped Phase 2 because no source was available, say so in the verdict.
- **`stays_as_indirect` matters.** If the dependency remains as an indirect dep after removal, note that removing direct usage reduces but does not eliminate the risk.
