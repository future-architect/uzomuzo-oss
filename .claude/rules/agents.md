<!-- Generated from .github/instructions/agent-orchestration.instructions.md — DO NOT EDIT DIRECTLY -->

# Agent Orchestration

## Available Agents

| Agent | Purpose | When to Use | Isolation |
|-------|---------|-------------|-----------|
| planner | Implementation planning | Complex features, refactoring | — |
| architect | System design | Architectural decisions, package structure | — |
| code-reviewer | Code review | After writing code | — |
| refactor-cleaner | Dead code cleanup | Code maintenance | `worktree` |
| doc-updater | Documentation | Updating docs, godoc | `worktree` |
| deep-inspector | EOL batch evidence fetching | Deep Inspection orchestration | — |

## Available Skills

| Skill | Command | When to Use |
|-------|---------|-------------|
| deadcode | `/deadcode [fix] [path]` | Quick dead code audit or interactive cleanup |
| review | `/review [PR#] [--copilot-only] [--dry-run]` | Unified review: Claude + Copilot resolution + rule learning |
| batch-issues | `/batch-issues [issue#s] [--dry-run] [--max-parallel N]` | Parallel issue processing with conflict-aware agent dispatch |
| diet-trial | `/diet-trial <org/repo> [--tool trivy\|syft] [--compare]` | Run diet on external OSS for testing, bug finding, and case study data |
| diet-fuzz | `/diet-fuzz <languages\|all> [--count N] [--tool trivy,syft,cdxgen] [--max-parallel N]` | Batch fuzz-test diet across many OSS projects for parser accuracy bugs |

## Immediate Agent Usage

No user prompt needed:
1. Complex feature requests - Use **planner** agent
2. Code just written/modified - Use **code-reviewer** agent
3. Architectural decision - Use **architect** agent

## Code Review Policy

When the user requests a code review (e.g., "レビューして", "review this"), use the `/review` skill.
This skill orchestrates:

1. **Phase 1** — Launch **code-reviewer** + **architect** agents in parallel (Claude review)
2. **Phase 2** — Discover and resolve unresolved **Copilot** review comments (fix, reply, resolve)
3. **Phase 3** — Detect recurring Copilot patterns and propose **rule additions** to prevent repeat feedback

Use `/review --copilot-only` to skip Phase 1 when only Copilot resolution is needed.
See `.github/prompts/review.prompt.md` for the full specification.

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
- **NEVER remove another agent's worktree.** Agent worktrees created by `isolation: "worktree"` are automatically managed by the Agent tool. Only the spawning session or the agent itself should clean them up.

```markdown
# GOOD: Write agent with worktree isolation
Agent(subagent_type="refactor-cleaner", isolation="worktree", prompt="...")

# GOOD: Read-only agent without isolation
Agent(subagent_type="code-reviewer", prompt="...")

# BAD: Write agent without isolation (can corrupt working tree)
Agent(subagent_type="doc-updater", prompt="...")

# BAD: Deleting a worktree you didn't create
git worktree remove .claude/worktrees/some-agent-worktree  # may break another session!
```

## Parallel Task Execution

ALWAYS use parallel Task execution for independent operations:

```markdown
# GOOD: Parallel execution
Launch 3 agents in parallel:
1. Agent 1: Review cmd/root.go changes
2. Agent 2: Review internal/config/ changes
3. Agent 3: Check test coverage

# BAD: Sequential when unnecessary
First agent 1, then agent 2, then agent 3
```
