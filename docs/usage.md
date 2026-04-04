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

<!-- begin:output:usage-express-detailed -->
```text
$ uzomuzo scan pkg:npm/express@4.18.2

── pkg:npm/express@4.18.2 ──────────────────────────────────
│ Fast, unopinionated, minimalist web framework for node.
│ ✅ Active: Actively maintained with recent releases
├─ Signals ─────────────────────────────────────────────────
│ Recent Stable Release: true
│ Last Human Commit: (unavailable)
│ Maintained Score: 10/10
├─ Health ──────────────────────────────────────────────────
│ 68892 stars
│ Used by: 2211 packages
│ Depends on: 31 direct, 39 transitive
│ Score: 8.4/10  Maintained: 10.0/10
├─ Releases ────────────────────────────────────────────────
│ Stable: 5.2.1 (2025-12-01)
│ Pre-release: 5.0.0-beta.3 (2024-03-25)
│ Requested: 4.18.2 (2022-10-08)
├─ Links ───────────────────────────────────────────────────
│ Homepage: https://expressjs.com
│ Repository: https://github.com/expressjs/express
│ Registry: https://www.npmjs.com/package/express
│ deps.dev: https://deps.dev/npm/express
└───────────────────────────────────────────────────────────
```
<!-- end:output:usage-express-detailed -->

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

When the SBOM includes a CycloneDX `dependencies` section (produced by most modern SBOM generators), uzomuzo classifies dependencies as **direct** or **transitive** based on the dependency graph. By default, only direct dependencies are shown. In OSS health assessment (unlike vulnerability scanning), transitive dependency issues are not directly actionable by the user — if a transitive dependency has a problem, the resolution path is to update or replace the direct dependency that pulls it in. Filtering also reduces API calls for faster results.

Use `--show-transitive` to include transitive dependencies in the output:

```bash
# Direct dependencies only (default)
trivy fs . --format cyclonedx | ./uzomuzo scan --sbom -

# Include transitive dependencies
trivy fs . --format cyclonedx | ./uzomuzo scan --sbom - --show-transitive
```

When transitive dependencies are included, output shows a `RELATION` column indicating `direct`, `transitive (via-parent)`, or `Unknown` (for SBOMs without a `dependencies` section). SBOMs without dependency graph information gracefully fall back to showing all components as `Unknown`.

### go.mod Input

```bash
./uzomuzo scan                     # auto-detect go.mod in cwd
./uzomuzo scan --file go.mod       # explicit path
```

<details>
<summary><strong>Example: go.mod (table output with RELATION column)</strong></summary>

<!-- begin:output:usage-gomod-table -->
```text
$ uzomuzo scan --file go.mod -f table

STATUS      PURL                                                        RELATION  LIFECYCLE
⚠️ caution  pkg:golang/github.com/gorilla/mux@v1.8.1                    direct    Stalled
🔴 replace   pkg:golang/github.com/dgrijalva/jwt-go@v3.2.0+incompatible  direct    EOL-Effective
✅ ok        pkg:golang/github.com/stretchr/testify@v1.9.0               direct    Active

── Summary ─────────────────────────────────────────────────
│ 3 dependencies | ✅ 1 ok | ⚠️ 1 caution | 🔴 1 replace | 🔍 0 review
└───────────────────────────────────────────────────────────
```
<!-- end:output:usage-gomod-table -->

go.mod input adds a `RELATION` column showing `direct` or `indirect` dependency relationship.

</details>

### GitHub Actions Workflow Input

Scan a GitHub Actions workflow YAML to evaluate the lifecycle health of referenced Actions:

```bash
./uzomuzo scan --file .github/workflows/ci.yml
```

This extracts `uses:` directives (e.g., `actions/checkout@v4`) and evaluates each referenced Action as a GitHub repository.

<details>
<summary><strong>Example: GitHub Actions workflow (detailed output)</strong></summary>

