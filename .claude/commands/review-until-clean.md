---
description: "Iterative pre-push review — /review Phase 1 + /simplify, repeat until clean, then push"
---

# Iterative Review & Fix

Run `/review` Phase 1 (code-reviewer + architect) AND `/simplify` Phase 2 (reuse + quality + efficiency) **together**, fix all findings, and repeat until zero issues remain. Then push.

This command exists to catch issues **before** Copilot sees them.

## Process

### Step 1: Get the Diff

Detect the associated PR and get the diff:

```bash
PR=$(gh pr list --head "$(git branch --show-current)" --json number --jq '.[0].number')
```

- If a PR exists: `gh pr diff $PR`
- Otherwise: `git diff main...HEAD`

### Step 2: Launch Five Review Agents in Parallel

Use the Agent tool to launch **all five** agents concurrently in a single message. Provide each with the full diff.

#### Agent 1: Code Reviewer (from /review)

Code quality, Go idioms, error handling, security. Return structured findings with severity (CRITICAL / HIGH / MEDIUM / LOW), file, line, and suggested fix.

Key areas:
- Error handling: silenced errors (`_ = err`), missing error wrapping, inconsistent error checking patterns
- Resource cleanup: `t.Cleanup` for process-global state AND explicit error-checked close on the normal path (both required, not either/or)
- API contracts, nil safety, race conditions

#### Agent 2: Architect (from /review)

DDD layer compliance, dependency direction, package structure. Return structured findings with severity, file, line, and suggested fix.

#### Agent 3: Code Reuse (from /simplify)

- Search for existing utilities/helpers that could replace newly written code
- Flag new functions that duplicate existing functionality
- Flag inline logic that could use an existing utility

#### Agent 4: Code Quality (from /simplify)

- Redundant state, parameter sprawl, copy-paste with variation
- Leaky abstractions, stringly-typed code
- Unnecessary comments (WHAT not WHY)
- Bugs: nil dereference, panics, edge cases

#### Agent 5: PR Hygiene

Check the PR metadata against the actual diff:
- Does the PR title accurately describe the changes?
- Does the PR description list all significant changes, or is it missing something?
- Are there independent concerns mixed in one PR that should be split?
- If the PR includes changes not mentioned in the description, flag them

### Step 3: Fix or Dismiss

Wait for all five agents. For each finding:
- **Genuine issue**: Fix it directly
- **False positive / not worth fixing**: Note it and move on

**Critical rule**: When fixing, do NOT degrade existing error handling. Specifically:
- Do NOT replace error-checked operations with `_ = err`
- Do NOT remove explicit resource cleanup just because `t.Cleanup`/`defer` exists — both serve different purposes (safety net vs. normal-path diagnostics)
- If unsure whether something is redundant, leave it as-is

### Step 4: Verify

After fixing, run:

```bash
go build ./...
go vet ./...
go test ./... -short -count=1
```

If tests fail, revert the failing fix and classify as unfixable.

### Step 5: Repeat or Finish

If ANY fixes were made in Step 3, **go back to Step 2** with fresh agents. Tell them what was already fixed so they focus on NEW issues only.

If all five agents report **zero new issues**, the code is clean.

### Step 6: Commit and Push

1. `git add` changed files
2. Commit with a descriptive message
3. `git push`
4. If push is rejected (e.g. Copilot auto-commits arrived), fetch and rebase, then push again

## Rules

- Maximum 5 rounds (circuit breaker)
- If an issue keeps reappearing across rounds, it's a false positive — skip it
- Do NOT fix cosmetic preferences or style nits
- Only fix issues within the diff — do not refactor unrelated code
- Do NOT post review comments to the PR — fix the code directly
