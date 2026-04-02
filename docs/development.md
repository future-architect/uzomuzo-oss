# Development Guide

[← Back to README.md](../README.md)

## SPDX Data Update (`update-spdx` Subcommand)

Uzomuzo vendors the upstream SPDX License List (JSON) and generates a fast lookup table for license normalization. The `update-spdx` subcommand provides end-to-end automation:

1. Downloads the latest `licenses.json` from the official SPDX repository (fails on truncation if size < ~2KB)
2. Atomically writes to `third_party/spdx/licenses.json` (temp file + rename)
3. Runs `go generate ./internal/domain/licenses`:
   - Parses JSON
   - Merges custom aliases from `internal/domain/licenses/aliases.custom.yml` (if present)
   - Outputs diff-friendly generated file `internal/domain/licenses/spdx_generated.go`

### When to Run

- Periodically (e.g., monthly) to incorporate new SPDX identifiers
- Before adding new custom alias rules (to verify correspondence with upstream IDs)
- When security / compliance requirements demand the latest list

### How to Run

```bash
./uzomuzo update-spdx
# or
go run . update-spdx
```

### Verifying Updates

```bash
go test ./...  # Ensure the new table doesn't break normalization logic
```

Review the git diff of updated artifacts:

- `third_party/spdx/licenses.json`
- `internal/domain/licenses/spdx_generated.go`

Both files should be committed together. The generated file uses line-by-line output (minimizes merge conflicts, improves review readability). Never edit manually — regenerate instead.

### Custom Aliases

Add organization-specific shorthands to `internal/domain/licenses/aliases.custom.yml` (simple `key: CanonicalID` lines). Re-run `update-spdx` after editing to integrate into the alias table. Targets not present in the upstream SPDX list are ignored.

## Instruction Sync (`sync-instructions` Subcommand)

`.github/instructions/` is the **Single Source of Truth** for instruction / rules files. `.claude/rules/` contains auto-generated copies.

```bash
# Regenerate .claude/rules/ from .github/instructions/
make sync-instructions
```

### Automatic Sync

- **Claude Code**: Auto-runs after Edit/Write on files in `.github/instructions/` (hook in `.claude/settings.json`)
- **Git pre-commit**: Auto-syncs and stages when `.github/instructions/` files are staged (`.githooks/pre-commit`)

### Git Hooks Setup

After cloning the repository, run:

```bash
git config core.hooksPath .githooks
```

### Details

See `.claude/rules/instruction-sync.md` for file structure and editing protocol.

## Adding Agents / Skills / Rules

`.github/` is the Single Source of Truth. When adding, create the `.github/` side first, then create the corresponding file in `.claude/`.

### 1. Adding an Agent

**Step 1**: Create `.github/agents/<name>.agent.md` (main file)

**Step 2**: Create a shim (thin delegation file) at `.claude/agents/<name>.md`

**Step 3**: Add to the Available Agents table in `.github/instructions/agent-orchestration.instructions.md`

### 2. Adding a Skill (Prompt)

**Step 1**: Create `.github/prompts/<name>.prompt.md` (main file)

**Step 2**: Create a shim at `.claude/skills/<name>.md`

### 3. Adding a Rule (Instruction)

**Step 1**: Create `.github/instructions/<name>.instructions.md`

**Step 2**: Run `make sync-instructions` — `.claude/rules/<name>.md` is auto-generated

### Checklist

Verify after adding:

- [ ] File exists on the `.github/` side (SoT)
- [ ] Corresponding file exists on the `.claude/` side (shim or generated)
- [ ] `go test ./...` passes

## Testing / Quality

```bash
# Run all tests
go test ./...

# Format imports
goimports -w .

# Run linter
golangci-lint run
```

## Project Structure (DDD Layer Boundaries)

Strict DDD layer boundaries:

```text
├── internal/
│   ├── domain/          # Business logic only (no external dependencies)
│   ├── application/     # Use case orchestration
│   ├── infrastructure/  # External APIs / parallel processing / I/O
│   └── interfaces/      # CLI / input validation (no parallel logic)
├── pkg/                 # Public library (uzomuzo) + examples
└── testdata/            # Test fixtures
```

Key components:

- **PURL Parser**: Unified Package URL parsing
- **GitHub Client**: Repository metadata / commit analysis
- **DepsDev Client**: Package ecosystem integration
- **Scorecard Integration**: OpenSSF security metrics
- **Lifecycle Assessment System**: Automatic security classification

## Performance

- **Concurrent processing**: Optimized goroutine pools for API calls
- **Rate limiting**: Configurable intervals for API usage compliance
- **Batch optimization**: Efficient processing of large package lists
- **Memory management**: Stream processing for large datasets

## Troubleshooting

### Common Issues

**GitHub API Rate Limiting**:

```bash
export GITHUB_TIMEOUT=60
export GITHUB_MAX_CONCURRENCY=5
```

**Missing Package Data**:

- Verify PURL format conforms to the specification
- Verify the package exists in the target ecosystem
- Check deps.dev API connectivity

**Authentication Errors**:

```bash
curl -H "Authorization: token $GITHUB_TOKEN" https://api.github.com/user
```

### Debug Mode

Enable verbose logging:

```bash
export LOG_LEVEL=debug
./uzomuzo scan pkg:npm/express@4.18.2
```
