# Build Integrity Grading

[← Back to README.md](../README.md)

uzomuzo assesses the **supply chain build tamper resistance** of each dependency — how hard it would be for an attacker to inject malicious code through the build pipeline.

> **"This dependency hasn't been compromised yet, but if someone tried, here's how easy it would be."**

## How It Works

Build integrity is assessed using data already fetched from [deps.dev](https://deps.dev):

- **OpenSSF Scorecard checks** — security practices of the source repository
- **SLSA Provenance** — whether published artifacts have verified build provenance
- **Build Attestations** — whether build attestations are verified

These signals are combined into a single **weighted average score** (0–10) using the official [OpenSSF Scorecard risk-level weights](https://github.com/ossf/scorecard).

## Labels

| Label | Score | Verdict Impact | Meaning |
|-------|-------|----------------|---------|
| **Hardened** | ≥ 7.5 | None (`ok`) | Strong resistance — core protections in place |
| **Moderate** | 2.5–7.4 | Downgrades to `caution` | Improvement needed — meaningful gaps present |
| **Weak** | < 2.5 | Downgrades to `replace` | Minimal resistance — build pipeline largely unprotected |
| **Ungraded** | No data | None | Insufficient data (no Scorecard, no SLSA) |

`Ungraded` does **not** affect the verdict — it prevents mass noise for packages without Scorecard coverage.

## Signals and Weights

Weights are aligned with Scorecard's official risk levels (Critical=10, High=7.5, Medium=5, Low=2.5):

| Signal | Source | Risk Level | Weight |
|--------|--------|-----------|--------|
| Dangerous Workflow | Scorecard | Critical | 10.0 |
| Branch Protection | Scorecard | High | 7.5 |
| Code Review | Scorecard | High | 7.5 |
| Token Permissions | Scorecard | High | 7.5 |
| Binary Artifacts | Scorecard | High | 7.5 |
| Signed Releases | Scorecard | High | 7.5 |
| SLSA Provenance | deps.dev | High* | 7.5 |
| SAST | Scorecard | Medium | 5.0 |
| Packaging | Scorecard | Medium | 5.0 |
| Pinned Dependencies | Scorecard | Medium | 5.0 |
| Attestation | deps.dev | Medium* | 5.0 |

*SLSA/Attestation weights are assigned by analogy with comparable Scorecard checks.

### Missing Signal Handling

Missing or inconclusive signals are **excluded** from both numerator and denominator, following Scorecard's own convention. A missing signal is not penalized — it means "we don't know", not "the protection is absent".

## CLI Output

### Table Format

```
STATUS     PURL                           LIFECYCLE       BUILD
✅ ok       pkg:golang/go.uber.org/zap     Active          Hardened 8.1
⚠️ caution  pkg:npm/lodash                 Legacy-Safe     Moderate 4.2
🔴 replace  pkg:pypi/old-lib               Stalled         Weak 1.3
✅ ok       pkg:npm/some-private-pkg       Active          —
```

`—` indicates Ungraded (no Scorecard or SLSA data available).

### Detailed Format

The detail view shows individual signal scores in a compact 2-column layout. Critical/High signals are always shown (including `—` for inconclusive). Medium signals appear only when evaluated. The header shows `(evaluated/total)` checks.

```
├─ Build Integrity: Moderate 4.2/10 (6/11) ────────────────
│   Dangerous Workflow 10  Branch Protection    —
│   Code Review         9  Token Permissions    —
│   Binary Artifacts    0  Signed Releases      —
│   SLSA Provenance     —  Pinned Deps          3
│   Packaging           0
│   → https://scorecard.dev/viewer/?uri=github.com/...
```

### JSON Format

```json
{
  "purl": "pkg:npm/lodash@4.17.21",
  "verdict": "caution",
  "lifecycle": "Legacy-Safe",
  "build_integrity": "Moderate",
  "build_integrity_score": 4.2
}
```

The summary includes build integrity tallies:

```json
{
  "summary": {
    "build_integrity": {
      "hardened": 2,
      "moderate": 4,
      "weak": 1,
      "ungraded": 3
    }
  }
}
```

### CSV Format

Two columns are added: `build_integrity` (label) and `build_integrity_score` (float).

## Differentiation from Other Tools

| Tool | What it detects | Build Integrity |
|------|----------------|-----------------|
| Trivy/Snyk | Known vulnerabilities (CVE) | Structural resistance to **future** compromise |
| socket.dev | Already-injected malware | Resistance to malware **injection** |
| Scorecard CLI | Per-project detailed scores | **Batch** evaluation across all dependencies |
| deps.dev | Individual SLSA provenance | Combined source + artifact assessment |

## Design Decision

See [ADR-0013](adr/0013-build-integrity-grading.md) for the full design rationale, including scoring algorithm details and implementation decisions.
