# Agent Orchestration

## Available Agents

| Agent | Purpose | When to Use |
|-------|---------|-------------|
| planner | Implementation planning | Complex features, refactoring |
| architect | System design | Architectural decisions, package structure |
| code-reviewer | Code review | After writing code |
| refactor-cleaner | Dead code cleanup | Code maintenance |
| doc-updater | Documentation | Updating docs, godoc |
| deep-inspector | EOL batch evidence fetching | Deep Inspection orchestration |

## Available Skills

| Skill | Command | When to Use |
|-------|---------|-------------|
| deadcode | `/deadcode [fix] [path]` | Quick dead code audit or interactive cleanup |

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
