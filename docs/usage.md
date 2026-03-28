# Usage

[← Back to README.md](../README.md)

## Direct Package Analysis

```bash
# NPM package
./uzomuzo scan pkg:npm/lodash@4.17.21

# Python package
./uzomuzo scan pkg:pypi/requests@2.28.1

# Maven package
./uzomuzo scan pkg:maven/org.springframework/spring-core@5.3.8

# GitHub repository
./uzomuzo scan github.com/microsoft/typescript

# Multiple inputs (PURL and GitHub URL can be mixed)
./uzomuzo scan pkg:npm/express@4.18.2 pkg:pypi/requests@2.28.1
./uzomuzo scan https://github.com/expressjs/express github.com/psf/requests
```

## Batch Processing

List one identifier per line (PURL or GitHub URL) in a text file:

```text
pkg:npm/express@4.18.2
pkg:pypi/django@4.2.0
https://github.com/golang/go
pkg:cargo/serde@1.0.136
```

Run:

```bash
./uzomuzo scan --file input_file.txt --sample 500
```

File mode is designed for large inputs (thousands of lines). The file path is specified via the `--file` flag and `--sample N` enables random sampling (0 = all).

### Line Range (`--line-range`)

Process only a contiguous subset of a large input file:

```bash
# Lines 1-250 (inclusive)
./uzomuzo scan --file input_file.txt --line-range=1:250

# Line 500 to end of file
./uzomuzo scan --file input_file.txt --line-range=500:

# Line range + random sampling (sampling applied after line range filter)
./uzomuzo scan --file input_file.txt --line-range=1001:2000 --sample=1000
```

Rules:

- Format: `START:END` (1-based, inclusive). Omit END to read to EOF
- START must be >= 1. When END is specified, it must be >= START
- Requires `--file`; ignored in direct input mode (specifying outside file mode causes an error)
- Counts physical line numbers (blank lines and `#` comments consume line numbers but are skipped during processing)

### Pipe / stdin Input

uzomuzo reads from stdin when piped. This enables integration with SBOM tools:

```bash
trivy image --format cyclonedx IMAGE | jq -r '.components[].purl' | ./uzomuzo scan --only-eol
```

All flags (`--only-eol`, `--ecosystem`, `--export-license-csv`) work with pipe input. See [Integration Examples](integration-examples.md) for detailed workflows.

## Filters & Output Control

Input filtering and output control options:

- `--ecosystem <name>`: Limit to a single ecosystem (e.g., `npm`, `pypi`, `maven`, `nuget`, `cargo`, `golang`, `gem`, `composer`). In file mode, filter is applied before sampling
- `--only-eol`: Show only items with confirmed EOL status
- `--only-review-needed`: Show only items with "Review Needed" status (includes unevaluated)
- Combining `--only-eol` and `--only-review-needed` shows both categories
- `--export-license-csv <path>`: Export extended license analysis CSV (project vs version licenses, SPDX statistics, fallback / derived / override indicators). Output only when specified (opt-in). Column definitions: `internal/infrastructure/export/csv/license.go`

```bash
# Direct input: npm only & EOL only
./uzomuzo scan --ecosystem npm --only-eol pkg:npm/express@4.18.2 pkg:npm/lodash@4.17.21

# File mode: sample 200, Maven only, Review Needed only
./uzomuzo scan --file input_file.txt --ecosystem maven --only-review-needed --sample 200
```

### Built-in Flags

| Flag | Description |
|------|-------------|
| `--help`, `-h` | Show help for any command |
| `--version`, `-v` | Print version information |

These flags are auto-generated. Use `uzomuzo --help` for the full list, or `uzomuzo scan --help` / `uzomuzo audit --help` for subcommand-specific help.

## Subcommands

| Subcommand | Description |
|------------|-------------|
| `scan` | Analyze packages by PURL, GitHub URL, file, or stdin pipe |
| `audit` | Bulk dependency health evaluation from CycloneDX SBOM or go.mod |
| `update-spdx` | Update and regenerate the embedded SPDX license list from upstream |

> **Deprecation notice:** Running `uzomuzo <PURL>` without the `scan` subcommand still works for backward compatibility but prints a deprecation warning. This legacy invocation will be removed in a future release. Use `uzomuzo scan <PURL>` instead.

### `audit` — Dependency Health Audit

Evaluates all project dependencies in bulk and derives a per-dependency verdict: **ok**, **caution**, **replace**, or **review**. Designed for CI pipelines — exits with code 1 when any dependency receives a `replace` verdict.

