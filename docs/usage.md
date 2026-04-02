# Usage

[← Back to README.md](../README.md)

## The `scan` Subcommand

`scan` is the unified entry point for all dependency lifecycle analysis. It accepts PURLs, GitHub URLs, CycloneDX SBOMs, go.mod files, and stdin pipes.

### Direct Package Analysis

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

<details>
<summary><strong>Example: Single PURL (detailed output)</strong></summary>

```text
$ uzomuzo scan pkg:npm/express@4.18.2

--- PURL 1 ---
📦 Package: pkg:npm/express@4.18.2
🧾 Description: Fast, unopinionated, minimalist web framework for node.
   🔗 Homepage: https://expressjs.com
   🗃 Registry: https://www.npmjs.com/package/express
⚖️  Result: 🟢 Active
💭 Reason: Recent stable package version published; maintenance score ≥ 3
📊 GitHub Info: Normal (⭐ 68886 stars)
👥 Used by: 2210 packages
📦 Depends on: 31 direct, 39 transitive
🏆 Overall Score: 8.4/10
  🔧 Maintained: 10.0/10
📦 Latest Stable Release: 5.2.1 (2025-12-01)
   ↳ Stable Advisories: 0
📦 Highest Version (SemVer): 5.2.1 (2025-12-01)
📋 Requested Version: 4.18.2 (2022-10-08)
📄 License: MIT (source: depsdev-project-spdx / depsdev-version-spdx)
🔗 Repository: https://github.com/expressjs/express
🔗 Scorecard: https://scorecard.dev/viewer/?uri=github.com%2Fexpressjs%2Fexpress
```

For ≤3 inputs, the `detailed` format is used automatically. Use `--format detailed` to force it for larger inputs.

</details>

### SBOM Input

```bash
# CycloneDX SBOM file
./uzomuzo scan --sbom bom.json

# Pipe from SBOM tools
trivy fs . --format cyclonedx | ./uzomuzo scan --sbom -
trivy image my-app:latest --format cyclonedx | ./uzomuzo scan --sbom -
syft . -o cyclonedx-json | ./uzomuzo scan --sbom -
```

### go.mod Input

```bash
./uzomuzo scan                     # auto-detect go.mod in cwd
./uzomuzo scan --file go.mod       # explicit path
```

### File Input (PURL/URL list)

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

File mode is designed for large inputs (thousands of lines). `--sample N` (N > 0) enables random sampling. If `--sample` is omitted, the configured default sample size (`APP_SAMPLE_SIZE`) is applied when set; otherwise all entries are processed.

### Pipe / stdin Input

uzomuzo reads from stdin when piped:

```bash
echo "pkg:npm/express@4.18.2" | ./uzomuzo scan
```

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
- Requires `--file`; specifying `--line-range` without `--file` results in an error
- Counts physical line numbers (blank lines and `#` comments consume line numbers but are skipped during processing)

## Input Resolution Order

1. `--sbom` flag (CycloneDX JSON, or `-` for stdin)
2. `--file` flag (go.mod, CycloneDX JSON, or PURL/URL list — auto-detected)
3. Positional args (PURLs or GitHub URLs)
4. Stdin pipe
5. Auto-detect `go.mod` in current working directory

## Output Formats

```bash
./uzomuzo scan pkg:npm/express@4.18.2 --format detailed   # rich per-package output
./uzomuzo scan --sbom bom.json --format table              # verdict summary table
./uzomuzo scan --sbom bom.json --format json               # enriched JSON with full analysis
./uzomuzo scan --sbom bom.json --format csv                # CSV for spreadsheet/pipeline
```

**Smart default:** `detailed` for ≤3 inputs, `table` for bulk.

<details>
<summary><strong>Example: <code>--format table</code></strong></summary>

```text
$ uzomuzo scan pkg:npm/request@2.88.2 pkg:npm/express@4.18.2 --format table
VERDICT  PURL                    LIFECYCLE  EOL
replace  pkg:npm/request@2.88.2  EOL        EOL
ok       pkg:npm/express@4.18.2  Active     Not EOL

Summary: 2 dependencies | 1 ok | 0 caution | 1 replace | 0 review
```

</details>

<details>
<summary><strong>Example: <code>--format json</code></strong></summary>

