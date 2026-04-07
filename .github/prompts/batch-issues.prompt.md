---
description: "Batch-process open GitHub issues with parallel agents. Analyzes conflicts, dispatches workers, and creates PRs."
---

# /batch-issues -- Parallel Issue Dispatcher

You are orchestrating parallel resolution of multiple GitHub issues using
isolated Agent workers. Each worker runs in its own worktree to prevent
branch conflicts.

## Arguments

Parse the user's arguments:

- **No arguments**: Auto-select small/medium open issues, analyze conflicts, dispatch
- **`<issue numbers>`**: Process specific issues (e.g., `/batch-issues 168 171 187`)
- **`--dry-run`**: Analyze and show the dispatch plan without launching agents
- **`--max-parallel N`**: Maximum parallel agents (default: 4)

## Procedure

### Phase 1: Issue Selection

1. Run `gh issue list --state open --limit 30 --json number,title,labels,body`
2. Exclude meta-issues (Dependency Dashboard, release checklists, scan reports)
3. Exclude issues labeled `documentation` (unless explicitly requested)
4. If specific issue numbers were provided, use those; otherwise auto-select

### Phase 2: Conflict Analysis

For each candidate issue:

1. Read the issue body to identify which files/packages it likely touches
2. Search the codebase (`grep`, `Glob`) to confirm file targets
3. Build a conflict matrix: issues that modify the same files cannot run in parallel

Group issues into **conflict groups**:
- Issues sharing files go in the same group (run sequentially within group)
- Issues with no shared files are independent (run in parallel across groups)

### Phase 3: Size & Risk Assessment

For each issue, classify:
- **Small**: Single function fix, test addition, output formatting (< 50 lines changed)
- **Medium**: Multi-function fix, new test file, query changes (50-200 lines)
- **Large**: Cross-cutting refactor, new feature, architectural change (> 200 lines)
- **Exclude**: Issues requiring design decisions, user input, or external dependencies

Exclude `Large` issues unless explicitly requested. Present the plan to the user.

### Phase 4: Dispatch Plan

Present the dispatch plan as a table:

```
| Batch | Issues | Conflict Group | Estimated Size |
|-------|--------|----------------|----------------|
| 1     | #168, #171, #187, #180 | independent | S, M, M, M |
| 2     | #181 (after #171), #186 (after #187) | C, A | M, M |
```

If `--dry-run`, stop here and show the plan.

### Phase 5: Agent Dispatch

For each batch, launch agents **in parallel** using the Agent tool with
`isolation: "worktree"` and `run_in_background: true`.

Each agent receives this workflow instruction:

#### Per-Issue Agent Workflow

```
1. REPRODUCE: Build the project, set up test fixtures from the issue body,
   run the reproduction steps. Capture exact "Before" output.
   - Use `GOWORK=off` for all go commands in worktrees.
   - If reproduction fails, report back instead of guessing.

2. ARCHITECT: Launch an architect agent (subagent_type="architect") to
   review the planned approach. Incorporate feedback.
   - If the architect identifies multiple valid approaches or unclear
     requirements, use AskUserQuestion to ask the human.

3. IMPLEMENT: Apply the fix. Follow all project conventions:
   - CLAUDE.md, .claude/rules/coding-standards.md
   - DDD layer boundaries
   - Error handling with context wrapping
   - Table-driven tests

4. TEST: Run `GOWORK=off go test ./...` to verify all tests pass.

5. REVIEW: Use the Skill tool to invoke `review-until-clean`.
   This is MANDATORY -- do not skip. The skill runs iterative review
   and fixes issues until the review passes clean. Only after
   review-until-clean completes with zero issues should you proceed.

6. VERIFY AFTER: Re-run the reproduction from Step 1. Capture "After"
   output showing the fix works.

7. COMMIT & PUSH: Commit with conventional commit format.
   Include `Closes #<number>` in the body. Push the branch.

8. CREATE PR: Use `gh pr create` with:
   - Descriptive title
   - Body containing:
     - Summary of changes
     - Before/After output (exact terminal output from Steps 1 and 6)
     - `Closes #<number>`
     - Test plan checklist

9. NEXT ISSUE: If there are successor issues in the same conflict group,
   fetch the next issue with `gh issue view <N> --json body` and repeat
   from Step 1. The successor issues MUST be done in the same worktree
   to avoid conflicts.
```

### Phase 6: Monitor & Report

After all agents complete:

1. Collect results from each agent
2. Present a summary table:

```
| Issue | Status | PR | Notes |
|-------|--------|-----|-------|
| #168  | PR created | #197 | Quick Wins always shown |
| #185  | Already fixed | -- | Closed as duplicate |
```

3. If any agent failed, report the failure reason
4. List remaining unprocessed issues for next batch

## Safety Rules

- **Never dispatch agents for issues requiring user design decisions** without
  confirming the approach first
- **Never run more than `--max-parallel` agents simultaneously**
- **Each agent MUST run `review-until-clean`** before creating a PR
- **Conflict groups are strict** -- never run two issues from the same group
  in parallel
- **If an agent's issue turns out to be already fixed**, close the issue and
  move to the next one in the group
- **Agents must not modify files outside their issue scope** -- no drive-by
  refactoring
