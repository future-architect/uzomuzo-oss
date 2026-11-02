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

--- Summary Table ---
STATUS     PURL                    LIFECYCLE  BUILD
✅ ok       pkg:npm/express@4.18.2  Active     Hardened 9.3

── Summary ─────────────────────────────────────────────────
│ 1 dependencies | ✅ 1 ok | ⚠️ 0 caution | 🔴 0 replace | 🔍 0 review
└───────────────────────────────────────────────────────────

--- Detailed Report ---

--- PURL 1 ---
── pkg:npm/express@4.18.2 ──────────────────────────────────
│ Fast, unopinionated, minimalist web framework for node.
│ ✅ Active: Actively maintained with recent releases
├─ Signals ─────────────────────────────────────────────────
│ Recent Stable Release: true
│ Last Human Commit: 2026-03-31
│ Maintained Score: 10/10
├─ Health ──────────────────────────────────────────────────
│ 68888 stars
│ Dependents: 861 direct, 2185 total (deps.dev, packages resolving to 4.18.2)
│ Depends on: 31 direct, 39 transitive
│ Scorecard Overall: 8.4/10  Maintained: 10.0/10
│ Last Commit: 2026-03-31
├─ Releases ────────────────────────────────────────────────
│ Stable: 5.2.1 (2025-12-01)
│ Pre-release: 5.0.0-beta.3 (2024-03-25)
│ Requested: 4.18.2 (2022-10-08)
├─ Build Integrity ─────────────────────────────────────────
│ ✅ Hardened 9.3/10 (5/6)
│ Dangerous Workflow  10  Branch Protection    —
│ Code Review          9  Token Permissions   10
│ Binary Artifacts    10  Pinned Deps          6
│ → https://scorecard.dev/viewer/?uri=github.com%2Fexpressjs%2Fexpress
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

When transitive dependencies are included, output shows a `RELATION` column indicating `direct` or `transitive (via-parent)`. SBOMs without a `dependencies` section still list every component, but without relation information: the table has no `RELATION` column and JSON omits the field. For go.mod input, `// indirect` requirements appear as `transitive` without a parent.

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

STATUS      PURL                                                        RELATION  LIFECYCLE      BUILD
🔴 replace   pkg:golang/github.com/dgrijalva/jwt-go@v3.2.0+incompatible  direct    Stalled        —
⚠️ caution  pkg:golang/github.com/gorilla/mux@v1.8.1                    direct    Stalled        Moderate 6.5
✅ ok        pkg:golang/github.com/stretchr/testify@v1.9.0               direct    Active         Moderate 6.7

── Summary ─────────────────────────────────────────────────
│ 3 dependencies | ✅ 1 ok | ⚠️ 1 caution | 🔴 1 replace | 🔍 0 review
└───────────────────────────────────────────────────────────
```
<!-- end:output:usage-gomod-table -->

go.mod input adds a `RELATION` column. Requirements marked `// indirect` are scanned only with `--show-transitive` and appear as `transitive`, with no parent shown, because go.mod does not record which direct dependency pulls them in.

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

### Maintainer E-mail Domains (`--maintainer-domain`)

For versioned npm inputs, `--maintainer-domain` looks up the domain of each current maintainer's e-mail address through RDAP (shared mail providers, private suffixes, invalid addresses, addresses not found in the release history, and TLDs without an https RDAP service are listed as unchecked instead) and adds a notice when the domain is lapsing, unregistered, or was registered again after the address first appeared in the package's npm history:

```bash
./uzomuzo scan --maintainer-domain --format json pkg:npm/express@4.18.2
```

The result appears as `maintainer_domain` in `--format json` and as a `⚠ Maintainer domain:` line in the detailed view; table and CSV output are unchanged. The flag is off by default, makes RDAP requests (lookups at least one second apart per RDAP server), and does not change verdicts, exit codes, or `--fail-on`. It also fetches IANA's list of RDAP servers (<https://data.iana.org/rdap/dns.json>), keeps it under the user cache directory until it expires, and falls back to the copy built into the binary when the fetch fails; `rdap_bootstrap` in the JSON output says which list was used.

