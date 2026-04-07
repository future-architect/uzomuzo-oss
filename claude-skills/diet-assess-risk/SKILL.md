---
name: diet-assess-risk
description: "Deep-dive security risk analysis for a dependency flagged by uzomuzo diet"
argument-hint: "<PURL or module path, or 'top N'>"
---

# Diet Risk Assessment: $ARGUMENTS

Analyze the security risk of a dependency identified by `uzomuzo diet`. This command performs deep data-flow analysis that diet's automated scoring cannot do.

**When to use**: After `uzomuzo diet` flags a dependency as EOL/Stalled/Archived with non-trivial source coupling. Trivial (0 imports) dependencies don't need this — just remove them.

**Relationship to diet**:
- `uzomuzo diet` tells you **what** to remove and **how hard** (automated, broad)
- `/diet-assess-risk` tells you **how dangerous** it is to keep (LLM-powered, deep)

## Phase 1: Gather diet context

If a diet plan JSON is available, read it to get the automated scores first:

```bash
uzomuzo diet --sbom bom.json --source . --format json > diet.json
```

Extract the target dependency's entry:
- Priority score, difficulty, ONLY-VIA-THIS count
- FILES, CALLS, API breadth
- Lifecycle status (EOL-Confirmed, Stalled, Archived, etc.)

**Skip any analysis that diet already provides.** Focus this command on what diet cannot do: data flow tracing and attack scenario construction.

## Phase 2: Trace security-relevant data flows

For each file importing this dependency (diet already identified these files — start there, don't re-search):

### Step 1: Classify import sites by security impact

- **Security-critical**: auth, ACL, policy, crypto, storage, credential, secret engine, network transport
- **Infrastructure**: config, logging, metrics, CLI
- **Peripheral**: testing, documentation, examples, code generation

### Step 2: Read the actual code at security-critical sites

For each security-critical import location, determine:

1. **What data is passed TO this package?** (credentials, secrets, config, metadata?)
2. **What data comes FROM this package?** (connections, decoded config, cloned data?)
3. **Is there a security boundary?** (encryption before, validation after, auth hot path?)

Check for:
- Build constraints (`//go:build`) that limit exposure
- Blank imports (`_ "package"`) — only `init()` executes, skip data flow
- Dot imports (`. "package"`) — mark as broadly coupled

### Step 3: Construct attack scenarios

**Risk A (Unpatched vulnerabilities)**:
- Does the dependency have known CVEs? (check diet's health signals, deps.dev, govulncheck if available)
- If a CVE were discovered tomorrow, what data or operations would be exposed?

**Risk B (Supply chain takeover)**:
- If this package **exfiltrated data**: What could an attacker steal?
- If this package **altered return values**: What would break?
- Would the attack be **silent?** (no crash, no error log, just changed behavior)

### Step 4: Identify mitigating factors

- Upstream encryption before data reaches this package?
- Downstream validation after?
- Package pinned by go.sum / lockfile hash?
- Package small and auditable?

## Phase 3: Risk verdict

```
### {package_name} — Risk: {CRITICAL|HIGH|MEDIUM|LOW}

**Diet scores**: Priority {N}, Difficulty {level}, {N} files, {N} calls
**Lifecycle**: {from diet's health signals}
**Known CVEs**: {from deps.dev or govulncheck}

**Data flow**:
- IN: {what data is passed to this package}
- OUT: {what data comes from this package}

**Risk A (Unpatched vulns)**: {assessment}
**Risk B (Supply chain)**: {most realistic attack scenario}
**Mitigating factors**: {what limits the blast radius}

**Verdict**: {1-2 sentence risk summary}
**Recommended action**: {immediate removal / monitor / accept risk}
```

If analyzing multiple dependencies ("top N"), output a summary table:

| Package | Diet Priority | Difficulty | Risk A | Risk B | Overall | Action |
|---------|--------------|------------|--------|--------|---------|--------|

## Important rules

- **Start from diet's output.** Don't re-discover what diet already computed.
- **Be precise.** Quote specific file paths and line numbers.
- **Do not speculate.** If you cannot determine something from the code, say so.
- **Do not overstate risk.** If data is encrypted before reaching the package, say so.
- **Focus on silent attacks.** Crashes are detectable. Subtle behavior changes are the real threat.
- **This is for non-Go projects too.** The data flow tracing approach works for any language — adapt the grep/search patterns accordingly.