```text
$ uzomuzo scan --file .github/workflows/ci.yml -f detailed

── https://github.com/actions/checkout ─────────────────────
│ Description: Action for checking out a repo
│ ✅ Active
│ Reason: Recent human commits but no recent package publishing; maintenance score unavailable (Scorecard not found)
├─ Health ──────────────────────────────────────────────────
│ 7733 stars
│ Last Commit: 2026-01-09
├─ License ─────────────────────────────────────────────────
│ Project: MIT (github)
│ Requested Version: (none)
├─ Links ───────────────────────────────────────────────────
│ Homepage: https://github.com/features/actions
│ Repository: https://github.com/actions/checkout
└───────────────────────────────────────────────────────────

── https://github.com/golangci/golangci-lint-action ────────
│ Package: pkg:golang/github.com/golangci/golangci-lint-action@v1.2.2
│ Description: Official GitHub Action for golangci-lint from its authors
│ ✅ Active
│ Reason: Recent human commits (VCS-direct ecosystem; commits deliver updates to consumers); maintenance score ≥ 3
├─ Health ──────────────────────────────────────────────────
│ 1419 stars
│ Score: 6.9/10  Maintained: 10.0/10
│ Last Commit: 2026-04-01
├─ Releases ────────────────────────────────────────────────
│ Stable: v1.2.2 (2020-07-10)
│ Highest (SemVer): v1.2.3-0.20260105112450-f75c1c4ee8cf (2026-01-05)
├─ License ─────────────────────────────────────────────────
│ MIT (depsdev)
├─ Links ───────────────────────────────────────────────────
│ Homepage: https://github.com/marketplace/actions/golangci-lint
│ Repository: https://github.com/golangci/golangci-lint-action
│ Registry: https://pkg.go.dev/github.com%2Fgolangci%2Fgolangci-lint-action
│ deps.dev: https://deps.dev/go/github.com/golangci/golangci-lint-action
└───────────────────────────────────────────────────────────
```

</details>

### Actions Discovery (`--include-actions`)

When scanning GitHub URLs, `--include-actions` automatically fetches the target repository's workflow files and evaluates referenced Actions:

```bash
# Scan a repo AND its GitHub Actions dependencies
./uzomuzo scan https://github.com/owner/repo --include-actions

# Combined with fail policy
./uzomuzo scan https://github.com/owner/repo --include-actions --fail-on stalled
```

Output includes a `SOURCE` column to distinguish direct results from discovered Actions. JSON and CSV formats include a `source` field (`"actions"` for discovered entries).

> **Note:** `--include-actions` is opt-in because it makes additional GitHub API calls to fetch workflow files. It requires `GITHUB_TOKEN` to be set (the Contents API is used to fetch workflow YAML). It is only supported for GitHub URL inputs (not `--sbom` or `--file go.mod`).

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

<!-- begin:output:usage-request-table -->
```text
$ uzomuzo scan pkg:npm/request@2.88.2 pkg:npm/express@4.18.2 --format table
STATUS     PURL                    LIFECYCLE
🔴 replace  pkg:npm/request@2.88.2  EOL-Confirmed
✅ ok       pkg:npm/express@4.18.2  Active

── Summary ─────────────────────────────────────────────────
│ 2 dependencies | ✅ 1 ok | ⚠️ 0 caution | 🔴 1 replace | 🔍 0 review
└───────────────────────────────────────────────────────────
```
<!-- end:output:usage-request-table -->

</details>

<details>
<summary><strong>Example: <code>--format table</code> with SBOM input (RELATION column)</strong></summary>

When scanning an SBOM that includes dependency graph information, a `RELATION` column is added:

```text
$ trivy fs . --format cyclonedx | uzomuzo scan --sbom - --show-transitive --format table
STATUS      RELATION                  PURL                        LIFECYCLE
✅ ok        direct                    pkg:npm/express@4.18.2      Active
✅ ok        transitive (express)      pkg:npm/accepts@1.3.8       Active
🔴 replace   transitive (express)      pkg:npm/inflight@1.0.6      EOL-Confirmed

── Summary ─────────────────────────────────────────────────
│ 3 dependencies | ✅ 2 ok | ⚠️ 0 caution | 🔴 1 replace | 🔍 0 review
└───────────────────────────────────────────────────────────
```

Without `--show-transitive`, only `direct` entries are displayed — transitive issues are addressed by updating the direct dependency that introduces them.

</details>

<details>
<summary><strong>Example: <code>--format json</code></strong></summary>

<!-- begin:output:usage-request-json -->
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
      "lifecycle": "EOL-Confirmed",
      "repo_url": "https://github.com/request/request",
      "overall_score": 3.6,
      "dependent_count": 186349,
      "stable_version": "2.88.2",
      "project_license": "Apache-2.0",
      "version_licenses": [
        "Apache-2.0"
      ],
      "advisory_count": 1,
      "max_advisory_severity": "MEDIUM",
      "max_cvss3_score": 6.1,
      "reason": "Stable version is deprecated in npm registry. Message: request has been deprecated, see https://github.com/request/request/issues/3142 UI: https://www.npmjs.com/package/request/v/2.88.2"
    }
  ]
}
```
<!-- end:output:usage-request-json -->

The JSON format includes all analysis fields (verdict, lifecycle, EOL evidence, scores, license info), making it suitable for CI pipelines and downstream tooling without needing a separate detailed run.

</details>

<details>
<summary><strong>Example: <code>--format csv</code></strong></summary>

<!-- begin:output:usage-request-csv -->
```text
$ uzomuzo scan pkg:npm/request@2.88.2 --format csv
verdict,purl,lifecycle,successor,advisory_count,max_advisory_severity,max_cvss3_score,repo_url,source,via
replace,pkg:npm/request@2.88.2,EOL-Confirmed,,1,MEDIUM,6.1,https://github.com/request/request,,
```
<!-- end:output:usage-request-csv -->

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

<!-- begin:output:usage-failon-match -->
```text
$ uzomuzo scan pkg:npm/request@2.88.2 --fail-on eol-confirmed --format table
STATUS     PURL                    LIFECYCLE
🔴 replace  pkg:npm/request@2.88.2  EOL-Confirmed

