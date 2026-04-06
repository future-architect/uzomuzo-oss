---
description: Analyze security risk of EOL/Archived dependencies by tracing data flow in source code
arguments:
  - name: target
    description: "Path to Go project source code, or 'auto' to use current directory"
    required: false
---

# EOL Dependency Risk Analysis

Analyze the security risk of EOL/Archived dependencies in the target project by reading the actual source code and tracing data flows.

**Scope**: This command is designed for Go projects. The detection phase (uzomuzo scan) works with any ecosystem via SBOM/PURL input, but the code analysis steps below are Go-specific.

## Prerequisites

- **`GITHUB_TOKEN` environment variable is recommended but not required.** uzomuzo detects archived repositories via deps.dev Scorecard data without a token. Setting `GITHUB_TOKEN` improves accuracy and speed by also querying the GitHub GraphQL API directly.

## Risk Models

EOL/Archived packages carry two distinct risk categories. Evaluate BOTH for each package:

- **Risk A — Unpatched vulnerabilities**: Known or future CVEs will not be fixed upstream. The package remains frozen, and any security flaw discovered after archival is permanent.
- **Risk B — Supply chain takeover**: An archived repo or unmaintained account could be hijacked, injecting malicious code into a trusted import path.

**Target**: $ARGUMENTS (default: current directory)

## Phase 1: Detect EOL packages

Run uzomuzo to find EOL/Archived dependencies:

```
uzomuzo scan --file go.mod --show-only replace --format table
```

If uzomuzo is not available, check go.mod and identify packages that are known to be archived or EOL.

List all EOL packages found, distinguishing direct vs indirect dependencies. If there are more than 5, focus on the top 5 by likely severity (prefer packages used in security-sensitive paths: auth, crypto, storage, ACL, network).

For **indirect dependencies**, also identify which direct dependency pulls them in — this determines the remediation path.

To include transitive (indirect) dependencies in the scan:
```
uzomuzo scan --file go.mod --show-transitive --show-only replace --format table
```
This may significantly increase the number of results. For initial triage, direct-only is recommended. Add `--show-transitive` for comprehensive analysis.

## Phase 1c: GitHub Actions scan (optional)

If the project uses GitHub Actions, also scan for EOL/archived actions:

```
uzomuzo scan <repository-url> --include-actions --format table --show-only replace,review
```

The repository URL can be found in Phase 1's detailed output (`--format detailed` shows the Repository link). Focus on actions used in release/deploy workflows — these have the highest supply chain impact.

Note: deps.dev resolution for Actions can sometimes map to wrong packages. Verify suspicious results manually.

## Phase 1.5: Reachability check (govulncheck)

```
govulncheck ./...
```

govulncheck uses call graph analysis to identify which known CVEs are actually reachable from your code.

**Scope**: Only extract results for EOL packages found in Phase 1.

- **EOL + Reachable (Called)**: Highest priority — reachable CVE with no upstream fix coming.
- **EOL + Not Reachable (Not called)**: Lowers Risk A, but Risk B (supply chain) remains.
- **EOL + No CVEs in govulncheck**: Does not mean safe — future CVEs won't be fixed either.

**CVE data sources**:
1. uzomuzo detailed output (`--format detailed`) shows advisories from deps.dev
2. govulncheck shows reachable CVEs with call graph analysis
3. For manual lookup: https://deps.dev/ or https://osv.dev/

Skip this phase if govulncheck is not available.

## Phase 2: For each EOL package, trace the data flow

### Step 1: Find all import locations

**Direct dependencies**: Search for all .go files (excluding tests) that import this package:
```
grep -r "package-path" --include="*.go" -l | grep -v _test.go
```

**Indirect dependencies**: Identify which direct dependency uses this indirect package, then trace data flow through it.

Categorize by component:
- **Security-critical**: auth, ACL, policy, crypto, storage, credential, secret engine
- **Infrastructure**: config, logging, metrics, CLI
- **Peripheral**: testing, documentation, examples

**Check build constraints**: For each import file, check for `//go:build` directives. If a dependency is only imported behind enterprise/platform-specific build tags, note this — it significantly reduces risk in the default build but the dependency remains in go.mod and go.sum (supply chain risk B still applies at the binary level).

**Blank imports**: If a file uses `_ "package"` (blank import), note that only the package's `init()` function executes. Skip data flow analysis (Step 2) for these — instead, check what the `init()` function does (usually driver/codec registration).

### Step 2: Read the actual code at each import site

For each security-critical import location, determine:

1. **What data is passed TO this package?** (credentials, secrets, config, metadata?)
2. **What data comes FROM this package?** (connections, decoded config, cloned data?)
3. **Is there a security boundary?** (encryption before, validation after, auth hot path?)

### Step 3: Construct attack scenarios

**Risk A (Unpatched vulnerabilities)**:
- Does uzomuzo report known CVEs? What type of vulnerability is most likely given the package's function?
- If a CVE were discovered tomorrow, what data or operations would be exposed?

**Risk B (Supply chain takeover)**:
- If this package **exfiltrated data**: What could an attacker steal?
- If this package **altered return values**: What would break?
- If this package **returned shallow copies instead of deep copies**: Could data leak between requests?
- **Would the attack be silent?** (no crash, no error log, just changed behavior)

### Step 4: Identify mitigating factors
- Upstream encryption (AES, TLS) before data reaches this package?
- Downstream validation after?
- Package pinned by go.sum hash?
- Package small and auditable?
- Alternative code paths that bypass this package?

## Phase 3: Risk assessment

For each EOL package, output:

```
### {package_name} — Risk: {CRITICAL|HIGH|MEDIUM|LOW}

**Status**: {Archived since YYYY / EOL-Confirmed / etc.}
**Dependency type**: {Direct / Indirect (via X)}
**Known CVEs**: {from uzomuzo detailed output or govulncheck, or "None known"}
**Reachability (govulncheck)**: {Called / Not called / No CVEs found / N/A}
**Import locations**: {N files, M security-critical}
**Build constraints**: {None / enterprise-only / platform-specific}

**Data flow**:
- IN: {what data is passed to this package}
- OUT: {what data comes from this package}

**Risk A (Unpatched vulns)**: {assessment}
**Risk B (Supply chain)**: {most realistic attack scenario}
**Mitigating factors**: {what limits the blast radius}
**Verdict**: {1-2 sentence risk summary}
```

## Phase 4: Summary

Output a summary table:

| Package | Direct/Indirect | Known CVEs | Reachable? | Risk A | Risk B | Overall | Mitigations |
|---------|----------------|-----------|-----------|--------|--------|---------|-------------|

And a recommended action priority:
1. Which packages to replace immediately
2. Which packages to monitor
3. Which packages are low risk despite being EOL

## Important rules

- **Be precise. Quote specific file paths and line numbers.**
- **Do not speculate.** If you cannot determine something from the code, say so.
- **Distinguish what is certain vs what is inferred.**
- **Do not overstate risk.** If data is encrypted before reaching the package, say so.
- **Focus on silent attacks.** Crashes are detectable. Subtle behavior changes are the real threat.
