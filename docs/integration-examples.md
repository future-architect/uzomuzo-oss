# Integration Examples

[← Back to README.md](../README.md)

uzomuzo accepts PURLs and GitHub URLs as input. By combining it with SBOM generators like [Trivy](https://github.com/aquasecurity/trivy) or [Syft](https://github.com/anchore/syft), you can assess the lifecycle health of every dependency in a container image or repository.

## Single Package Check

```bash
# Analyze a single PURL
./uzomuzo pkg:golang/github.com/future-architect/vuls@v0.36.2

# Analyze a GitHub repository
./uzomuzo https://github.com/future-architect/vuls
```

## Container Image Scanning (Trivy + uzomuzo)

Generate a CycloneDX SBOM from a container image, extract PURLs, and pipe them into uzomuzo:

```bash
trivy image --scanners vuln --list-all-pkgs --format cyclonedx bitnami/node \
  | jq -r '.components[].purl' \
  | ./uzomuzo --only-eol
```

This workflow:

1. **Trivy** scans the container image and outputs a CycloneDX SBOM (JSON).
2. **jq** extracts the `purl` field from each component.
3. **uzomuzo** reads PURLs from stdin and evaluates their lifecycle status.
4. `--only-eol` filters the output to show only EOL packages (Confirmed, Effective, Scheduled).

You can omit `--only-eol` to see the full lifecycle classification for every component.

## Repository Transitive Dependency Check

Assess the health of all transitive dependencies of a GitHub repository:

```bash
trivy repo --scanners vuln --list-all-pkgs --format cyclonedx https://github.com/future-architect/vuls \
  | jq -r '.components[].purl' \
  | ./uzomuzo --only-eol
```

This identifies EOL or stagnant packages hidden deep in the dependency tree — packages that traditional SCA scanners may report as "0 vulnerabilities" but are operationally abandoned.

## Tracing EOL Dependencies Back to Their Consumer (Go)

When uzomuzo flags an EOL dependency, the next question is: *who depends on it?*

For Go projects, `go mod why` traces the import chain:

```bash
# Step 1: Run uzomuzo to find EOL packages
./uzomuzo https://github.com/future-architect/vuls

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
  | jq -r '.components[].purl' \
  | ./uzomuzo --only-eol
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
./uzomuzo --sample 500 purls.txt
```

## Combining Filters

Filters can be combined with pipe input:

```bash
# Only npm ecosystem, only EOL packages
trivy repo --format cyclonedx https://github.com/example/app \
  | jq -r '.components[].purl' \
  | ./uzomuzo --ecosystem npm --only-eol

# Export license CSV while scanning
trivy image --format cyclonedx my-app:latest \
  | jq -r '.components[].purl' \
  | ./uzomuzo --export-license-csv licenses.csv
```
