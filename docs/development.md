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

## Documentation Output Examples (`update-doc-examples`)

README.md and docs/usage.md contain CLI output examples that must match actual binary output. A Go script automates keeping these in sync.

### When to Run

- After changing CLI output rendering (`boxdraw.go`, `scan_render.go`, table/JSON/CSV formatters)
- After changing lifecycle classification logic (labels, reason strings)
- After adding or removing output example blocks in documentation

### How to Run

```bash
make update-doc-examples    # build binary, run all commands, update doc blocks
git diff                    # review changes
git add -p && git commit    # commit updated output
```

### How It Works

Output blocks in Markdown are wrapped with HTML comment markers:

```markdown
<!-- begin:output:express-detailed -->
` `` text
... CLI output replaced automatically ...
` `` 
<!-- end:output:express-detailed -->
```

The script (`scripts/update-doc-examples/`) reads command definitions from an embedded `commands.json`, runs each command, and replaces the content between matching markers.

### CI Automation (PR)

The `doc-examples` CI job runs on every PR and does two things:

1. **Marker validation** — checks that every command in `commands.json` has matching `begin/end` markers (fast, no API calls)
2. **Auto-update** — runs `make update-doc-examples`, and if any output blocks changed, commits and pushes the update automatically

This means you don't need to run `make update-doc-examples` locally — CI will do it for you. The auto-commit uses `[skip ci]` to avoid triggering another CI run.

### Manual Trigger (GitHub UI)

To force-refresh all doc examples without creating a PR:

1. Go to **Actions** tab → **CI** workflow
2. Click **"Run workflow"** → select the target branch (e.g. `main`) → click **"Run workflow"**
3. The `doc-examples` job builds the binary, runs all 17 commands, and auto-commits any changes

This is useful for periodic refresh (star counts, dependent counts drift over time) or after merging output-affecting changes.

### Adding a New Output Block

1. Add the marker pair to the Markdown file:
   ```markdown
   <!-- begin:output:my-new-block -->
   ```text
   placeholder
   ```
   <!-- end:output:my-new-block -->
   ```
2. Add a command entry to `scripts/update-doc-examples/commands.json`
3. Run `make update-doc-examples` to populate the block (or let CI do it)

## Testing / Quality

```bash
# Run all tests
go test ./...

# Format imports
goimports -w .

# Run linter
golangci-lint run
```

## GitHub Actions Workflows

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `ci.yml` | push / PR | Build, test, lint, doc-examples check |
| `dependency-scan.yml` | Monthly cron / manual dispatch | Trivy SBOM generation + `uzomuzo scan` with issue creation |
| `release.yml` | Tag push | GoReleaser + cosign signing |
| `codeql.yml` | push / PR / weekly | CodeQL security analysis |
| `scorecard.yml` | Weekly | OpenSSF Scorecard |

The dependency scan workflow (`dependency-scan.yml`) uses three separate jobs with scoped permissions: `scan` (contents: read), `report` (issues: write), and `notify` (inherits the workflow-level `contents: read` permission). See [Integration Examples](/docs/integration-examples.md#github-actions-scheduled-scanning) for configuration details.

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