── Summary ─────────────────────────────────────────────────
│ 1 dependencies | ✅ 0 ok | ⚠️ 0 caution | 🔴 1 replace | 🔍 0 review
└───────────────────────────────────────────────────────────
# exit code: 1  (request is EOL-Confirmed → matches --fail-on eol-confirmed)
```
<!-- end:output:usage-failon-match -->

**Exit 0 — label does not match:**

<!-- begin:output:usage-failon-nomatch -->
```text
$ uzomuzo scan pkg:npm/request@2.88.2 --fail-on eol-effective --format table
STATUS     PURL                    LIFECYCLE
🔴 replace  pkg:npm/request@2.88.2  EOL-Confirmed

── Summary ─────────────────────────────────────────────────
│ 1 dependencies | ✅ 0 ok | ⚠️ 0 caution | 🔴 1 replace | 🔍 0 review
└───────────────────────────────────────────────────────────
# exit code: 0  (request is EOL-Confirmed, not EOL-Effective → no match)
```
<!-- end:output:usage-failon-nomatch -->

**Multiple labels — exit 1 if any label matches any dependency:**

<!-- begin:output:usage-failon-multi -->
```text
$ uzomuzo scan pkg:npm/request@2.88.2 pkg:npm/express@4.18.2 --fail-on eol-confirmed,stalled --format table
STATUS     PURL                    LIFECYCLE
🔴 replace  pkg:npm/request@2.88.2  EOL-Confirmed
✅ ok       pkg:npm/express@4.18.2  Active

── Summary ─────────────────────────────────────────────────
│ 2 dependencies | ✅ 1 ok | ⚠️ 0 caution | 🔴 1 replace | 🔍 0 review
└───────────────────────────────────────────────────────────
# exit code: 1  (request matches eol-confirmed)
```
<!-- end:output:usage-failon-multi -->

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

## Analysis Precision and `GITHUB_TOKEN`

uzomuzo combines data from **deps.dev** (package registry) and **GitHub API** (repository state). When `GITHUB_TOKEN` is not set, GitHub API calls are skipped and analysis relies on deps.dev data only, which significantly reduces lifecycle assessment precision.

### What each data source provides

| Data Source | Available Without Token | Requires `GITHUB_TOKEN` |
|-------------|------------------------|------------------------|
| **deps.dev** | Package versions & publish dates, Scorecard metrics, Advisory/CVE counts, Dependent counts, License info | — |
| **GitHub API** | — | Last human commit date, Bot vs. human commit ratio, Archive/disabled status, Fork detection |

### How missing data affects lifecycle classification

| Actual State | With Token | Without Token | Risk |
|--------------|-----------|---------------|------|
| Archived repository | **EOL-Confirmed** | Stalled | False negative — clear EOL signal missed |
| Unpatched CVEs + no commits for 2+ years | **EOL-Effective** | Stalled | False negative — supply chain risk missed |
| Active Go/Composer package (commits but no registry publish) | **Active** | Stalled | False positive — healthy package flagged |
| Frozen utility with zero advisories | **Legacy-Safe** | Stalled | False positive — safe package flagged |
| Bot-only maintenance (Dependabot/Renovate) | **Stalled** | Active | False negative — automation masquerades as maintenance |

Without `GITHUB_TOKEN`, many packages fall into **Review Needed** instead of actionable categories because the assessor lacks commit-based signals to make a confident determination.

### Recommendation

For production CI gates and security audits, always set `GITHUB_TOKEN`. The token requires no special scopes for public repositories — a default `GITHUB_TOKEN` from GitHub Actions or `gh auth login` is sufficient.

```bash
# GitHub Actions — automatic
# GITHUB_TOKEN is available by default in workflows

# Local — via GitHub CLI
gh auth login
export GITHUB_TOKEN=$(gh auth token)
./uzomuzo scan --sbom bom.json --fail-on eol-confirmed
```

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

For automated monthly scanning with GitHub Actions, see [Integration Examples: GitHub Actions Scheduled Scanning](/docs/integration-examples.md#github-actions-scheduled-scanning).

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
