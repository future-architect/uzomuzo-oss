# ADR-0001: Relax Legacy-Safe Classification for Zero-Advisory Dormant Packages

## Status

Accepted (2026-03-13)

## Context

The lifecycle assessment classified 58% of packages (555/961) as **Stalled** in production runs. This made the Stalled label function as a catch-all bucket rather than a meaningful signal, reducing the utility of the assessment for operators.

A large subset (322 packages) were marked Stalled solely because:
- Scorecard Maintained score was low or absent
- No human commits for >2 years

However, many of these packages (e.g., `function-bind` with 1M+ dependents, `concat-map`) are intentionally "complete" — small utility packages that do one thing well and have zero known advisories. The previous logic required a Scorecard Vulnerability score >= 8.0 AND 3+ years of inactivity for Legacy-Safe, which only 6 packages satisfied.

## Decision

Add a new Legacy-Safe classification path in `assessInactiveState` (Path A, commit data available):

**Condition:** `advisory count == 0 AND days since last human commit > EolInactivityDays (730)`

This is evaluated **after** the existing high vulnerability score check and **before** the low maintenance score branch. The ordering ensures:

1. Packages with high vuln scores still get the existing Legacy-Safe path (stricter, higher confidence)
2. Packages with zero advisories and long dormancy get the new path
3. Packages with advisories still fall through to Stalled/EOL-Effective as before

### Why advisory count, not Scorecard Vulnerability score?

- Scorecard's Vulnerability score is often missing or unreliable for small packages
- Advisory count from deps.dev is a **concrete, binary signal** — either there are known vulnerabilities or there aren't
- Zero advisories on a dormant package is strong evidence of safety (no one has found or reported issues)

## Consequences

### Positive

- Stalled dropped from 58% to 24% (555 → 230 packages)
- Legacy-Safe increased from 1% to 34% (6 → 331 packages)
- Each label now carries more meaningful signal for operators

### Negative

- Packages with undiscovered vulnerabilities (zero advisories but actually vulnerable) will be classified as Legacy-Safe rather than Stalled. This is an acceptable risk because:
  - Advisory databases are actively maintained by the security community
  - The alternative (Stalled) also does not trigger any remediation action
  - Operators should still review Legacy-Safe packages during major audits

### Neutral

- No impact on Active, EOL-Confirmed, EOL-Effective, or Review Needed counts
- No configuration changes required (uses existing `EolInactivityDays` threshold)