Each flagged finding also carries `publisher_email_evidence`: the account's latest visible publish (`versions[v]._npmUser`) among the npm packuments analysed in the same batch (PURL inputs and GitHub URL inputs are separate batches), with its `status` (`observed`, `email_missing`, `conflicting`, `not_observed`), `domain_relation` to the flagged domain (`same`, `different`, `unknown`), `registration_relation` to the re-registration date (`before`, `equal`, `after`, `unknown`), and `newer_publish_unknown`. That flag is true when a version newer than the latest visible publish has no visible publisher (its manifest was removed, or it names no `_npmUser`), in the flagged package or in another package of the batch that the account published or maintains, or, when no visible publish was found, when any version of the flagged package has none. `latest_observed` names the latest visible publish; it is absent for `not_observed` and `conflicting`, and has no `email_domain` for `email_missing`. `source` is always `versions._npmUser`, `scope` is `scan_packuments`, and `account_history_complete` is always false because the scan does not list every package of the account. Only the e-mail domain is reported. When the status is `observed`, the notice adds one sentence: that a newer publish exists whose publisher is not visible, or else whether the latest publish used the same domain or another one. See [ADR-0030](adr/0030-maintainer-email-domain-marker.md).

Each flagged finding also has `package_publish`: `found` includes `last_published_at` and `version` for the account's latest dated publish found in this package's npm records (when several versions share that time, `version` is the lowest by string order); `not_found` means those records show no dated, attributable publish by the account. It does not mean the account never published the package. The notice adds the corresponding package sentence without printing an e-mail address. A finding without an account name has no `package_publish` and no package sentence. The package-level `version_publisher` is `trusted_publishing` when the analysed version's `_npmUser.name` is exactly `GitHub Actions`, `other` for any other named publisher, or `unknown` without a named publisher record. The trusted case adds a notice sentence. `other` does not imply a manual publish. `version_publisher` is present when the flag produces a result even if `flagged` is empty; a package without a flagged finding has no maintainer-domain notice.

### Actions Discovery (`--include-actions`)

When scanning GitHub URLs, `--include-actions` automatically fetches the target repository's workflow files and evaluates referenced Actions:

```bash
# Scan a repo AND its GitHub Actions dependencies
./uzomuzo scan https://github.com/owner/repo --include-actions

# Fail when a referenced Action's repository is archived
./uzomuzo scan https://github.com/owner/repo --include-actions --fail-on archived
```

An Action has no registry, so no end-of-life is ever declared for it and
`--fail-on eol-confirmed` does not fire. `--fail-on archived` fails on archived
Actions only; `--fail-on stalled` also fails on Actions that have merely gone quiet.

Output includes a `SOURCE` column to distinguish direct results from discovered Actions. JSON and CSV formats include a `source` field (`"actions"` for discovered entries).

> **Note:** `--include-actions` is opt-in because it makes additional GitHub API calls to fetch workflow files. It requires `GITHUB_TOKEN` to be set (the Contents API is used to fetch workflow YAML). It is only supported for GitHub URL inputs (not `--sbom` or `--file go.mod`).

#### Transitive Composite Action Scanning

When combined with `--show-transitive`, `--include-actions` recursively resolves composite actions referenced by the discovered workflows. This includes:

- **Remote composite actions**: Actions that reference other Actions via `uses:` in their `action.yml`
- **Local composite actions**: `./` references resolved via the GitHub Contents API relative to the repository root

```bash
# Include transitive composite action dependencies
./uzomuzo scan https://github.com/owner/repo --include-actions --show-transitive
```

