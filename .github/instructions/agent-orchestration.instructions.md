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
| review-until-clean | `/review-until-clean` | **1-shot to merge-ready**: Phase A (5-agent local review) → Phase B (push + Copilot review iteration with cron coordination) → Phase C (reply+resolve). Final pre-merge skill |
| deadcode | `/deadcode [fix] [path]` | Quick dead code audit or interactive cleanup |
| batch-issues | `/batch-issues [issue#s] [--dry-run] [--max-parallel N]` | Parallel issue processing with conflict-aware agent dispatch |
| diet-trial | `/diet-trial <org/repo> [--tool trivy\|syft] [--compare]` | Run diet on external OSS for testing, bug finding, and case study data |
| diet-fuzz | `/diet-fuzz <languages\|all> [--count N] [--tool trivy,syft,cdxgen] [--max-parallel N]` | Batch fuzz-test diet across many OSS projects for parser accuracy bugs |

## Immediate Agent Usage

No user prompt needed:
1. Complex feature requests - Use **planner** agent
2. Code just written/modified - Use **code-reviewer** agent
3. Architectural decision - Use **architect** agent

## Code Review Policy

When the user requests a code review (e.g., "レビューして", "review this"), use the `/review-until-clean` skill. The skill is a single 1-shot pipeline:

1. **Phase A** — 5 review agents in parallel (`code-reviewer` + `architect` named subagents + 3 `general-purpose` agents for Code Reuse / Code Quality / PR Hygiene), iterate until 0 findings, build/vet/test/lint
2. **Phase B** — push, then iterate Copilot review cycles (fix, push, wait for re-review) with cron coordination via `<!-- copilot-fix-local:<HEAD> -->` marker
3. **Phase C** — discover all unresolved Copilot threads (paginated GraphQL), classify FIX / ALREADY_FIXED / WONT_FIX, reply + resolve mutation (WONT_FIX is replied but left unresolved for further discussion)

CI runs the same procedure (full Phase A+B+C) via `claude.yml`. See `.claude/skills/review-until-clean/SKILL.md` for the canonical procedure.

### Reviewer Findings Are Input, Not Directives

Reviewer agent findings (including CRITICAL severity) are **discussion input**, not authoritative instructions. Before implementing a reviewer suggestion:

1. **Check ADRs** (`docs/adr/`) — the flagged behavior may be an intentional design decision with documented rationale.
2. **Check conversation history** — the user may have already made this decision earlier in the session.
3. **When in doubt, ask the user** — do not auto-fix reviewer findings that contradict prior decisions.

**Why:** Reviewer agents see only code, not the design intent behind it. In PR #123, a reviewer flagged "transitive advisories not shown on RequestedVersion" as CRITICAL, but this was an intentional decision (see ADR-0011). Blindly implementing the "fix" re-introduced a bug the user had already reported and resolved.

### Workflow-File PRs Are Resolved Locally, Not by CI Claude

PRs whose changes touch `.github/workflows/**` MUST be fixed locally by a human-driven Claude session, not by the auto-fix loop in `copilot-review-fix.yml`.

**Why:** The auto-fix PAT (`GH_ACTIONS_TOKEN`) intentionally lacks the `workflow` scope. Workflow files define CI runtime permissions and secret access — granting a bot the ability to rewrite them creates a privilege-escalation surface (prompt-injection or misbehaviour can self-modify the bot's own runtime). Defense-in-depth: keep PAT scope minimal; pay the small operational cost of manual intervention on workflow PRs.

**How to apply:**
- When CI Claude reports "PAT lacks `workflow` scope" on a workflow-file thread, do NOT propose granting the scope. Pull the branch locally, apply the fix, push.
- The marker dedup in `copilot-review-fix.yml` invalidates on push (HEAD SHA changes), so a manual push cleanly re-arms the auto-fix loop for the next round.
- If the same Copilot finding loops indefinitely on a non-workflow PR, that's a different bug — investigate the auto-fix loop, do NOT relax PAT scope.

**Symptom of the broken case (for debugging):** trigger-claude job runs `success` but logs `trigger already posted for HEAD <sha> (count=1) — skip` because a prior Claude run posted the marker but couldn't push the actual fix.

## Worktree Isolation Policy

Agents that **write files** (Edit, Write) MUST be launched with `isolation: "worktree"` to prevent branch conflicts during parallel development. This gives each agent an isolated copy of the repository.

**Rules:**
- Agents with write tools (`refactor-cleaner`, `doc-updater`) → always `isolation: "worktree"`
- Read-only agents (`planner`, `architect`, `code-reviewer`) → no isolation needed
- If the worktree agent makes changes, review the returned branch and merge manually
- **NEVER remove another agent's worktree.** Agent worktrees created by `isolation: "worktree"` are automatically managed by the Agent tool. Only the spawning session or the agent itself should clean them up.

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
