# ADR-0002: Ecosystem-Aware Commit Delivery and Scorecard Absence Handling for Active Classification

## Status

Accepted (2026-03-13)

## Context

The lifecycle assessment treated "recent commits without recent registry publish" identically across all ecosystems and all Scorecard availability states. Two distinct problems existed:

**Problem 1: Ecosystem delivery model ignored.** For registry-dependent ecosystems (npm, PyPI, Maven, etc.), requiring a publish is correct — commits alone do not deliver updates to consumers. But some ecosystems deliver packages directly via VCS:

| Ecosystem | Delivery mechanism | Commit = delivery? |
|-----------|-------------------|-------------------|
| **Go (golang)** | `go get` resolves via Git tags/commits through Go module proxy | **Yes** |
| **Composer (PHP)** | Composer resolves packages from VCS repositories (VCS mode) | **Yes** |
| npm | `npm install` from registry only | No |
| PyPI | `pip install` from registry only | No |
| Maven | Central/Sonatype publish required | No |
| NuGet | nuget.org publish required | No |
| Cargo | crates.io publish required | No |
| RubyGems | rubygems.org publish required | No |

For VCS-direct ecosystems, penalizing packages for "no recent publish" when they have recent commits produces false-negative Active classifications.

**Problem 2: Scorecard absence conflated with low score.** `IsMaintenanceOk()` returned `false` for both "Maintained score < 3" and "no Scorecard data at all". In the commits-only branch, this meant 85 out of 87 Stalled packages had no Scorecard whatsoever — they were penalized for missing third-party metrics, not for proven low maintenance. Only 2 packages had an actual low Maintained score.

| `IsMaintenanceOk()` = false | Count (of 87) | Actual meaning |
|------------------------------|---------------|----------------|
| Scorecard absent (Maintained key missing) | ~85 | **Unknown** — no data |
| Scorecard present, Maintained < 3 | ~2 | **Proven low** |

## Decision

### Decision 1: Ecosystem-aware delivery model

#### Domain model addition

Add `Analysis.IsVCSDirectDelivery() bool` to the domain aggregate. This method returns `true` for ecosystems where Git commits are the delivery mechanism (`golang`, `composer`), and `false` for all others.

#### Assessment logic change

In `assessActiveState`, when a package has recent human commits but no recent registry publish:

1. **VCS-direct ecosystem** → **Active** (regardless of maintenance score)
2. **Registry-dependent ecosystem** → tri-state maintenance check (see Decision 2)

### Decision 2: Distinguish "maintenance unknown" from "maintenance low"

In `assessActiveState` for registry-dependent ecosystems with recent commits but no publish, the maintenance check becomes a three-way branch:

```
hasRecentCommit only (registry-dependent):
  1. Maintained score ≥ 3        → Active  (confirmed adequate maintenance)
  2. Maintained score absent      → Active  (unknown ≠ low; commits prove maintainer exists)
  3. Maintained score < 3         → Stalled (Scorecard confirms low maintenance)
```

#### Rationale

- **Scorecard absence is normal** for small/niche packages — it is not a negative signal
- **Recent human commits are direct evidence** of an active maintainer; this is strictly stronger than any third-party score
- Only when Scorecard **explicitly reports** low maintenance should that override the commit signal
- The Reason text now distinguishes "maintenance score unavailable (Scorecard not found)" from "maintenance score < 3", giving operators accurate information

### Why in the domain layer?

The delivery model is an intrinsic property of the ecosystem, not an infrastructure concern. It determines how the domain interprets activity signals, so it belongs on the `Analysis` aggregate as a domain method.

## Alternatives Considered

### 1. Configuration-driven ecosystem list

Rejected. The VCS-direct vs. registry-dependent distinction is a stable, well-known property of each ecosystem. Making it configurable would add unnecessary complexity per the project's Configuration & Flags Policy (YAGNI).

### 2. Treat all commits-only as Active

Rejected. For npm/PyPI/Maven, commits without publishing genuinely do not reach consumers. An npm user running `npm install` will get the old published version regardless of how many commits exist. Classifying these as Active would be misleading.

### 3. Add a "Developing" intermediate label

Rejected. Adding a new label increases complexity across the entire output chain (CSV export, CLI display, downstream consumers) for a narrow edge case. The existing Active/Stalled distinction is sufficient when informed by ecosystem context.

## Consequences

### Positive

- Go and Composer packages with active commit history are correctly classified as Active
- No false penalization for ecosystems where registry publish is not the delivery mechanism
- ~85 npm packages with recent commits but no Scorecard are correctly classified as Active instead of Stalled
- Packages with Scorecard-confirmed low maintenance (Maintained < 3) remain Stalled — no false positives
- Reason text now accurately distinguishes "unavailable" from "low", improving operator trust
- Decision logic is explicit and testable via `IsVCSDirectDelivery()` and Scorecard presence checks

### Negative

- New ecosystem additions require updating `IsVCSDirectDelivery()` (low risk: ecosystem list changes very rarely)
- Packages with genuinely poor maintenance but no Scorecard will be classified as Active rather than Stalled. This is acceptable because:
  - Recent commits are a stronger first-party signal than Scorecard's absence
  - Scorecard coverage is expanding; as more packages gain scores, these will be re-evaluated
  - The alternative (penalizing all Scorecard-absent packages) produces far more false negatives

### Neutral

- No impact on existing npm/PyPI/Maven/NuGet/Cargo/RubyGems classifications when Scorecard is present
- No configuration changes required
