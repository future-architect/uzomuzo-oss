---
description: "Unified code review: launch Claude reviewers, resolve Copilot comments, and learn coding rules from patterns"
---

# /review — Unified Code Review

You are performing a comprehensive code review that combines Claude's own review,
Copilot comment resolution, and rule learning from recurring patterns.

## Arguments

Parse the user's arguments:

- **No arguments**: Review the current branch's changes AND resolve Copilot comments on the associated PR
- **`<PR number>`**: Target a specific PR (e.g., `/review 21`)
- **`--copilot-only`**: Skip Claude's review, only resolve Copilot comments and learn rules
- **`--dry-run`**: Show classifications and proposed rules without making changes

## Procedure

### Phase 1: Claude Code Review

Skip this phase if `--copilot-only` is specified.

Launch **both** review agents in parallel:

1. **code-reviewer** agent — Code quality, Go idioms, error handling, security
2. **architect** agent — DDD layer compliance, dependency direction, package structure

Provide each agent with the relevant diff context:
- If a PR number is given, use `git diff main...HEAD`
- Otherwise, use `git diff` for uncommitted changes or `git diff HEAD~1` for the last commit

Wait for both agents to complete and present their findings to the user.

### Phase 2: Resolve Copilot Review Comments

#### Step 2.1: Identify the Target PR

- If a PR number was given as argument, use it directly
- Otherwise, detect the PR for the current branch:

```bash
gh pr list --head "$(git branch --show-current)" --json number,title --jq '.[0]'
```

If no PR exists for the current branch, skip Phase 2 and Phase 3.

#### Step 2.2: Discover Unresolved Copilot Threads

First, detect the repository owner and name from the git remote:

```bash
gh repo view --json owner,name --jq '"\(.owner.login)/\(.name)"'
```

Then run this GraphQL query to find unresolved Copilot review threads
(substitute `{owner}`, `{repo}`, and `{N}` with actual values):

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

If no unresolved Copilot threads exist, report "No unresolved Copilot comments found."
and skip to Phase 3.

#### Step 2.3: Checkout the PR Branch

```bash
git fetch origin <headRefName>
git checkout <headRefName>
```

#### Step 2.4: Analyze and Classify Each Thread

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

#### Step 2.5: Apply Fixes

For threads classified as **FIX**:

1. Read the current file content
2. Apply the fix (prefer Copilot's `suggestion` block if provided and correct)
3. Verify the fix compiles: `go build ./...` (for Go files)
4. Group related fixes per file to minimize commits

#### Step 2.6: Commit and Push

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

#### Step 2.7: Reply and Resolve Each Thread

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

### Phase 3: Learn from Copilot — Update Local Coding Rules

After resolving all threads, analyze the FIX-classified comments for **recurring patterns**
that the project's coding rules should prevent in the future.

#### Step 3.1: Pattern Detection

Group all FIX comments (from the current run AND from already-resolved Copilot threads on the
same PRs) by category. Look for patterns such as:

| Pattern Category | Example Copilot Feedback |
|-----------------|--------------------------|
| Naming consistency | "Variable still uses old terminology" |
| Comment/doc drift | "Comment says X but code does Y" |
| Error handling | "Error not wrapped with context" |
| Defensive coding | "Nil check missing", "Fallback needed" |
| API consistency | "Public function missing doc comment" |
| Security | "User input not validated" |

A pattern is **recurring** if Copilot has flagged the same category **2+ times** across
any threads (current or historical on the same repo).

#### Step 3.2: Map Pattern to Rule File

Each pattern maps to an existing rule file:

| Pattern Category | Target Rule File |
|-----------------|------------------|
| Naming consistency | `.github/instructions/coding-standards.instructions.md` |
| Comment/doc drift | `.github/instructions/coding-standards.instructions.md` |
| Error handling | `.github/instructions/error-handling.instructions.md` |
| Defensive coding | `.github/instructions/coding-standards.instructions.md` |
| API consistency | `.github/instructions/coding-standards.instructions.md` |
| Security | `.github/instructions/security.instructions.md` |
| Testing | `.github/instructions/testing-performance.instructions.md` |
| DDD violations | `.github/instructions/ddd-architecture.instructions.md` |

If no existing file fits, add to `coding-standards.instructions.md` as a new section.

#### Step 3.3: Propose Rule Additions

For each recurring pattern, draft a concise, actionable rule. Rules must be:

- **Specific**: "When renaming a type, update all variable names, comments, and output labels
  that reference the old name" — not "Keep things consistent"
- **Preventive**: Describe what to do BEFORE the mistake, not what the mistake looks like
- **Minimal**: One rule per pattern, 1-3 sentences max

Format the proposal:

```
## Proposed Rule Additions

### → coding-standards.instructions.md
> **Rename Completeness**: When renaming a type, constant, or function, search the entire
> codebase for the old name in variable names, comments, log messages, CLI output, and
> documentation. Update all occurrences in the same commit.

### → error-handling.instructions.md
> (none — no recurring patterns detected)
```

#### Step 3.4: Apply Rules (with confirmation)

1. Show the proposed rules to the user and ask for confirmation
2. For each confirmed rule, append it to the appropriate `.github/instructions/` file
   under a `## Learned from Copilot Reviews` section (create the section if it doesn't exist)
3. Run `make sync-instructions` to regenerate `.claude/rules/`
4. Commit with message: `chore: update coding rules based on Copilot review patterns`

If `--dry-run` is active, only show the proposals without modifying files.

#### Step 3.5: Deduplication

Before adding a rule, check if an equivalent rule already exists in the target file
(including any prior `Learned from Copilot Reviews` section). If a similar rule exists,
either skip or refine the existing rule instead of adding a duplicate.

### Phase 4: Summary Report

After all phases complete, output a unified summary:

```
## Review Summary

### Claude Review
- code-reviewer: APPROVE / WARNING / BLOCK
- architect: APPROVE / WARNING / BLOCK

### Copilot Resolution — PR #<number>
| # | File | Classification | Action |
|---|------|---------------|--------|
| 1 | path/to/file.go:42 | FIX | Fixed naming inconsistency |
| 2 | path/to/other.go:10 | ALREADY_FIXED | Code already updated |
| 3 | path/to/file.go:55 | WONT_FIX | Matches project convention |

Commits: <sha1>, <sha2>
Threads resolved: N/N

### Rules Learned
| Pattern | Rule Added To | Summary |
|---------|--------------|---------|
| Rename completeness | coding-standards | Update all references when renaming |
| (none detected) | — | — |
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
- Phase 1: Launch review agents normally (read-only anyway)
- Phase 2: Discover and classify only (Steps 2.1-2.4), no fixes/commits/replies
- Phase 3: Pattern detection and proposals only (Steps 3.1-3.3), no rule file changes
- Phase 4: Output the summary with proposed classifications and rules
