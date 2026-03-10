# Landscape Comparison

[← Back to README.md](../README.md)

## The Problem

Modern software depends on deep trees of transitive open-source dependencies. Several categories of tools exist to manage the associated risks, but each addresses only part of the picture:

- **SBOM generators** (Trivy, Syft) enumerate every component in a container image or repository and flag known CVEs — but they say nothing about whether a package is still maintained.
- **Repository health scorers** (OpenSSF Scorecard) evaluate the security posture of a single repository across 17 automated checks — but they cannot scale to analyze an entire dependency tree in one pass.
- **EOL databases** (endoflife.date) track end-of-life dates for major frameworks and runtimes — but they cover only around 400 projects, leaving the vast majority of OSS (the "Long Tail") completely unmonitored.

This creates a **CVE Blind Spot**: packages with zero known vulnerabilities are assumed safe, even when they have been abandoned for years. Our research found that a significant proportion of active production components are stagnant or effectively dead. These stagnant dependencies are precisely the targets of supply chain attacks — as demonstrated by incidents like xz-utils and event-stream.

uzomuzo addresses this gap by aggregating signals from multiple authoritative sources and producing a definitive lifecycle classification for every dependency.

## Tool Comparison

| Capability | Trivy / Syft | OpenSSF Scorecard | endoflife.date | uzomuzo |
| --- | --- | --- | --- | --- |
| Asset enumeration (SBOM) | Yes | No | No | No (pipe integration) |
| Vulnerability scanning (CVE) | Yes | Partial (Vulns check) | No | No (uses Scorecard data) |
| Single-repo health scoring | No | Yes (17 checks) | No | Yes (via Scorecard) |
| Dependency tree lifecycle assessment | No | No | No | Yes |
| Long Tail EOL detection | No | No | No (~400 projects) | Yes (heuristic + catalog) |
| Bot vs human commit filtering | No | No | N/A | Yes |
| Lifecycle classification granularity | N/A | N/A | Binary (EOL / not) | 7 states |
| Batch processing scale | N/A | 1 repo per run | N/A | 5,000+ PURLs per run |
| Pluggable EOL catalogs | No | No | No | Yes (AnalysisEnricher) |

## Complementary Usage

uzomuzo is designed to **complement**, not replace, existing tools. The typical workflow pipes SBOM output into uzomuzo for lifecycle triage:

```bash
# Trivy generates SBOM → uzomuzo assesses lifecycle health
trivy image --format cyclonedx my-app:latest \
  | jq -r '.components[].purl' \
  | ./uzomuzo --only-eol
```

This combines Trivy's comprehensive asset enumeration with uzomuzo's lifecycle intelligence, surfacing abandoned dependencies that traditional SCA scanners would report as "0 vulnerabilities — safe."

See [Integration Examples](integration-examples.md) for detailed workflows with Trivy, Syft, and Go module tracing.
