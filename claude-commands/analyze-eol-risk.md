---
description: Analyze security risk of EOL/Archived dependencies by tracing data flow in source code
arguments:
  - name: target
    description: "Path to Go project source code, or 'auto' to use current directory"
    required: false
---

# EOL Dependency Risk Analysis

Analyze the security risk of EOL/Archived dependencies in the target project by reading the actual source code and tracing data flows.

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

## Phase 1.5: Reachability check (govulncheck)

```
govulncheck ./...
```

govulncheck uses call graph analysis to identify which known CVEs are actually reachable from your code.

**Scope**: Only extract results for EOL packages found in Phase 1.

- **EOL + Reachable (Called)**: Highest priority — reachable CVE with no upstream fix coming.
- **EOL + Not Reachable (Not called)**: Lowers Risk A, but Risk B (supply chain) remains.
- **EOL + No CVEs in govulncheck**: Does not mean safe — future CVEs won't be fixed either.

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
**Known CVEs**: {from uzomuzo results, or "None known"}
**Reachability (govulncheck)**: {Called / Not called / No CVEs found / N/A}
**Import locations**: {N files, M security-critical}

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
