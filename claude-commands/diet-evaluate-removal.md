---
description: "6-axis deep evaluation of whether a dependency is worth removing"
arguments:
  - name: module
    description: "Module path to analyze (e.g. github.com/pkg/errors)"
    required: true
---

# Diet Removal Evaluation: $ARGUMENTS

Deep-dive evaluation of whether `$ARGUMENTS` is worth removing, using a 6-axis framework. This command picks up where `uzomuzo diet` leaves off — diet gives you the priority ranking, this gives you the removal plan.

**When to use**: After `uzomuzo diet` ranks a dependency as moderate/easy but you're unsure whether to invest the effort. Also useful for "hard" dependencies where you need a concrete migration plan.

**Relationship to diet**:
- `uzomuzo diet` tells you **ranking and difficulty** (automated, all dependencies at once)
- `/diet-evaluate-removal` tells you **exactly how to remove it** and **whether it's worth it** (LLM-powered, one dependency at a time)

## Phase 1: Gather diet context

If a diet plan is available, extract the target dependency's automated scores first:

```bash
uzomuzo diet --sbom bom.json --source . --format json | jq '.dependencies[] | select(.name == "$ARGUMENTS")'
```

Note the following from diet (do NOT re-compute these):
- **ONLY-VIA-THIS**: transitive deps removed together
- **FILES / CALLS / API breadth**: coupling metrics
- **Difficulty**: trivial / easy / moderate / hard
- **Lifecycle**: maintenance status

## Phase 2: Analysis beyond diet

Focus on what diet's automated analysis cannot determine:

### 1. Full removal feasibility

Check whether removing direct usage actually eliminates the module, or if it stays as an indirect dependency.

For Go projects:
```bash
go mod why -m $ARGUMENTS
# or faster: go mod graph | grep " $ARGUMENTS@"
```

For other ecosystems: check if other direct dependencies pull this in transitively.

### 2. API leakage check

**Critical**: Do any types from this dependency appear in EXPORTED identifiers?
- If yes → removing it is a **breaking change** for downstream consumers
- If no → internal swap, no API impact

This is the single most important factor diet cannot detect.

### 3. Generated code check

For each file importing the dependency, check for:
- `// Code generated` headers
- `//go:generate` directives referencing this module's tools

Generated files are trivially migrated by re-running the generator with a replacement tool.

### 4. Replacement options

Evaluate concrete alternatives (check compatibility with the project's language version):

| Option | Estimated effort | Risk | Notes |
|--------|-----------------|------|-------|
| Standard library | N lines | Low | Available since version X |
| Consolidate into existing dep | N lines | Low | Already in go.mod/package.json |
| Smaller alternative | N lines | Med | New dependency, but smaller |
| Self-implement | N lines (+ N test lines) | Varies | Only if non-crypto, non-protocol |

**Do NOT self-implement** crypto, protocol, or security-related functionality.

### 5. Maintenance burden history

```bash
git log --all --oneline --grep="$ARGUMENTS" --since="2023-01-01" | wc -l
```

How many PRs/commits were caused by this dependency? (Dependabot updates, breaking changes, etc.)

## Phase 3: 6-Axis Evaluation

| Axis | Rating | Notes |
|------|--------|-------|
| Update PR reduction | High/Med/Low | N PRs/year eliminated |
| Clean dependency list | High/Med/Low | Does removing clarify intent? |
| Code standardization | High/Med/Low | Can it be replaced with stdlib? |
| Supply chain risk | High/Med/Low | Is it abandoned + handles sensitive data? |
| Code portability | High/Med/Low | Does removing make code easier to extract? |
| Future removal readiness | High/Med/Low | Does this unblock further cleanups? |

### Rating anchors

| Axis | High | Medium | Low |
|------|------|--------|-----|
| Update PR reduction | >10 PRs/year | 3-10 PRs/year | 0-2 PRs/year |
| Clean dependency list | Removes confusing dep | One of many similar | Marginal |
| Code standardization | Full stdlib replacement | Partial stdlib | No alternative |
| Supply chain risk | Abandoned + credentials/crypto | Abandoned + config/metadata | Maintained or test-only |
| Code portability | Types in exported API | Internal, moderate coupling | Isolated |
| Future removal readiness | Blocks 3+ other deps | Blocks 1-2 deps | No blocking |

## Phase 4: Verdict

```
### Verdict: $ARGUMENTS

**Diet ranking**: #{rank}, priority {score}, difficulty {level}
**Full removal possible**: Yes / Stays as indirect (via X)
**API leakage**: Yes (breaking change) / No

**Recommendation**: Full removal / Self-implement / Keep (wrap) / Test-only (ignore)
**Effort**: Trivial (<1h) / Small (1-4h) / Medium (1-3d) / Large (1+ week)
**Priority**: S (immediate) / A (soon) / B (when convenient) / C (keep)
**Cost-effectiveness**: {effort vs. 6-axis summary}
```

## Important rules

- **Start from diet's output.** Don't re-compute FILES, CALLS, ONLY-VIA-THIS, or lifecycle status.
- **Focus on what diet can't determine**: API leakage, generated code, replacement feasibility, maintenance history, 6-axis evaluation.
- **Be concrete about replacements.** Don't just say "use stdlib" — name the specific function and show the migration pattern.
- **This works for any language.** Adapt the Go-specific commands (go mod why, go mod graph) to the project's ecosystem.
