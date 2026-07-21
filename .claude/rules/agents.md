<!-- Generated from .github/instructions/agent-orchestration.instructions.md — DO NOT EDIT DIRECTLY -->

# Agent Orchestration

## Agent invocation

Agents are launched via the **`Task` tool**. The legacy `Agent(...)` API has been replaced with `Task(subagent_type=..., prompt=...)`.

```
Task(subagent_type="<named>", prompt="...")              # named subagent
Task(subagent_type="general-purpose", prompt="...")     # generic agent + freeform prompt
```

`general-purpose` is **not a named subagent_type with a pre-registered prompt template** — it's a generic agent where the prompt you pass becomes the entire instruction. Use it for review focuses or open-ended investigations not covered by a named subagent (e.g., `/review-until-clean` Phase A's "Code Reuse" / "Code Quality" / "PR Hygiene" passes).

## Available subagent types

| subagent_type | Kind | Purpose | When to Use | Isolation |
|---|---|---|---|---|
| planner | named | Implementation planning | Complex features, refactoring | — |
| architect | named | System design | Architectural decisions, package structure | — |
| code-reviewer | named | Code review | After writing code | — |
| refactor-cleaner | named | Dead code cleanup | Code maintenance | `worktree` |
| doc-updater | named | Documentation | Updating docs, godoc | `worktree` |
| deep-inspector | named | EOL batch evidence fetching | Deep Inspection orchestration | — |
| general-purpose | generic | Any focused review / investigation | Review focuses without a named subagent (e.g., Code Reuse / Quality / PR Hygiene), open-ended search | — |

## Available Skills

| Skill | Command | When to Use |
|-------|---------|-------------|
| review-until-clean | `/review-until-clean` | **1-shot to merge-ready (local-only)**: Phase A (5-agent local review) → Phase B (push + self-driven Copilot re-review iteration) → Phase C (reply+resolve). Final pre-merge skill |
| deadcode | `/deadcode [fix] [path]` | Quick dead code audit or interactive cleanup |
| batch-issues | `/batch-issues [issue#s] [--dry-run] [--max-parallel N]` | Parallel issue processing with conflict-aware agent dispatch |
| diet-trial | `/diet-trial <org/repo> [--tool trivy\|syft] [--compare]` | Run diet on external OSS for testing, bug finding, and case study data |
| diet-fuzz | `/diet-fuzz <languages\|all> [--count N] [--tool trivy,syft,cdxgen] [--max-parallel N]` | Batch fuzz-test diet across many OSS projects for parser accuracy bugs |
| review-diff | `/review-diff [BASE_REF \| --cached]` | Pre-push **diff** review by the local Copilot CLI (gpt-5.6-sol, a separate-vendor LLM). Default base=origin/main; pre-empts a Phase B Copilot-bot round-trip. The same Copilot CLI is also wired into `/review-until-clean` Phase A as the always-spawned Reviewer 7. |
| plan-review | `/plan-review [<plan-path>]` | Have the local Copilot CLI (gpt-5.6-sol) critique the latest plan-mode plan file (`/home/node/.claude/plans/*.md`) — a sanity check before ExitPlanMode. |
| plan-debate | `/plan-debate [<plan-path>]` | Heavyweight `/plan-review`: gpt-5.6-sol reviews the plan, Claude ↔ gpt-5.6-sol debate up to 2 rounds, then the `architect` subagent rules neutrally (adopt / revise / reconsider). For high-rework-cost plans (premium ~15-22); send trivial plans to `/plan-review`. |

The three Copilot-driven skills (`/review-diff`, `/plan-review`, `/plan-debate`) require the local `@github/copilot` CLI on `PATH` (`npm install -g @github/copilot`) authenticated via the ambient `gh auth` token, and the `gpt-5.6-sol` model (override with the `COPILOT_MODEL` env var; an empty value drops to the cheaper server-default model). They send code/plan content to GitHub Copilot servers — uzomuzo-oss is public, but do not run them on diffs/plans that carry unpushed secrets (`.env`, credentials, tokens). Skill-name stage prefixes: `plan-*` = plan stage, `review-*` = review stage.

## Immediate Agent Usage

No user prompt needed:
1. Complex feature requests - Use **planner** agent
2. Code just written/modified - Use **code-reviewer** agent
3. Architectural decision - Use **architect** agent (on plan-mode entry, launch **planner + architect** in parallel by default — see "Plan Mode Default Behavior" below)

## Plan Mode Default Behavior