Cycle detection prevents infinite recursion when composite actions reference each other.

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
uzomuzo scan --file go.mod --format sarif > uzomuzo.sarif  # SARIF for code scanning
```

**Smart default:** `detailed` for ≤3 inputs, `table` for bulk.

`--format sarif` writes a SARIF 2.1.0 report. It includes non-ok findings and any
entries selected by `--fail-on`, with `--show-only` applied to the results.
Policy matches have level `error`; other `replace` findings have level `warning`,
and `caution` or `review` findings have level `note`. Output is written before
the policy exit code is returned.

Locations point to dependency declarations in go.mod, workflow YAML, and PURL
list files. Repeated references have multiple locations. SBOM files have
file-level locations; stdin, direct arguments, and remotely discovered Actions
have none. The artifact URI is the input path with forward slashes, a leading
`./` removed, and special characters percent-encoded. For GitHub code scanning, run from the repository
root and supply a repository-relative path. For a complete workflow, see
[Pull Request Annotations](integration-examples.md#pull-request-annotations-github-code-scanning).

<details>
<summary><strong>Example: <code>--format table</code></strong></summary>

<!-- begin:output:usage-request-table -->
```text
$ uzomuzo scan pkg:npm/request@2.88.2 pkg:npm/express@4.18.2 --format table
STATUS     PURL                    LIFECYCLE      BUILD
🔴 replace  pkg:npm/request@2.88.2  EOL-Confirmed  —
✅ ok       pkg:npm/express@4.18.2  Active         Hardened 9.3

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
    "review": 0,
    "build_integrity": {
      "hardened": 0,
      "moderate": 0,
      "weak": 0,
      "ungraded": 1
    }
  },
  "packages": [
    {
      "purl": "pkg:npm/request@2.88.2",
      "verdict": "replace",
      "lifecycle": "EOL-Confirmed",
      "build_integrity": "Ungraded",
      "repo_url": "https://github.com/request/request",
      "overall_score": 3.6,
      "dependent_count": 185814,
      "dependents": {"source": "deps.dev", "version": "2.88.2", "direct": 51145, "total": 185814},
      "dependencies": {"source": "deps.dev", "version": "2.88.2", "direct": 20, "transitive": 26},
      "stable_version": "2.88.2",
      "project_license": "Apache-2.0",
      "version_license": "Apache-2.0",
      "advisory_count": 4,
      "max_advisory_severity": "MEDIUM",
      "max_cvss3_score": 6.5,
      "direct_advisory_count": 1,
      "transitive_advisory_count": 3,
      "max_transitive_advisory_severity": "MEDIUM",
      "max_transitive_cvss3_score": 6.5,
      "reason": "Deprecated in npm registry"
    }
  ],
  "shown": 0
}
```
<!-- end:output:usage-request-json -->

`dependent_count` (and the CSV `dependents` column) are deprecated for one release in favour of `dependents` (see ADR-0033).

`dependencies` reports what `version` depends on, from the deps.dev dependency graph. `version` is the version the graph describes; it can differ from the requested version when deps.dev had no graph for it and a nearby release was counted instead. It is present only when that graph was retrieved, and `0` is a measured zero. It is omitted for cargo, whose deps.dev graph also includes dev and optional dependencies (see ADR-0034).

The JSON format includes all analysis fields (verdict, lifecycle, EOL evidence, scores, license info), making it suitable for CI pipelines and downstream tooling without needing a separate detailed run.

</details>

<details>
<summary><strong>Example: <code>--format csv</code></strong></summary>

<!-- begin:output:usage-request-csv -->
```text
$ uzomuzo scan pkg:npm/request@2.88.2 --format csv
verdict,purl,lifecycle,build_integrity,build_integrity_score,successor,advisory_count,max_advisory_severity,max_cvss3_score,direct_advisory_count,transitive_advisory_count,max_transitive_advisory_severity,max_transitive_cvss3_score,repo_url,source,via
replace,pkg:npm/request@2.88.2,EOL-Confirmed,Ungraded,,,4,MEDIUM,6.5,1,3,MEDIUM,6.5,https://github.com/request/request,,
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

Valid labels: `eol-confirmed`, `eol-effective`, `eol-scheduled`, `stalled`, `legacy-safe`, `review-needed`, `malicious`, `dormant-release`, `archived`, `disabled`

`malicious` fails on a version named in an OSV malicious-package advisory. Any
other non-empty `--fail-on` selection also fails on such a version (see
[ADR-0027](adr/0027-osv-malicious-advisories.md)).

`dormant-release` fails on an npm version that came out in the last 7 days after
a year with no releases, whatever its lifecycle label: any stable version, or
any version in a burst across release lines. No other value implies it: it is a reason to look before adopting, not
a finding, and ordinary releases carry it too (see
[ADR-0028](adr/0028-dormant-release-marker.md)). `review-needed` stops only on
the narrower case below, and only while the label is Review Needed.

`archived` fails when the repository is archived, and `disabled` when GitHub has
disabled it, whatever the lifecycle label or verdict, including entries whose
analysis failed. An archived repository is usually labelled Stalled (see
[ADR-0020](adr/0020-archived-registry-liveness.md)), so `stalled` also catches
it but fails on quiet, unarchived repositories too. No other value implies
either one, and neither implies the other. Both need `GITHUB_TOKEN`: without it
the disabled state is never read and the archived state only through Scorecard,
so the scan refuses these values instead of passing entries it did not check (see
[ADR-0029](adr/0029-archived-disabled-are-facts.md)). The table shows
`· archived` / `· disabled` after the lifecycle label; JSON has `archived` and
`disabled` booleans, and CSV has `archived` and `disabled` columns.

`review-needed` covers the cases uzomuzo cannot decide on its own — including a
package whose every published release is yanked (see
[ADR-0022](adr/0022-all-releases-yanked-is-not-eol.md)), and an npm version
published in the last 7 days after a year with no releases, together with versions on other release
lines (other major versions, or other minor versions below 1.0) within a day: the way past takeovers of npm packages that had gone quiet
(node-ipc, rc) have looked (see [ADR-0026](adr/0026-dormant-release-burst.md)). Add it to `--fail-on`
when CI should stop for a human to look, rather than only for a confirmed
end-of-life.

