<!-- Generated from .github/instructions/git-workflow.instructions.md — DO NOT EDIT DIRECTLY -->

# Git Workflow

## Commit Message Format

```
<type>: <description>

<optional body>
```

Types: feat, fix, refactor, docs, test, chore, perf, ci

## Pull Request Workflow

When creating PRs:
1. Analyze full commit history (not just latest commit)
2. Use `git diff [base-branch]...HEAD` to see all changes
3. Draft comprehensive PR summary
4. Include test plan with TODOs
5. Push with `-u` flag if new branch

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
