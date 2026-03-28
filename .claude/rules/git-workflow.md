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
2. **One worktree per branch.** Each feature branch gets its own directory.
3. **Clean up after merge.** Remove the worktree once the branch is merged.
4. **Claude Code agents**: Use `isolation: "worktree"` when spawning agents that modify code on a different branch.

### Worktree Cleanup (MANDATORY)

Stale worktrees cause merge conflicts and clutter. Follow these rules:

1. **Before creating a new worktree**: Run `git worktree list` and remove any worktrees whose branch has already been merged or is no longer needed.
2. **After PR merge**: Immediately `git worktree remove <path>` the worktree used for that PR.
3. **At session start**: If `git worktree list` shows 3+ non-main worktrees, proactively clean up merged ones before starting new work.

```bash
# Create a worktree for a feature branch
git worktree add ../uzomuzo-oss-<branch-short-name> <branch-name>

# After merge, clean up
git worktree remove ../uzomuzo-oss-<branch-short-name>

# List active worktrees
git worktree list
```

### When worktree is NOT needed

- Read-only operations (log, diff, blame)

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