Without `--fail-on`, exit code is always 0 regardless of scan results.

<details>
<summary><strong>Example: <code>--fail-on</code> exit code behavior</strong></summary>

**Exit 1 — label matches a dependency:**

<!-- begin:output:usage-failon-match -->
```text
$ uzomuzo scan pkg:npm/request@2.88.2 --fail-on eol-confirmed --format table
STATUS     PURL                    LIFECYCLE      BUILD
🔴 replace  pkg:npm/request@2.88.2  EOL-Confirmed  —

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
STATUS     PURL                    LIFECYCLE      BUILD
🔴 replace  pkg:npm/request@2.88.2  EOL-Confirmed  —

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
STATUS     PURL                    LIFECYCLE      BUILD
🔴 replace  pkg:npm/request@2.88.2  EOL-Confirmed  —
✅ ok       pkg:npm/express@4.18.2  Active         Hardened 9.3

── Summary ─────────────────────────────────────────────────
│ 2 dependencies | ✅ 1 ok | ⚠️ 0 caution | 🔴 1 replace | 🔍 0 review
└───────────────────────────────────────────────────────────
# exit code: 1  (request matches eol-confirmed)
```
<!-- end:output:usage-failon-multi -->

`--fail-on` works with all output formats (`detailed`, `table`, `json`, `csv`, `sarif`). Output is produced normally before the exit code is set.

</details>

## Young-release notice

For a versioned input published within the last seven days (configurable), `--format detailed`
shows a publish-age notice and `--format json` includes `release_age` with
`status: "young"`, `published_at`, `source`, `window_days`, and `until`. The
source is `npm`, `pypi`, or `deps.dev`. If no publish time is available, the
time is in the future, or the input does not identify the analysed package, JSON
reports `status: "unknown"` and `window_days`; the detailed view has no notice.
Versions older than the window have no `release_age` field. A dormant-release
marker or malicious advisory suppresses the young-release notice, while JSON
keeps both facts. Table and CSV output are unchanged.

Set `LIFECYCLE_ASSESS_YOUNG_RELEASE_DAYS` to change the window from its default
of seven days (0 or less falls back to seven). This notice does not change a label, verdict, exit code, or
`--fail-on` decision. Run scans on dependency-update PRs or builds before
installing dependencies: a monthly scan may miss the window. Transitive
dependencies are scanned only with `--show-transitive`. This measures version
age, not the upload age of every distribution file. See
[ADR-0031](adr/0031-young-release-marker.md).

## Verdict Mapping

| Verdict | Lifecycle states | Action |
|---------|-----------------|--------|
| `ok` | Active, Legacy-Safe | No action needed |
| `caution` | Stalled, EOL-Scheduled | Monitor; plan migration |
| `replace` | EOL-Confirmed, EOL-Effective; also any label when a malicious-package advisory names the version, or when the repository is archived and the analysis succeeded (the table shows `· archived`) | Migrate immediately |
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

## Analysis Precision and `GITHUB_TOKEN`

uzomuzo runs without `GITHUB_TOKEN`, but use one whenever you can, and always in CI. Package versions, publish dates, Scorecard metrics, advisories, and license info come from **deps.dev**, which requires no token. Commit history and repository state come from GitHub, and without them the results differ. `gopkg.in/yaml.v3` is an example: its repository is archived, and only a token run reports that. A scan without a token prints a warning on stderr.

Setting `GITHUB_TOKEN` adds:

| Signal | Without Token | With Token |
|--------|--------------|------------|
| Archive detection | Only when a Scorecard result reports it | Via Scorecard + GitHub API |
| Human vs. bot commit ratio | Not available | Available — detects Dependabot-only maintenance |
| Last human commit date | Not available | Available — improves Stalled/Active boundary |
| Fork detection | Not available | Available |

### Setting a token

Without commit history, a package that commits but rarely publishes to a registry can be classified as **Stalled**, and an archived repository can go unnoticed. `--fail-on archived` and `--fail-on disabled` refuse to run without a token.

The token requires no special scopes for public repositories — a default `GITHUB_TOKEN` from GitHub Actions or `gh auth login` is sufficient.

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

uzomuzo outputs structured logs via Go's `slog`. Designed for batch/cron operation and incident investigation.

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
- **Lifecycle assessment**: Automatic classification into one of the seven
  lifecycle states in [Lifecycle Classification](../README.md#lifecycle-classification)
- **Repository status**: Stars, forks, archive status, last commit

### CLI Display Order

Lifecycle label order in CLI detailed view (non-CSV):

1. Active
2. Stalled
3. EOL
4. Review Needed
