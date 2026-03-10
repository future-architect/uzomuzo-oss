---
description: "Detect and optionally remove dead code (unused functions, types, variables, packages) in the Go codebase"
---

# /deadcode — Dead Code Detection & Removal

You are performing dead code analysis on this Go codebase.

## Mode

Parse the user's arguments to determine the mode:

- **No arguments** or `audit`: Detection only. Report findings, do not modify code.
- **`fix`**: Detection + interactive removal. Ask for confirmation before each batch.
- **`<path>`**: Scope detection to a specific package path (e.g., `/deadcode internal/infrastructure/`).
- **`fix <path>`**: Scoped fix mode.

## Detection Procedure

Run these steps in order and collect all results:

### Step 1: Compiler-Level Checks

```bash
go vet ./... 2>&1
go build ./... 2>&1
```

### Step 2: Unused Module Dependencies

```bash
go mod tidy -v 2>&1 | grep "unused"
```

### Step 3: Dead Code Tool

```bash
deadcode ./... 2>/dev/null || echo "NOTICE: deadcode not installed. Install with: go install golang.org/x/tools/cmd/deadcode@latest"
```

If `deadcode` is not available, fall back to manual detection in Step 4.

### Step 4: Manual Detection

Search for unexported symbols with zero callers:

1. Find unexported function/method definitions (`func lowerCase...`)
2. Grep for each symbol across the entire codebase
3. If only referenced in its own definition (and optionally its own `_test.go`), flag it
4. Check for exported symbols in `internal/` packages that have no importers

When scoped to a path, only scan definitions in that path but still check references globally.

## Classification

Categorize every finding into exactly one category:

| Category | Criteria | Action |
|----------|----------|--------|
| **SAFE** | Unexported, zero references outside definition file, no reflection/generate/linkname usage | Remove immediately (in fix mode) |
| **CAREFUL** | Exported but internal (not in `pkg/uzomuzo/`), test-only references, or part of an interface impl | Remove after confirming no downstream impact |
| **RISKY** | Part of `pkg/uzomuzo/` public API, used via reflect, or unclear reference chain | Report only, never auto-remove |

## Output Format (Audit Mode)

Present findings as a structured report:

```
## Dead Code Report

### SAFE (N items) — can be removed
- `internal/foo/bar.go`: func `helperXyz` — unexported, 0 references
- ...

### CAREFUL (N items) — review recommended
- `internal/domain/analysis/eol.go`: type `OldStatus` — exported but no external imports
- ...

### RISKY (N items) — do not auto-remove
- `pkg/uzomuzo/types.go`: func `DeprecatedMethod` — public API
- ...

### Dependencies
- `go mod tidy` would remove: [list or "none"]

### Summary
Total: N items (S safe, C careful, R risky)
```

If no dead code is found, report "No dead code detected." and stop.

## Fix Mode Procedure

1. Present the full audit report first
2. Ask the user to confirm SAFE removals as a batch
3. For each confirmed SAFE batch:
   a. Remove the dead code (delete functions, remove unused imports)
   b. Run `go build ./... && go test ./... && go vet ./...`
   c. If all pass, report success
   d. If any fail, revert the change and recategorize as CAREFUL
4. Present CAREFUL items individually for user review
5. Never touch RISKY items without explicit user instruction
6. After all removals, run a final `go build ./... && go test ./... && go vet ./...`

## Safety Rules

- ALWAYS verify with `go build ./... && go test ./... && go vet ./...` after removals
- NEVER remove anything in `pkg/uzomuzo/` without explicit user approval
- NEVER remove code referenced via `reflect`, `go:generate`, or `go:linkname`
- Check build tags — a function may be used only in `_test.go` or platform-specific files
- Verify DDD layer boundaries are maintained after changes
- When removing a function, also clean up any orphaned imports it was the sole user of
- Grep for string-based references in comments (e.g., `// see funcName` documentation references)
