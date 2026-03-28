<!-- Generated from .github/instructions/git-workflow.instructions.md — DO NOT EDIT DIRECTLY -->

# Git Workflow

## Commit Message Format

```
<type>: <description>

<optional body>
```

Types: feat, fix, refactor, docs, test, chore, perf, ci

## Branch Protection Policy

**NEVER push directly to main.** All changes MUST go through a pull request, no matter how small. Create a branch, push it, and open a PR. Ask the user before merging.

## Pull Request Workflow

When creating PRs:
1. Create a feature/fix/chore branch from main
2. Analyze full commit history (not just latest commit)
3. Use `git diff [base-branch]...HEAD` to see all changes
4. Draft comprehensive PR summary
5. Include test plan with TODOs
6. Push with `-u` flag if new branch

## Branch Isolation with Git Worktree

Multiple Claude Code sessions or terminals may run concurrently in the same repository. To prevent branch switching conflicts, **always use `git worktree`** for feature branch work.

### Rules

1. **Never `git checkout` a feature branch in the main worktree.** Use `git worktree add` instead.
2. **One worktree per branch.** Each feature branch gets its own directory under `.claude/worktrees/`.
3. **Clean up after merge.** Remove the worktree once the branch is merged.
4. **Claude Code agents**: See `.claude/rules/agents.md` Worktree Isolation Policy for agent-specific rules.

### Worktree Lifecycle: Lock and Cleanup

Stale worktrees accumulate disk clutter, but **deleting an active worktree breaks another session**. Use `git worktree lock` to guard active worktrees.

#### Creating a worktree (ALWAYS lock immediately)

```bash
git worktree add .claude/worktrees/<name> -b <branch-name>
git worktree lock .claude/worktrees/<name> --reason "session active: $(date -Iseconds)"
```

#### Exiting / finishing with a worktree

```bash
# Unlock before removing (only YOUR worktree)
git worktree unlock .claude/worktrees/<name>
git worktree remove .claude/worktrees/<name>
```

#### Safe Cleanup Rules (MANDATORY)

Before removing ANY worktree, you MUST pass **all three checks**:

1. **Lock check**: Run `git worktree list --porcelain` and verify the worktree is **not locked**. If locked, **NEVER remove it** — another session is using it.
2. **Merge check**: Verify the branch is merged into main (`git branch --merged main | grep <branch>`). If not merged, **do not remove** — work may be in progress.
3. **Uncommitted changes check**: `git -C .claude/worktrees/<name> status --porcelain` must be empty. If there are uncommitted changes, **do not remove**.

```bash
# Safe cleanup script (check before each removal)
for wt in .claude/worktrees/*/; do
  name=$(basename "$wt")

  # Skip if locked (another session is active)
  if git worktree list --porcelain | grep -A3 "worktree.*$name" | grep -q "^locked"; then
    echo "SKIP (locked): $name"
    continue
  fi

  # Skip if branch not merged into main
  branch=$(git -C "$wt" rev-parse --abbrev-ref HEAD 2>/dev/null)
  if [ -n "$branch" ] && ! git branch --merged main | grep -q "$branch"; then
    echo "SKIP (not merged): $name ($branch)"
    continue
  fi

  # Skip if uncommitted changes
  if [ -n "$(git -C "$wt" status --porcelain 2>/dev/null)" ]; then
    echo "SKIP (dirty): $name"
    continue
  fi

  echo "REMOVING: $name ($branch)"
  git worktree remove "$wt"
done
```

#### When to run cleanup

- **Before creating a new worktree**: Run the safe cleanup script above.
- **After PR merge**: Remove only the worktree used for that PR (after unlocking).
- **At session start**: If 3+ non-main worktrees exist, run safe cleanup.

#### NEVER do this

- `git worktree remove` on a **locked** worktree — another session depends on it.
- `git worktree remove --force` — bypasses safety checks.
- Remove a worktree whose branch is **not merged** without explicit user confirmation.

### When worktree is NOT needed

- Read-only operations (log, diff, blame)
- Work on the default branch (e.g., `main`) in the main worktree. Do NOT use the main worktree for feature branches.

## Feature Implementation Workflow

1. **Plan First**
   - Use **planner** agent to create implementation plan
   - Identify dependencies and risks
   - Break down into phases

2. **Write Tests**
   - Write table-driven tests first
   - Run `go test ./...` - should FAIL
   - Implement to pass tests
   - Verify coverage with `go test -cover ./...`

3. **Code Review**
   - Use **code-reviewer** agent after writing code
   - Address CRITICAL and HIGH issues
   - Run `go vet ./...` and linter before commit

4. **Commit & Push**
   - Detailed commit messages
   - Follow conventional commits format
