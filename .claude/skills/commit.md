---
name: commit
description: "Safe commit with branch verification and build/test/lint checks"
argument-hint: "optional commit message override"
---

# /commit — Safe Commit

> **Full specification**: See `.github/prompts/commit.prompt.md` for the complete
> pre-commit procedure and safety checks.

## Quick Reference

- **Default**: `/commit` — run all checks and commit with auto-generated message
- **With message**: `/commit fix: resolve nil pointer in batch processing`
- Checks: branch guard → build → test (with `-race`) → vet → lint → staging review
- Blocks commit on `main`/`master` branches
- Prefers explicit `git add <file>` over `git add -A`
