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
| resolve-copilot | `/resolve-copilot [PR#] [--dry-run]` | Resolve Copilot review comments on open PRs |

## Immediate Agent Usage

No user prompt needed:
1. Complex feature requests - Use **planner** agent
2. Code just written/modified - Use **code-reviewer** agent
3. Architectural decision - Use **architect** agent

## Code Review Policy

When the user requests a code review (e.g., "レビューして", "review this"), ALWAYS launch **both** agents in parallel:
1. **code-reviewer** — Code quality, idioms, error handling, security
2. **architect** — DDD layer compliance, dependency direction, package structure

This ensures every review covers both implementation quality and architectural correctness.

## Worktree Isolation Policy

Agents that **write files** (Edit, Write) MUST be launched with `isolation: "worktree"` to prevent branch conflicts during parallel development. This gives each agent an isolated copy of the repository.

**Rules:**
- Agents with write tools (`refactor-cleaner`, `doc-updater`) → always `isolation: "worktree"`
- Read-only agents (`planner`, `architect`, `code-reviewer`) → no isolation needed
- If the worktree agent makes changes, review the returned branch and merge manually

```markdown
# GOOD: Write agent with worktree isolation
Agent(subagent_type="refactor-cleaner", isolation="worktree", prompt="...")

# GOOD: Read-only agent without isolation
Agent(subagent_type="code-reviewer", prompt="...")

# BAD: Write agent without isolation (can corrupt working tree)
Agent(subagent_type="doc-updater", prompt="...")
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