```bash
# CycloneDX SBOM input (recommended)
./uzomuzo audit --sbom bom.json
trivy fs . --format cyclonedx | ./uzomuzo audit --sbom -
trivy image my-app:latest --format cyclonedx | ./uzomuzo audit --sbom -
syft . -o cyclonedx-json | ./uzomuzo audit --sbom -

# go.mod fallback
./uzomuzo audit                    # auto-detect go.mod in cwd
./uzomuzo audit --file go.mod

# Output formats
./uzomuzo audit --format table     # default: human-readable table
./uzomuzo audit -f json            # short flag alias
./uzomuzo audit --format csv       # CSV for spreadsheet/pipeline processing
```

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--sbom <path>` | Path to CycloneDX SBOM JSON file (use `-` for stdin) | — |
| `--file <path>` | Path to go.mod file | — |
| `--format <fmt>`, `-f` | Output format: `table`, `json`, `csv` | `table` |

**Input resolution order:**

1. `--sbom` flag (CycloneDX JSON)
2. `--file` flag (go.mod)
3. Auto-detect `go.mod` in current working directory

**Verdict mapping:**

| Verdict | Lifecycle states | Action |
|---------|-----------------|--------|
| `ok` | Active, Legacy-Safe | No action needed |
| `caution` | Stalled, EOL-Scheduled | Monitor; plan migration |
| `replace` | EOL-Confirmed, EOL-Effective, Archived | Migrate immediately |
| `review` | Insufficient data, analysis error | Manual investigation |

**Difference from pipe mode:** The `audit` subcommand accepts structured SBOM files directly (no `jq` extraction needed), provides a summarized verdict view optimized for quick scanning, and exits with code 1 for CI gating. Use the existing pipe mode (`trivy ... | jq -r '.components[].purl' | ./uzomuzo scan`) when you need detailed per-package analysis.

## License CSV Column Reference (`--export-license-csv`)

1 row = 1 PURL (`Analysis`). Version-side licenses are aggregated with **semicolon separators** without adding rows.

| Column | Type | Source / Mapping | Purpose |
|--------|------|------------------|---------|
| original_purl | string | `Analysis.OriginalPURL` | Exact user input. Key for matching in reproduction tests / audit logs. |
| effective_purl | string | `Analysis.EffectivePURL` | Final identifier after resolution. Used for re-fetching and cache keys. |
| version_resolved | bool(string) | `IsVersionResolved()` | true = version identified. false rows are "latest dependency" with change risk. |
| project_license_identifier | string | `ProjectLicense.Identifier` | Confirmed project SPDX / normalized identifier. Empty if undetermined. |
| project_license_raw | string | `ProjectLicense.Raw` | Upstream raw string. Primary source for non-SPDX / notation variation investigation. |
| project_license_source | string | `ProjectLicense.Source` | License acquisition path. For trust level / improvement priority analysis by source. |
| project_license_is_spdx | bool | `ProjectLicense.IsSPDX` | true = official SPDX. false with raw present = normalization improvement candidate. |
| project_license_is_zero | bool | `ProjectLicense.IsZero()` | true = project license absent. Starting point for fallback/promotion decisions. |
| version_license_identifiers | string(list) | Version slice.Identifier | Version-side SPDX/normalized identifiers (`;` separated). Multi-license overview. |
| version_license_raws | string(list) | Version slice.Raw | Version-side raw strings (`;`). For checking composite expressions and notation variations. |
| version_license_sources | string(list) | Version slice.Source | Source list (`;`). For SPDX / non-SPDX ratio analysis. |
| version_license_count | int | len(slice) | Version-side license count. Dual/multi-license detection. |
| version_licenses_all_non_spdx | bool | derived | true = all non-SPDX. Priority extraction target for normalization. |
| version_licenses_any_composite_expr | bool | AND/OR/() detection | true = contains composite conditions (AND/OR). Raises legal review priority. |
| project_vs_version_mismatch | bool | derived | true = Project SPDX not found in Version set, indicating divergence. Audit target for config/evolution. |
| licenses_all_missing_or_nonstandard | bool | derived | true = no confirmed SPDX. Coverage KPI denominator & improvement tracking. |
| fallback_applied | bool | version source==`project-fallback` | true = Project SPDX copied to Version. Indicates original data gap. |
| derived_from_version | bool | project source==`derived-from-version` | true = single Version SPDX promoted. Suggests missing Project metadata. |
| github_override_applied | bool | GitHub override sources | true = GitHub info used preferentially. Override logic triggered. |
| license_resolution_scenario | string | classifier | License state tag. Filter/pivot starting point. Unknown values can be safely ignored. |
| error | string | `Analysis.Error` | Analysis failure reason. Non-empty rows should not trust other columns; re-run candidates. |
| registry_url | string | `PackageLinks.RegistryURL` | Official registry page. Shortcut to original source. |
| repository_url | string | `RepoURL` | Source repository. Entry point for LICENSE file / update re-fetch. |

Legend:

- `string(list)` uses `;` separator. No in-element `;` expected (escape spec will be added if needed)
- bool columns output as string `true` / `false`. Convert to boolean on the consumer side
- "derived" flags (fallback_applied / derived_from_version) indicate algorithm intervention
- `license_resolution_scenario` may have new labels added in the future. Ignore unknown values or bucket as "other"

## Configuration

Environment variable-centric (12-factor). Unset / 0 falls back to safe defaults. See `config.template.env` for the complete list with comments.

### Key Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LOG_LEVEL` | Log level (`debug`/`info`/`warn`/`error`) | `info` |
| `APP_TIMEOUT_SECONDS` | Overall timeout (seconds) | `14400` |
| `APP_SAMPLE_SIZE` | Batch input sampling size (0 = disabled) | `0` |
| `APP_OUTPUT_FORMAT` | Output format (`csv`) | `csv` |
| `APP_MAX_PURLS` | Maximum PURLs per run (safety guard) | `5000` |

