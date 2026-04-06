# ADR-0013: Build Integrity Grading — Supply Chain Build Tamper Resistance

## Status

Proposed

## Context

uzomuzo currently assesses OSS dependency **lifecycle health** (is the project maintained? is it EOL?). However, an actively maintained project can still be a supply chain risk if its build pipeline is poorly protected against tampering.

Recent supply chain attacks (SolarWinds, Codecov, xz-utils, event-stream) demonstrate that attackers target the **build and release pipeline** — injecting malicious code between source and published artifact. The question is not "is this dependency maintained?" but "if an attacker targeted this dependency's build pipeline, how resistant would it be?"

### Data Already Available

uzomuzo already fetches but underutilizes significant supply chain security data:

| Data | Source | Current Usage |
|------|--------|---------------|
| Scorecard overall score | deps.dev Project API | Displayed in detail view |
| Scorecard `Maintained` check | deps.dev Project API | Used in lifecycle assessment |
| Scorecard `Vulnerabilities` check | deps.dev Project API | Used in lifecycle assessment |
| **Other 14 Scorecard checks** | deps.dev Project API | **CSV export only** |
| **SLSA Provenance** (`Verified`, `SourceRepository`, `Commit`) | deps.dev Version API | **Not used (dead code)** |
| **Attestation** (`Verified`, `SourceRepository`, `Commit`) | deps.dev Version API | **Not used (dead code)** |

### Threat Model

The build integrity grade focuses on **build pipeline tamper resistance** — how hard it is for an attacker to:

1. **Inject malicious source** — Push unauthorized commits that bypass review
2. **Poison the CI pipeline** — Exploit CI configuration to execute arbitrary code
3. **Tamper with artifacts** — Modify published artifacts without detection
4. **Hijack dependencies of CI** — Substitute CI-time dependencies (e.g., unpinned Actions)

This is distinct from vulnerability scanning (Trivy/Snyk) which detects **known exploited weaknesses**, and from malware detection (socket.dev) which detects **already-injected malicious code**. Build integrity assesses **structural resistance to future attacks**.

## Decision

### Label System

Introduce a **Build Integrity Label** with numeric score for each dependency. No letter grades (A/B/C/D) — label + score provides sufficient information without redundancy.

| Label | Score Range | Interpretation |
|-------|-------------|----------------|
| `Hardened` | ≥ 7.5 | Strong resistance — core protections in place |
| `Moderate` | 2.5–7.4 | Improvement needed — meaningful gaps present |
| `Weak` | < 2.5 | Minimal resistance — build pipeline largely unprotected |
| `Ungraded` | No signals | Insufficient data to grade |

Score is on the 0–10 scale (same as Scorecard). Thresholds are aligned with Scorecard's risk-level weight boundaries (High=7.5, Low=2.5).

