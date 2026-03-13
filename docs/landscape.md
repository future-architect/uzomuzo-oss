# Landscape Comparison

[← Back to README.md](../README.md)

## The Problem: CVE Blind Spot

Modern software depends on deep trees of transitive open-source dependencies. Several categories of tools exist to manage the associated risks, but each addresses only part of the picture:

- **SBOM generators** (Trivy, Syft) enumerate every component in a container image or repository and flag known CVEs — but they say nothing about whether a package is still maintained.
- **Repository health scorers** (OpenSSF Scorecard) evaluate the security posture of a single repository across 17 automated checks — but they cannot scale to analyze an entire dependency tree in one pass.
- **EOL databases** (endoflife.date) track end-of-life dates for major frameworks and runtimes — but they cover only around 400 projects, leaving the vast majority of OSS (the "Long Tail") completely unmonitored.

This creates a **CVE Blind Spot**: packages with zero known vulnerabilities are assumed safe, even when they have been abandoned for years. These stagnant dependencies are precisely the targets of supply chain attacks — as demonstrated by xz-utils (2024) and event-stream (2018).

uzomuzo addresses this gap by aggregating signals from multiple authoritative sources and producing a definitive lifecycle classification for every dependency.

## Tool Comparison

| Capability | Trivy / Syft | OpenSSF Scorecard | endoflife.date | uzomuzo |
| --- | --- | --- | --- | --- |
| Asset enumeration (SBOM) | Yes | No | No | No (pipe integration) |
| Vulnerability scanning (CVE) | Yes | Partial (Vulns check) | No | No (uses Scorecard data) |
| Single-repo health scoring | No | Yes (17 checks) | No | Yes (via Scorecard) |
| Dependency tree lifecycle assessment | No | No | No | **Yes** |
| Long Tail EOL detection | No | No | No (~400 projects) | **Yes (heuristic + catalog)** |
| Bot vs human commit filtering | No | No | N/A | **Yes** |
| Lifecycle classification granularity | N/A | N/A | Binary (EOL / not) | **7 states** |
| Batch processing scale | N/A | 1 repo per run | N/A | **5,000+ PURLs per run** |
| Pluggable EOL catalogs | No | No | No | **Yes (AnalysisEnricher)** |

## Why Existing Tools Miss the Supply Chain Risk

### Trivy / Syft: "0 CVEs" Does Not Mean "Safe"

Trivy and Syft are excellent at what they do — enumerating packages and matching known CVEs. But a package with 0 CVEs that has been abandoned for 3 years is not "safe." It means:

- No one is watching for new vulnerabilities
- No one will release a patch if a vulnerability is discovered
- No one will respond to security reports

uzomuzo fills this gap: pipe Trivy's SBOM output into uzomuzo to discover which of those "0 CVE" packages are actually abandoned.

### OpenSSF Scorecard: Single-Repo, Not Dependency-Wide

Scorecard evaluates 17 security checks for one repository at a time. Running it across 1,000+ transitive dependencies is impractical. uzomuzo integrates Scorecard data (via deps.dev batch API) and combines it with commit history and publish activity to produce a lifecycle classification at dependency-tree scale.

### endoflife.date: The Long Tail Problem

endoflife.date tracks ~400 major frameworks (Node.js, Django, Rails). But a typical production application has 500-2,000 transitive dependencies, most of which are small utility packages that will never appear in endoflife.date. uzomuzo's heuristic assessment covers this entire Long Tail, and its `AnalysisEnricher` hook allows injecting custom or community-maintained EOL catalogs for the packages in between.

## How uzomuzo Would Have Helped

### xz-utils (CVE-2024-3094)

The xz-utils backdoor was inserted by a social engineering attack on a burned-out single maintainer. Before the backdoor was discovered:

- **Trivy/Syft**: 0 known CVEs. Status: safe.
- **Scorecard**: Maintained score declining as the original maintainer reduced activity.
- **uzomuzo**: Would have flagged the combination of **declining human commit diversity** (single maintainer pattern) and **low Maintained score** as a risk signal for downstream consumers. After the compromised version was published with the backdoor, the advisory would have triggered EOL-Effective classification.

### event-stream (2018)

The event-stream package was handed to a new maintainer who injected cryptocurrency-stealing malware:

- **npm registry**: No deprecation flag. Status: normal.
- **uzomuzo**: The package had **recent commits but no recent stable publish** for a period — the new maintainer was working in a feature branch before injecting the malicious flatmap-stream dependency. The human commit pattern change (new author, different commit cadence) would have appeared in the commit analysis.

These are not guarantees of detection — but they represent signals that existing tools completely miss.

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