### GitHub Client

| Variable | Description | Default |
|----------|-------------|---------|
| `GITHUB_BASE_URL` | API base (for GitHub Enterprise) | `https://api.github.com` |
| `GITHUB_TIMEOUT` | Per-request timeout (Go duration, e.g., `45s`) | `45s` |
| `GITHUB_MAX_RETRIES` | Transient failure retry count | `3` |
| `GITHUB_MAX_CONCURRENCY` | GitHub concurrent request limit | `20` |

### deps.dev Client

| Variable | Description | Default |
|----------|-------------|---------|
| `DEPSDEV_BASE_URL` | API base URL | `https://api.deps.dev` |
| `DEPSDEV_TIMEOUT` | Request timeout (duration) | `10s` |
| `DEPSDEV_MAX_RETRIES` | Retry count | `3` |
| `DEPSDEV_BATCH_SIZE` | Max identifiers per batch request | `1000` |
| `DEPSDEV_REQUEST_INTERVAL_MS` | Sleep between batches (ms) | `500` |

### Lifecycle Assessment Settings

Configure thresholds via `LIFECYCLE_ASSESS_*` environment variables.

#### Minimal .env Override Example

```env
LIFECYCLE_ASSESS_MAX_HUMAN_COMMIT_GAP_DAYS=150
LIFECYCLE_ASSESS_RESIDUAL_ADVISORY_THRESHOLD=1
LIFECYCLE_ASSESS_EOL_INACTIVITY_DAYS=730
```

### Runtime Overrides

Normalization is applied after loading, so you only need to set the fields you want to change. All others inherit defaults:

```bash
export LIFECYCLE_ASSESS_RESIDUAL_ADVISORY_THRESHOLD=2
export LIFECYCLE_ASSESS_MAX_HUMAN_COMMIT_GAP_DAYS=120
```

Operational tuning without code changes (low risk, reversible).

## Logging

Uzomuzo outputs structured logs via Go's `slog`. Designed for batch/cron operation and incident investigation.

### Log Environment Variables

| Variable | Values | Purpose |
|----------|--------|---------|
| `LOG_FORMAT` | `json` or `text` | Structured JSON (recommended for production) / human-readable text |
| `LOG_LEVEL` | `debug`, `info`, `warn`, `error` | Verbosity; default `info` |
| `RUN_ID` | arbitrary string | Correlate all logs for a single run; auto-generated if empty |

- Normal batch: `LOG_FORMAT=json` + `LOG_LEVEL=info`
- Set `RUN_ID` uniquely per run (timestamp or job ID) for cross-system log correlation

Info-level summary is output at the end of each batch:

- `BatchSummary`: `total`, `missing_repo`, `missing_repo_pct`, `missing_project`, `missing_project_pct`, `missing_scorecard`, `missing_scorecard_pct`
- Automatic warn escalation:
  - `HighMissingProjectRatio`: `missing_project_pct >= 0.20` and `total >= 10`
  - `HighMissingScorecardRatio`: `missing_scorecard_pct >= 0.30` and `total >= 10`

### Log Field Quick Reference (JSON format)

- Correlation: `run_id`, `app`, `ts`, `level`, `msg`
- Batch metrics: `BatchSummary` field group
- deps.dev client signals: `PURLsWithoutRepoURL`, `PackageInfoBatchEmpty`, `ProjectBatch*`
- Errors: under `error` key

Pipe JSON logs to a log aggregation platform (ELK / OpenSearch / Cloud Logging) and correlate by `run_id`.

## Output Format

CSV output comprehensively covers security metrics:

- **Basic info**: PURL, repository URL, package metadata
- **Scorecard metrics**: All OpenSSF Scorecard check results
- **Security assessment**: Vulnerability scores, maintenance status
- **Lifecycle assessment**: Automatic classification (Active / Stalled / Legacy-Safe / EOL)
- **Repository status**: Stars, forks, archive status, last commit

### CLI Display Order

Lifecycle label order in CLI detailed view (non-CSV):

1. Active
2. Stalled
3. EOL
4. Review Needed

When `--only-eol` / `--only-review-needed` is specified, only the matching category is shown (both flags together show both).
