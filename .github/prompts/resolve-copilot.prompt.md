---
description: "Resolve GitHub Copilot review comments on open PRs by fixing code and marking threads as resolved"
---

# /resolve-copilot — Resolve Copilot Review Comments

You are resolving GitHub Copilot pull request review comments. Your job is to analyze each
unresolved Copilot comment, fix the code if the suggestion is valid, reply to the thread,
and resolve it.

## Arguments

Parse the user's arguments:

- **No arguments**: Process ALL open PRs with unresolved Copilot comments
- **`<PR number>`**: Process only the specified PR (e.g., `/resolve-copilot 21`)
- **`--dry-run`**: Show what would be done without making changes

## Procedure

### Step 1: Discover Unresolved Copilot Threads

For each target PR, run this GraphQL query to find unresolved Copilot review threads:

```bash
gh api graphql -f query='
{
  repository(owner: "{owner}", name: "{repo}") {
    pullRequest(number: {N}) {
      headRefName
      reviewThreads(first: 100) {
        nodes {
          id
          isResolved
          path
          line
          comments(first: 10) {
            nodes {
              databaseId
              author { login }
              body
              diffHunk
            }
          }
        }
      }
    }
  }
}'
```

Filter for threads where:
- `isResolved` is `false`
- First comment author is `copilot-pull-request-reviewer`

If no unresolved Copilot threads exist, report "No unresolved Copilot comments found." and stop.

### Step 2: Checkout the PR Branch

```bash
git fetch origin <headRefName>
git checkout <headRefName>
```

### Step 3: Analyze and Classify Each Thread

For each unresolved thread, read the Copilot comment body, the `diffHunk` for context,
and the current file content at the referenced `path`. Classify as:

| Classification | Criteria | Action |
|---------------|----------|--------|
| **FIX** | Valid suggestion that improves code quality, correctness, or consistency | Apply the fix |
| **ALREADY_FIXED** | The issue has already been addressed in the current code | Reply and resolve |
| **WONT_FIX** | Suggestion is incorrect, irrelevant, cosmetic-only, or conflicts with project conventions | Reply with reason and resolve |

Classification guidelines:
- If Copilot provides a `suggestion` code block, compare it against the current file — if the
  suggestion is already applied or the code has been rewritten, classify as ALREADY_FIXED
- Naming/terminology consistency fixes: usually FIX
- Comment/doc wording improvements: FIX if clearly wrong, WONT_FIX if subjective
- Suggestions that violate project conventions (see CLAUDE.md): WONT_FIX
- Security or correctness issues: always FIX

### Step 4: Apply Fixes

For threads classified as **FIX**:

1. Read the current file content
2. Apply the fix (prefer Copilot's `suggestion` block if provided and correct)
3. Verify the fix compiles: `go build ./...` (for Go files)
4. Group related fixes per file to minimize commits

### Step 5: Commit and Push

After all fixes for a PR are applied:

```bash
go build ./... && go test ./... && go vet ./...
```

If all pass, commit with message format:

```
fix: address Copilot review feedback on PR #<number>

<brief summary of changes>
```

Then push to the PR branch.

### Step 6: Reply and Resolve Each Thread

For each thread, post a reply comment and resolve:

**FIX reply format:**
```
Fixed in <commit-sha>.
```

**ALREADY_FIXED reply format:**
```
Already addressed in the current code.
```

**WONT_FIX reply format:**
```
Not applicable — <brief reason>.
```

To reply to a thread (use the `databaseId` of the first Copilot comment in the thread):

```bash
gh api repos/{owner}/{repo}/pulls/{pr}/comments \
  -f body="<reply text>" \
  -F in_reply_to=<databaseId>
```

To resolve the thread:

```bash
gh api graphql -f query='
mutation {
  resolveReviewThread(input: {threadId: "<thread.id>"}) {
    thread { isResolved }
  }
}'
```

### Step 7: Summary Report

After processing all threads, output a summary:

```
## Copilot Review Resolution Summary

### PR #<number> — <title>
| # | File | Classification | Action |
|---|------|---------------|--------|
| 1 | path/to/file.go:42 | FIX | Fixed naming inconsistency |
| 2 | path/to/other.go:10 | ALREADY_FIXED | Code already updated |
| 3 | path/to/file.go:55 | WONT_FIX | Matches project convention |

Commits: <sha1>, <sha2>
Threads resolved: N/N
```

## Safety Rules

- NEVER force-push or rewrite history
- NEVER modify files outside the scope of Copilot's comments
- Always verify `go build` passes before committing
- If `go test` fails after fixes, revert the failing change and classify as WONT_FIX
- If the PR branch has merge conflicts, skip that PR and report it
- Do not resolve threads you haven't replied to
- If a thread has existing human replies disagreeing with Copilot, classify as WONT_FIX
  and respect the human's decision

## Dry Run Mode

When `--dry-run` is specified:
- Perform Steps 1-3 (discover and classify) only
- Output the summary table with proposed classifications
- Do not modify any files, commit, push, reply, or resolve
