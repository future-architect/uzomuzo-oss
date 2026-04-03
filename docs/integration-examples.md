# Integration Examples

[← Back to README.md](../README.md)

uzomuzo accepts PURLs and GitHub URLs as input. By combining it with SBOM generators like [Trivy](https://github.com/aquasecurity/trivy) or [Syft](https://github.com/anchore/syft), you can assess the lifecycle health of every dependency in a container image or repository.

## Scan Subcommand (Recommended for CI)

The `scan` subcommand accepts CycloneDX SBOM JSON directly — no `jq` extraction needed. It provides a summarized verdict view and supports `--fail-on` for CI exit code gating.

### Filesystem Scan

```bash
# Trivy scans go.mod/package-lock.json/etc. and outputs CycloneDX SBOM
trivy fs . --format cyclonedx | ./uzomuzo scan --sbom -
```

### Container Image Scan

```bash
trivy image my-app:latest --format cyclonedx | ./uzomuzo scan --sbom -
```

### Using with Syft

```bash
syft . -o cyclonedx-json | ./uzomuzo scan --sbom -
```

### CI Pipeline Example

```bash
# Fail the build if any dependency is EOL or archived
trivy fs . --format cyclonedx | ./uzomuzo scan --sbom - --fail-on eol-confirmed,eol-effective --format json
# Exit code 0 = no dependencies matched --fail-on policy
# Exit code 1 = at least one dependency matched --fail-on policy
```

### go.mod Shortcut

For Go-only projects, `scan` can read `go.mod` directly without an SBOM tool:

```bash
./uzomuzo scan                    # auto-detect go.mod in cwd
./uzomuzo scan --file go.mod      # explicit path
```

---

## Pipe Mode (Detailed Per-Package Analysis)

The examples below use the pipe mode for detailed per-package analysis. Use this when you need the full 17+ column output for each dependency rather than the summarized verdict view.

## Single Package Check

```bash
# Scan a single PURL
./uzomuzo scan pkg:golang/github.com/future-architect/vuls@v0.36.2

# Scan a GitHub repository
./uzomuzo scan https://github.com/future-architect/vuls
```

## Container Image Scanning (Trivy + uzomuzo)

Generate a CycloneDX SBOM from a container image, extract PURLs, and pipe them into uzomuzo:

```bash
trivy image --scanners vuln --list-all-pkgs --format cyclonedx bitnami/node \
  | ./uzomuzo scan --sbom - --fail-on eol-confirmed,eol-effective
```

This workflow:

1. **Trivy** scans the container image and outputs a CycloneDX SBOM (JSON).
2. **uzomuzo scan** reads the SBOM from stdin and evaluates each component's lifecycle status.
3. `--fail-on` causes exit code 1 when any component matches the specified lifecycle labels.

You can omit `--fail-on` to see the full lifecycle classification for every component without failing the build.

## Repository Transitive Dependency Check

Assess the health of all transitive dependencies of a GitHub repository:

```bash
trivy repo --scanners vuln --list-all-pkgs --format cyclonedx https://github.com/future-architect/vuls \
  | ./uzomuzo scan --sbom - --fail-on eol-confirmed,eol-effective
```

This identifies EOL or stagnant packages hidden deep in the dependency tree — packages that traditional SCA scanners may report as "0 vulnerabilities" but are operationally abandoned.

## Tracing EOL Dependencies Back to Their Consumer (Go)

When uzomuzo flags an EOL dependency, the next question is: *who depends on it?*

For Go projects, `go mod why` traces the import chain:

```bash
# Step 1: Run uzomuzo to find EOL packages
./uzomuzo scan https://github.com/future-architect/vuls

# Step 2: For each flagged EOL package, trace the dependency chain
go mod why github.com/pkg/errors
```

Example output:

```text
# github.com/pkg/errors
github.com/future-architect/vuls/contrib/snmp2cpe/pkg/cmd/convert
github.com/pkg/errors
```

This tells you exactly which module in your project imports the EOL package, so you can prioritize remediation at the right level.

## Using with Syft

[Syft](https://github.com/anchore/syft) is another SBOM generator that works with the same pipeline:

```bash
syft bitnami/node -o cyclonedx-json \
  | ./uzomuzo scan --sbom - --fail-on eol-confirmed,eol-effective
```

## File-Based Workflow (Alternative)

If you prefer to inspect the PURL list before analysis, save to a file first:

```bash
# Generate and save PURLs
trivy image --format cyclonedx bitnami/node | jq -r '.components[].purl' > purls.txt

# Review the list
wc -l purls.txt   # check count
head purls.txt     # inspect format

# Run analysis
./uzomuzo scan --file purls.txt --sample 500
```

## GitHub Actions Scheduled Scanning

uzomuzo includes a ready-to-use GitHub Actions workflow (`.github/workflows/dependency-scan.yml`) that runs a monthly dependency scan and creates a GitHub Issue with the results.

### What It Does

1. **Generates SBOM** using [Trivy](https://github.com/aquasecurity/trivy) (CycloneDX format)
2. **Runs `uzomuzo scan`** with detailed output (summary table + per-dependency analysis)
3. **Creates a GitHub Issue** with the summary table as a monthly report
4. **Uploads artifacts** (SBOM + detailed report) for download

### Schedule

Runs automatically on the **1st of each month at 09:00 UTC**. Can also be triggered manually via `workflow_dispatch`.

### Manual Trigger

```bash
# Run with defaults (fail-on: eol-confirmed)
gh workflow run dependency-scan.yml

# Run without CI gate (never fail)
gh workflow run dependency-scan.yml --field fail-on=""

# Disable issue creation
gh workflow run dependency-scan.yml --field create-issue=false
```

### Workflow Dispatch Inputs

| Input | Default | Description |
|-------|---------|-------------|
| `fail-on` | `eol-confirmed` | Comma-separated lifecycle labels that trigger exit 1 (empty = never fail) |
| `extra-args` | *(empty)* | Extra arguments passed to `uzomuzo scan` |
| `create-issue` | `true` | Create a GitHub Issue with scan report |

### Example Report Issue

See [Issue #95](https://github.com/future-architect/uzomuzo-oss/issues/95) for a demo report showing EOL detection across npm, PyPI, and Go ecosystems.

### Slack Notification

The workflow creates a GitHub Issue with the `dependencies` label on each run. To receive Slack notifications, use GitHub's native Slack integration:

```
# In your Slack channel:
/github subscribe owner/repo issues

# Filter to dependency scan reports only:
/github subscribe owner/repo issues label:"dependencies"
```

This sends a Slack notification whenever a scan report issue is created, with no secrets or webhook configuration needed.

### Setup for Your Repository

1. Copy `.github/workflows/dependency-scan.yml` to your repository
2. Ensure the `dependencies` label exists (the workflow uses it for issues)
3. Optionally configure Slack notifications via `/github subscribe` (see above)

---

## Combining Options

Options can be combined with SBOM input:

```bash
# CI gate with JSON output
trivy repo --format cyclonedx https://github.com/example/app \
  | ./uzomuzo scan --sbom - --fail-on eol-confirmed --format json

# Table output for quick triage
trivy image --format cyclonedx my-app:latest \
  | ./uzomuzo scan --sbom - --format table
```
