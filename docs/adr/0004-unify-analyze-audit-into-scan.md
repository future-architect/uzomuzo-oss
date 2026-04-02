# ADR-0004: Unify analyze/audit into a single scan subcommand

## Status

Accepted (2026-04-02)

## Context

uzomuzo had two separate subcommands for dependency lifecycle evaluation:

- **`analyze`**: Single or batch PURL/GitHub URL evaluation via positional args, `--file`, or stdin pipe. Output was detailed per-package analysis.
- **`audit`**: Bulk evaluation from CycloneDX SBOM or go.mod. Output was a verdict table (ok/caution/replace/review) with exit code 1 on `replace`.

This split created several problems:

1. **User confusion**: Users had to choose between `analyze` and `audit` based on input type, even though both evaluate the same lifecycle signals.
2. **Overlapping functionality**: Both commands shared the same analysis pipeline (`AnalysisService.ProcessBatchPURLs`), differing only in input parsing and output rendering.
3. **Inconsistent CI support**: Only `audit` supported exit code gating, so CI pipelines using PURL lists had to parse output manually.
4. **Documentation burden**: Two separate CLI reference sections, two sets of examples, two mental models.

## Decision

Merge both subcommands into a single **`scan`** subcommand that:

1. Accepts all input types: PURL, GitHub URL, CycloneDX SBOM (`--sbom`), go.mod, PURL list file (`--file`), and stdin pipe.
2. Auto-detects file format when using `--file` (go.mod vs CycloneDX JSON vs PURL list).
3. Provides four output formats (`--format detailed|table|json|csv`) with smart defaults (detailed for ≤3 inputs, table for bulk).
4. Adds `--fail-on <labels>` for configurable CI exit code gating by lifecycle label (not just verdict).

### New packages (DDD compliant)

- `internal/domain/scan` — `FailPolicy` value object (pure, no I/O)
- `internal/application/scan` — `ScanService` orchestrating analysis + verdict + fail policy
- `internal/interfaces/cli/scan.go` — CLI handler with input routing
- `internal/interfaces/cli/scan_render.go` — Output renderers
- `internal/infrastructure/depparser/detect.go` — File format detection (I/O)

### Removed packages

- `internal/application/audit` — Replaced by `internal/application/scan`
- `internal/interfaces/cli/audit.go` — Replaced by `internal/interfaces/cli/scan.go`

## Consequences

### Positive

- **Single entry point**: Users only need to learn `uzomuzo scan`.
- **Flexible CI gating**: `--fail-on eol-confirmed,stalled` is more granular than the old binary replace/no-replace exit code.
- **Smart defaults**: Output format auto-selects based on input count — no need to specify for common cases.
- **No backward compatibility burden**: The project has no external users yet, so no migration shim is needed.

### Negative

- **Breaking change**: Any documentation or scripts referencing `analyze` or `audit` must be updated. Mitigated by the fact that there are no external users.
- **Larger single file**: `scan.go` handles multiple input modes in one file (mitigated by extracting `detectFileParser` to infrastructure layer and `runScanPURLList` as a separate function).

### Neutral

- ADR-0003 (CycloneDX SBOM input design) remains valid — the SBOM input path is preserved in `scan --sbom`.