Build Integrity labels are **informational only** and do not affect the verdict. See [Verdict Integration](#verdict-integration).

### Weighting: Aligned with OpenSSF Scorecard Risk Levels

Rather than inventing custom weights, we adopt the **official Scorecard risk-level weights** defined in `ossf/scorecard` (`pkg/scorecard/scorecard_result.go`):

| Risk Level | Weight | Rationale (Scorecard) |
|-----------|--------|----------------------|
| Critical  | 10.0   | Exploitable with no/minimal effort |
| High      | 7.5    | Exploitable with moderate effort |
| Medium    | 5.0    | Requires significant effort or conditions |
| Low       | 2.5    | Theoretical or minimal impact |

This ensures our grading is grounded in the same expert risk assessment that Scorecard uses, avoids arbitrary custom weights, and stays aligned as Scorecard evolves.

### Signals and Weights

All included Scorecard checks use their official risk-level weight. Only signals with sufficient real-world availability are included (validated against a 100-project survey, 2026-04).

| Signal | Source | Risk Level | Weight | Availability | Threat Mitigated |
|--------|--------|-----------|--------|-------------|------------------|
| `Dangerous-Workflow` | Scorecard | Critical | **10.0** | 95% | CI exploitation via `pull_request_target` |
| `Branch-Protection` | Scorecard | High | **7.5** | 39% | Unauthorized commits to default branch |
| `Code-Review` | Scorecard | High | **7.5** | 100% | Unreviewed malicious code |
| `Token-Permissions` | Scorecard | High | **7.5** | 95% | Blast radius of compromised CI token |
| `Binary-Artifacts` | Scorecard | High | **7.5** | 100% | Unreviewable code in repository |
| `Pinned-Dependencies` | Scorecard | Medium | **5.0** | 98% | CI dependency substitution |

**Deferred signals** (insufficient real-world availability for Phase 1, re-evaluate in Phase 4):

| Signal | Availability | Reason for Deferral |
|--------|-------------|---------------------|
| `Signed-Releases` | 12% | Nearly all N/A; no discriminating power |
| `Packaging` | 26% | All 10/10 when present; no discriminating power |
| SLSA Provenance | 3% | Ecosystem adoption too low |
| Attestation | 3% | Ecosystem adoption too low |

**Excluded Scorecard checks** (not related to build tamper resistance):

| Check | Risk Level | Reason for Exclusion |
|-------|-----------|---------------------|
| `Maintained` | High | Already used by lifecycle assessment |
| `Vulnerabilities` | High | Already used by lifecycle assessment |
| `Dependency-Update-Tool` | High | Dependency freshness, not build integrity |
| `Webhooks` | Critical | Not available via deps.dev API |
| `SAST` | Medium | Code quality, not build tamper resistance (same rationale as Fuzzing) |
| `Fuzzing` | Medium | Code quality, not build integrity |
| `Security-Policy` | Medium | Process documentation, not build integrity |
| `SBOM` | Medium | Transparency, not tamper resistance |
| `CI-Tests` | Low | Code quality, not build integrity |
| `CII-Best-Practices` | Low | Process maturity, not build integrity |
| `Contributors` | Low | Community health, not build integrity |
| `License` | Low | Legal, not build integrity |

### Scoring Algorithm

Following Scorecard's own aggregate formula (`GetAggregateScore`):

```
build_integrity_score = Σ(weight_i × score_i) / Σ(weight_i)
```

Where:
- Scorecard checks: `score_i` = check score (0–10 scale, as-is from Scorecard)
- SLSA Provenance Verified: `score_i` = 10 if any provenance is verified, 0 otherwise
- Attestation Verified: `score_i` = 10 if any attestation is verified, 0 otherwise
- Missing/inconclusive checks: **excluded from both numerator and denominator** (same as Scorecard behavior)

If fewer than **3 signals** are evaluated, the label is `Ungraded`. This prevents inflated scores from a small number of "easy" checks (e.g., Dangerous-Workflow + Binary-Artifacts alone could yield 10.0 Hardened despite no branch protection, code review, or signing). The threshold of 3 was validated against 100 popular OSS projects: it preserves grades for Go stdlib (3 evaluated), Kubernetes (3), and Linux kernel (3) while correctly marking projects with only 1-2 evaluated signals as insufficient.

**Example calculation** for a package with Branch-Protection=8, Code-Review=7, Dangerous-Workflow=10, Pinned-Dependencies=3, no SLSA:

```
score = (7.5×8 + 7.5×7 + 10.0×10 + 5.0×3) / (7.5 + 7.5 + 10.0 + 5.0)
      = (60 + 52.5 + 100 + 15) / 30.0
      = 227.5 / 30.0
      = 7.58 out of 10  → Hardened
```

### SLSA/Attestation Version Selection

SLSA Provenance and Attestation are per-version data from the deps.dev Version API. We use the **StableVersion** (latest stable release) as the evaluation target, consistent with uzomuzo's lifecycle assessment which also evaluates the current health of a package based on its latest release.

When `SlsaProvenances` or `Attestations` contain multiple entries, **any single verified entry** is sufficient for the signal to be true. One verified provenance proves that at least one build path has verifiable provenance.

### Missing Signal Handling

Following Scorecard's own convention for inconclusive checks:

- **Exclude missing checks** from both numerator and denominator of the weighted average
- Record as `SignalAbsent` in the assessment trace for transparency
- If **no build-related signals are available at all** (no Scorecard + no SLSA), label is `Ungraded`
- SLSA/Attestation: absence means the signal is simply not included (not penalized as 0)

Rationale: Scorecard excludes inconclusive checks rather than treating them as 0 (`GetAggregateScore` skips `score < 0`). We follow the same principle — a missing check means "we don't know", not "the protection is absent". This avoids unfairly penalizing packages in ecosystems where certain checks are not applicable (e.g., `Packaging` may not apply to all ecosystems).

**Note:** A check that Scorecard **ran and scored 0** means the protection is absent. A check that **was not evaluated** is a different situation. The distinction is preserved in the Signal trace (`SignalUsed` with value "0/10" vs `SignalAbsent`).

### Verdict Integration

**Build Integrity is informational only — it does not affect the verdict.** The verdict is determined solely by the lifecycle assessment (`DeriveVerdict` = `deriveLifecycleVerdict`).

Rationale: Build Integrity flags structural weaknesses in the build pipeline, but users cannot fix another project's Branch Protection or Token Permissions. Since the verdict is meant to prompt actionable responses, including a non-actionable signal in the verdict creates noise (e.g., Active packages downgraded to `caution` because of Moderate build integrity — 76% of OSS scores Moderate). The BUILD column and detail box section provide the information for dependency selection decisions without polluting the verdict.

Build Integrity is **hidden for EOL packages** (verdict `replace`). A package that should no longer be used does not benefit from build pipeline assessment — it is not a realistic attack target since no new releases are expected.

### Type Design: `AssessmentResult.Label` as `string`

`AssessmentResult.Label` is changed from `MaintenanceStatus` to `string`. Each assessor defines its own typed label constants internally:

- `BuildHealthAssessorService` uses `BuildIntegrityLabel` type (`Hardened`, `Moderate`, `Weak`, `Ungraded`)
- `LifecycleAssessorService` continues using `MaintenanceStatus` type (`Active`, `Stalled`, etc.)

Both convert to `string` when populating `AssessmentResult`. This avoids forced coupling between axis-specific label sets while keeping `AssessmentResult` as a generic cross-axis structure.

### CLI Display

**Summary table** — Add `BUILD` column:

```
STATUS  PURL                           LIFECYCLE       BUILD
✅      pkg:golang/go.uber.org/zap     Active          Hardened 8.1
⚠️      pkg:npm/lodash                 Legacy-Safe     Moderate 4.2
🔴      pkg:pypi/requests              Stalled         Weak 1.3
✅      pkg:npm/some-private-pkg       Active          —
```

`—` indicates Ungraded (no Scorecard or SLSA data available).

**Detail view** — Add Build Integrity section to the existing box:

```
┌───────────────────────────────────────────────────────────────┐
│  pkg:npm/lodash@4.17.21                                       │
│  Lifecycle: ⚠️ Legacy-Safe    Build Integrity: Moderate 4.2  │
├─ Build Integrity: Moderate 4.2/10 (4/6) ─────────────────────┤
│   Dangerous Workflow 10  Branch Protection    —               │
│   Code Review         9  Token Permissions    —               │
│   Binary Artifacts    0  Pinned Deps          3               │
│   → https://scorecard.dev/viewer/?uri=github.com/lodash/lodash│
└───────────────────────────────────────────────────────────────┘
```

Signals are displayed in a compact 2-column layout ordered by weight (Critical → High → Medium). Critical/High signals are always shown (including `—` for inconclusive). Medium signals appear only when evaluated. The `(6/11)` in the header indicates how many of the 11 signals were evaluated.

### Output Schema

**JSON:**

```json
{
  "purl": "pkg:npm/lodash@4.17.21",
  "verdict": "caution",
  "lifecycle": "Legacy-Safe",
  "build_integrity": "Moderate",
  "build_integrity_score": 4.2,
  ...
}
```

Summary includes build integrity tallies:

```json
{
  "summary": {
    "total": 10,
    "ok": 3, "caution": 5, "replace": 1, "review": 1,
    "build_integrity": {
      "hardened": 2, "moderate": 4, "weak": 1, "ungraded": 3
    }
  }
}
```

**CSV:**

Add two columns after `lifecycle`: `build_integrity` (label string) and `build_integrity_score` (float).

Individual signal scores are **not** included — the existing `ExportScorecard` CSV already exports all Scorecard check scores. This avoids duplication.

### `--show-only` Filter

Phase 1 uses the existing `--show-only` verdict filter only. Build Integrity does not affect the verdict, so `--show-only` filters on lifecycle verdict exclusively.

A dedicated `--show-only-build` flag is **not added** in Phase 1. If demand is confirmed through user feedback, it can be introduced later without breaking changes.

## Implementation Plan

### Phase 1: Data Pipeline (low cost)

1. Propagate `SLSAProvenance` and `Attestation` from infrastructure `Version` struct to domain `Analysis`
2. Add fields to `Analysis`: `SLSAVerified bool`, `AttestationVerified bool`
3. Use StableVersion's SLSA/Attestation data; any single verified entry = true
4. Populate in `populate_project.go` alongside existing Scorecard flow

### Phase 2: Grading Logic (medium cost)

1. Change `AssessmentResult.Label` from `MaintenanceStatus` to `string`
2. Define `BuildIntegrityLabel` type and constants (`Hardened`, `Moderate`, `Weak`, `Ungraded`)
3. Implement scoring algorithm in `BuildHealthAssessorService.Assess()` (replacing current stub)
4. Define build-integrity Signal constants and record all evaluated checks
5. `DeriveVerdict` uses lifecycle only (build is informational)
6. Test with synthetic table-driven tests covering: all signals present, partial signals, no signals, boundary values (7.5, 2.5), SLSA verified/absent, score 0 vs absent distinction

### Phase 3: Display (low cost)

1. Add BUILD column to summary table (`—` for Ungraded)
2. Add Build Integrity section to detail box
3. Add `build_integrity` and `build_integrity_score` to JSON/CSV output
4. Add `build_integrity` tallies to JSON summary

### Phase 4: Tuning (ongoing)

1. Validate grading against known-compromised packages (event-stream, ua-parser-js, colors.js)
2. Validate score distribution against real-world data using Scorecard snapshots in `testdata/`
3. Consider ecosystem-specific adjustments (e.g., Go modules with sumdb provide some artifact integrity by default)
4. Evaluate whether `--show-only-build` filter is needed based on user feedback

## Consequences

- **Scorecard-aligned weighting** — Weights are derived from Scorecard's official risk levels, not custom heuristics. This makes the grading defensible and easy to explain. If Scorecard changes weights in future versions, we should re-evaluate alignment.
- **New assessment axis visible to users** — `BUILD` column in output; may initially confuse users unfamiliar with supply chain concepts.
- **Score distribution concern** — With missing signals excluded (not penalized), packages with few evaluated checks may receive artificially high grades. Phase 4 tuning should validate the distribution against real-world data and consider a minimum signal count for Hardened.
- **No new API calls** — All data is already fetched; implementation is pure domain logic + display.
- **No verdict escalation** — Build Integrity is informational only. A well-maintained package keeps its `ok` verdict regardless of build integrity score. Users see the BUILD column and detail section for awareness but are not forced to act on non-actionable findings.
- **Ungraded packages are not penalized** — Verdict composition skips Ungraded to avoid mass `review` noise. Users see `—` in the BUILD column and can investigate manually.
- **SLSA/Attestation weight is an editorial decision** — Unlike Scorecard checks, SLSA and Attestation weights (High/Medium) are our assignment, not Scorecard's. Document this clearly so future maintainers know which weights are upstream-derived and which are our own.

## Differentiation

| Tool | What it detects | uzomuzo Build Integrity |
|------|----------------|------------------------|
| Trivy/Snyk | Known vulnerabilities (CVE) | Structural resistance to **future** compromise |
| socket.dev | Already-injected malware | Resistance to malware **injection** |
| Scorecard CLI | Per-project detailed scores | **Batch** evaluation across all dependencies with actionable label |
| deps.dev | Individual SLSA provenance | Combined source + artifact assessment with severity integration |

uzomuzo's unique value: **"This dependency hasn't been compromised yet, but if someone tried, here's how easy it would be."**
