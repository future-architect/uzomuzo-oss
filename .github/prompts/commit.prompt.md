---
description: "Safe commit — branch guard + build/test/lint pre-commit checks"
argument-hint: "optional commit message override (auto-generated from changes if omitted)"
---

## Pre-Commit Checks

Before creating a commit, run all of the following checks **in order**. If any check fails, **stop immediately and report**.

### Step 1: Branch Verification

```bash
git branch --show-current
```

- Confirm you are NOT on `main` / `master` (double-check even though Branch Protection blocks it)
- Confirm you are on the intended branch

### Step 2: Build

```bash
go build ./...
```

### Step 3: Test

```bash
go test ./...
```

### Step 4: Lint

```bash
golangci-lint run
```

### Step 5: Staging Review

```bash
git status
git diff --cached
git diff
```

- Ensure no secrets (.env, credentials, API keys) are staged
- Ensure no unintended files are included

## Commit

Only after all checks pass:

1. Stage relevant files (prefer explicit filenames over `git add -A`)
2. Commit with a conventional commits format message (always in English)
3. Run `git status` after commit to verify success

If the user provides a commit message argument, use it. Otherwise, generate an appropriate message from the changes.