```json
{
  "summary": {
    "total": 1,
    "ok": 0,
    "caution": 0,
    "replace": 1,
    "review": 0
  },
  "packages": [
    {
      "purl": "pkg:npm/request@2.88.2",
      "verdict": "replace",
      "lifecycle": "EOL",
      "eol": "EOL",
      "eol_reason": "Stable version is deprecated in npm registry. ...",
      "repo_url": "https://github.com/request/request",
      "overall_score": 3.6,
      "dependent_count": 186345,
      "stable_version": "2.88.2",
      "project_license": "Apache-2.0",
      "version_licenses": ["Apache-2.0"],
      "reason": "Primary-source EOL"
    }
  ]
}
```

The JSON format includes all analysis fields (verdict, lifecycle, EOL evidence, scores, license info), making it suitable for CI pipelines and downstream tooling without needing a separate detailed run.

</details>

<details>
<summary><strong>Example: <code>--format csv</code></strong></summary>

```text
$ uzomuzo scan pkg:npm/request@2.88.2 --format csv
verdict,purl,lifecycle,eol,eol_reason,successor,repo_url
replace,pkg:npm/request@2.88.2,EOL,EOL,"Stable version is deprecated in npm registry. ...",https://github.com/request/request
```

</details>

## CI Gating with `--fail-on`

Exit with code 1 when any dependency matches the specified lifecycle labels:

```bash
# Fail on confirmed EOL packages
./uzomuzo scan --sbom bom.json --fail-on eol-confirmed

# Fail on multiple lifecycle states
./uzomuzo scan --sbom bom.json --fail-on eol-confirmed,eol-effective,stalled
```

Valid labels: `eol-confirmed`, `eol-effective`, `eol-scheduled`, `stalled`, `legacy-safe`

Without `--fail-on`, exit code is always 0 regardless of scan results.

<details>
<summary><strong>Example: <code>--fail-on</code> exit code behavior</strong></summary>

**Exit 1 — label matches a dependency:**

```text
$ uzomuzo scan pkg:npm/request@2.88.2 --fail-on eol-confirmed --format table
VERDICT  PURL                    LIFECYCLE  EOL
replace  pkg:npm/request@2.88.2  EOL        EOL

Summary: 1 dependencies | 0 ok | 0 caution | 1 replace | 0 review
# exit code: 1  (request is EOL-Confirmed → matches --fail-on eol-confirmed)
```

**Exit 0 — label does not match:**

```text
$ uzomuzo scan pkg:npm/request@2.88.2 --fail-on eol-effective --format table
VERDICT  PURL                    LIFECYCLE  EOL
replace  pkg:npm/request@2.88.2  EOL        EOL

Summary: 1 dependencies | 0 ok | 0 caution | 1 replace | 0 review
# exit code: 0  (request is EOL-Confirmed, not EOL-Effective → no match)
```

**Multiple labels — exit 1 if any label matches any dependency:**

```text
$ uzomuzo scan pkg:npm/request@2.88.2 pkg:npm/express@4.18.2 --fail-on eol-confirmed,stalled --format table
VERDICT  PURL                    LIFECYCLE  EOL
replace  pkg:npm/request@2.88.2  EOL        EOL
ok       pkg:npm/express@4.18.2  Active     Not EOL

Summary: 2 dependencies | 1 ok | 0 caution | 1 replace | 0 review
# exit code: 1  (request matches eol-confirmed)
```

`--fail-on` works with all output formats (`table`, `json`, `csv`). Output is produced normally before the exit code is set.

</details>

## Verdict Mapping

| Verdict | Lifecycle states | Action |
|---------|-----------------|--------|
| `ok` | Active, Legacy-Safe | No action needed |
| `caution` | Stalled, EOL-Scheduled | Monitor; plan migration |
| `replace` | EOL-Confirmed, EOL-Effective, Archived | Migrate immediately |
| `review` | Insufficient data, analysis error | Manual investigation |

### Built-in Flags

| Flag | Description |
|------|-------------|
| `--help`, `-h` | Show help for any command |
| `--version`, `-v` | Print version information |

Use `uzomuzo --help` for the full list, or `uzomuzo scan --help` for scan-specific help.

## Subcommands

| Subcommand | Description |
|------------|-------------|
| `scan` | Scan dependencies for lifecycle health (PURL, GitHub URL, SBOM, go.mod, file, pipe) |
| `update-spdx` | Update and regenerate the embedded SPDX license list from upstream |

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
| project_vs_version_mismatch | bool | derived | true = Project SPDX not found in Version set, indicating divergence. |
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
