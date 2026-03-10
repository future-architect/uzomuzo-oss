---
name: deadcode
description: "Detect and optionally remove dead code in the Go codebase. Usage: /deadcode [fix] [<path>]"
---

# /deadcode — Dead Code Detection & Removal

> **Full specification**: See `.github/prompts/deadcode.prompt.md` for the complete
> detection procedure, classification rules, output format, and safety rules.

## Quick Reference

- **Audit mode** (default): `/deadcode` — detect and report only
- **Fix mode**: `/deadcode fix` — detect, remove SAFE items, confirm CAREFUL items
- **Scoped**: `/deadcode internal/infrastructure/` — limit to a package path
- Detection: `go vet`, `go build`, `deadcode ./...`, `go mod tidy -v`, manual grep
- Classify: SAFE / CAREFUL / RISKY
- After each removal batch: `go build ./... && go test ./... && go vet ./...`
- Never auto-remove `pkg/uzomuzo/` public API or reflect/generate/linkname targets