**The default action when entering plan mode is to launch `planner + architect` in parallel in a single message.** Proactive, not reactive ("I'll only call architect if architectural judgment seems needed" is the wrong default).

```
# Default action right after entering plan mode
Task(subagent_type="planner", prompt="...")  +  Task(subagent_type="architect", prompt="...")
```

The only case where skipping is acceptable: **trivial fixes** (typo, comment, single-file refactor, simple flag addition, etc. with no DDD layer placement, repository interface, or cross-layer concerns). When in doubt, launch — calling unnecessarily is safer than not calling when needed.

A `PreToolUse` hook (`.claude/hooks/plan-mode-architect-check.sh`) detects missing architect consultation on `ExitPlanMode` and emits a soft reminder. It is advisory only — `ExitPlanMode` itself is always allowed (never blocked). If the reminder fires, ignore it only if you can immediately answer "this was trivial"; otherwise, consult the architect subagent and re-propose the plan before user approval.

## Code Review Policy

When the user requests a code review (e.g., "レビューして", "review this"), use the `/review-until-clean` skill. The skill is a single 1-shot pipeline:

1. **Phase A** — 5 review agents in parallel (`code-reviewer` + `architect` named subagents + 3 `general-purpose` agents for Code Reuse / Code Quality / PR Hygiene), iterate until 0 findings, build/vet/test/lint
2. **Phase B** — push, then iterate Copilot review cycles (fix, push, re-request review, wait for re-review). The skill drives the Copilot re-review itself via the GraphQL `requestReviews` mutation after each push.
3. **Phase C** — discover all unresolved Copilot threads (paginated GraphQL), classify FIX / ALREADY_FIXED / WONT_FIX, reply + resolve mutation (WONT_FIX is replied but left unresolved for further discussion)

This skill is **local-only**: run `/review-until-clean` from your own machine (your `gh` auth drives the pushes and the Copilot re-review requests). There is **no CI auto-fix** — GitHub Actions never runs Claude to fix Copilot findings. See `.claude/skills/review-until-clean/SKILL.md` for the canonical procedure.

### Reviewer Findings Are Input, Not Directives

Reviewer agent findings (including CRITICAL severity) are **discussion input**, not authoritative instructions. Before implementing a reviewer suggestion:

1. **Check ADRs** (`docs/adr/`) — the flagged behavior may be an intentional design decision with documented rationale.
2. **Check conversation history** — the user may have already made this decision earlier in the session.
3. **When in doubt, ask the user** — do not auto-fix reviewer findings that contradict prior decisions.

**Why:** Reviewer agents see only code, not the design intent behind it. In PR #123, a reviewer flagged "transitive advisories not shown on RequestedVersion" as CRITICAL, but this was an intentional decision (see ADR-0011). Blindly implementing the "fix" re-introduced a bug the user had already reported and resolved.

## Worktree Isolation Policy

Agents that **write files** (Edit, Write) MUST be launched with `isolation: "worktree"` to prevent branch conflicts during parallel development. This gives each agent an isolated copy of the repository.

**Rules:**
- Agents with write tools (`refactor-cleaner`, `doc-updater`) → always `isolation: "worktree"`
- Read-only agents (`planner`, `architect`, `code-reviewer`) → no isolation needed
- If the worktree agent makes changes, review the returned branch and merge manually
- **NEVER remove another agent's worktree.** Agent worktrees created by `isolation: "worktree"` are automatically managed by the Task tool. Only the spawning session or the agent itself should clean them up.

```markdown
# GOOD: Write agent with worktree isolation
Task(subagent_type="refactor-cleaner", isolation="worktree", prompt="...")

# GOOD: Read-only agent without isolation
Task(subagent_type="code-reviewer", prompt="...")

# BAD: Write agent without isolation (can corrupt working tree)
Task(subagent_type="doc-updater", prompt="...")

# BAD: Deleting a worktree you didn't create
git worktree remove .claude/worktrees/some-agent-worktree  # may break another session!
```

## Parallel Task Execution

ALWAYS use parallel Task execution for independent operations:

```markdown
# GOOD: Parallel execution (one message, multiple Task tool calls)
Task(subagent_type="code-reviewer", prompt="Review cmd/root.go changes")
Task(subagent_type="code-reviewer", prompt="Review internal/config/ changes")
Task(subagent_type="general-purpose", prompt="Check test coverage")

# BAD: Sequential when unnecessary
First Task 1, then Task 2, then Task 3
```
