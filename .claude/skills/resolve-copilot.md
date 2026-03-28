---
name: resolve-copilot
description: "Resolve GitHub Copilot review comments on open PRs. Usage: /resolve-copilot [PR#] [--dry-run]"
---

# /resolve-copilot — Resolve Copilot Review Comments

> **Full specification**: See `.github/prompts/resolve-copilot.prompt.md` for the complete
> procedure, classification rules, reply format, and safety rules.

## Quick Reference

- **All open PRs**: `/resolve-copilot` — fix and resolve all unresolved Copilot comments
- **Single PR**: `/resolve-copilot 21` — target a specific PR
- **Dry run**: `/resolve-copilot --dry-run` — classify without modifying anything
- Classify: FIX / ALREADY_FIXED / WONT_FIX
- For each thread: fix code (if FIX) -> commit & push -> reply -> resolve via GraphQL
- After fixes: `go build ./... && go test ./... && go vet ./...`
- Rule learning: detects recurring FIX patterns and proposes additions to `.github/instructions/`
- Runs `make sync-instructions` after rule updates to regenerate `.claude/rules/`
- Never force-push, never modify files outside Copilot's comments scope
