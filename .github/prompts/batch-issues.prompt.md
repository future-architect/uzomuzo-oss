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
1. REPRODUCE (MANDATORY — never skip):
   Write a Go test that reproduces the bug described in the issue.
   Run it and capture the exact terminal output as "Before".
   - Use `GOWORK=off` for all go commands in worktrees.
   - The test MUST fail (or produce wrong output) before the fix and
     pass (or produce correct output) after the fix.
   - If the issue describes behavior on an external OSS project
     (e.g., "run diet on elasticsearch"), do NOT clone the external
     repo. Instead, create a minimal unit test with synthetic fixtures
     (test SBOM, test source files in t.TempDir()) that triggers the
     same bug condition.
   - If you cannot reproduce the bug at all, report back with evidence
     instead of guessing. Do NOT proceed to IMPLEMENT.

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

6. VERIFY AFTER (MANDATORY — never skip):
   Re-run the reproduction test from Step 1. Capture the exact
   terminal output as "After". The test must now pass / show correct
   output. If it still fails, go back to Step 3.

7. COMMIT & PUSH: Commit with conventional commit format.
   Include `Closes #<number>` in the body. Push the branch.

8. CREATE PR: Use `gh pr create` with the EXACT template below.
   Replace placeholders with real content. The Before/After sections
   are MANDATORY — paste actual terminal output, not hand-written
   tables or expected values.

   ```
   ## Summary
   <1-3 bullet points describing what changed and why>

   ## Before (reproduction test output)
   ```
   <paste exact terminal output from Step 1 — the failing/wrong test>
   ```

   ## After (verification test output)
   ```
   <paste exact terminal output from Step 6 — the passing test>
   ```

   Closes #<number>

   ## Test plan
   - [x] <test case 1>
   - [x] <test case 2>
   - [ ] CI green

   🤖 Generated with [Claude Code](https://claude.com/claude-code)
   ```

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

### Phase 7: Evidence Verification (MANDATORY)

After Phase 6, verify every PR has proper test evidence. Agents often
produce hand-written tables or skip Before/After — this phase catches
and fixes that.

For each PR created in this batch:

1. **Check**: Run `gh pr view <N> --json body -q .body` and verify it
   contains BOTH a `## Before` and `## After` section, each with a
   fenced code block (` ``` `) containing real terminal output (lines
   starting with `=== RUN`, `--- PASS`, `--- FAIL`, `PASS`, `ok`, or
   similar Go test output patterns).

2. **Remediate** if either section is missing or contains only a table:
   a. Fetch the PR branch: `gh pr view <N> --json headRefName`
   b. Create a temporary detached worktree:
      `git worktree add /tmp/pr<N> origin/<branch> --detach`
   c. Identify the new test functions from the diff:
      `git diff origin/main...origin/<branch> -- '*.go' | grep '+func Test'`
   d. Run them with verbose output:
      `cd /tmp/pr<N> && GOWORK=off go test -run '<TestName>' -v ./<package>/...`
   e. Capture the output and update the PR body with `gh pr edit`.
   f. Clean up: `git worktree remove /tmp/pr<N>`

3. **Case-study enrichment** (optional): If you have access to prior
   diet output data (e.g., from memory or local files) that shows the
   bug in a real-world project, add it to the `## Before` section as
   additional evidence. Label it clearly:
   `## Before (case-study evidence: <source>)`

4. Report which PRs were remediated in the summary table.

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
