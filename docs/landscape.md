# Landscape Comparison

[← Back to README.md](../README.md)

## The Problem: CVE Blind Spot

Modern software depends on deep trees of transitive open-source dependencies. Several categories of tools exist to manage the associated risks, but each addresses only part of the picture:

- **SBOM generators** (Trivy, Syft) enumerate every component in a container image or repository and flag known CVEs — but they say nothing about whether a package is still maintained.
- **Repository health scorers** (OpenSSF Scorecard) evaluate the security posture of a single repository across 17 automated checks — but they cannot scale to analyze an entire dependency tree in one pass.
- **EOL databases** (endoflife.date) track end-of-life dates for major frameworks and runtimes — but they cover only around 400 projects, leaving the vast majority of OSS (the "Long Tail") completely unmonitored.

This creates a **CVE Blind Spot**: packages with zero known vulnerabilities are assumed safe, even when they have been abandoned for years. Once maintenance ends, a newly found vulnerability may never be fixed, and the CVE record may not even list the version you run (see the Tomcat 8.5 example below).

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

## What uzomuzo Can and Cannot See

### What it sees: a version whose maintenance has ended (Apache Tomcat 8.5)

Tomcat 8.5 reached end of life on 2024-03-31. About eleven months later, CVE-2025-24813 (CVSS 9.8 by NVD) was published on 2025-03-10. The record listed only 9.0, 10.1 and 11 as affected, and no fix was released for 8.5. On 2025-04-01 it was added to CISA's Known Exploited Vulnerabilities catalog. 8.5.0–8.5.100 were added to the affected versions on 2025-08-08, about five months after publication; older versions and 10.0 are still `unknown`.

For those five months, the affected-versions field of the CVE record alone did not tell an 8.5 user that they were affected. The end-of-life status was known from the start. This is the gap uzomuzo is built for: telling you that a dependency is no longer maintained, independently of what the CVE data says.

Sources: [Tomcat 8.5 EOL](https://tomcat.apache.org/tomcat-85-eol.html), [CVE record](https://www.cve.org/CVERecord?id=CVE-2025-24813), [NVD change history](https://nvd.nist.gov/vuln/detail/CVE-2025-24813#VulnChangeHistorySection), [announcement on 2025-03-10](https://www.openwall.com/lists/oss-security/2025/03/10/5)

### What it does not judge: who publishes a release

uzomuzo classifies maintenance status. It does not decide whether a release was published by a legitimate maintainer.

- **xz-utils (CVE-2024-3094)**: the backdoor was in the 5.6.0 and 5.6.1 release tarballs, created and signed by someone who was making the project's releases at the time ([tukaani.org](https://tukaani.org/xz-backdoor/), [oss-security](https://www.openwall.com/lists/oss-security/2024/03/29/4)). The project was actively maintained, so its maintenance status was not a warning sign.
- **event-stream (2018)**: the package was handed to a new maintainer, who then added malicious code. A change of hands is not a maintenance-status signal.

For these cases, use signals about publishing and ownership (malicious-package advisories, changes in who can publish), which uzomuzo does not provide.

## Complementary Usage

uzomuzo is designed to **complement**, not replace, existing tools. The typical workflow pipes SBOM output into uzomuzo for lifecycle triage:

```bash
# Trivy generates SBOM → uzomuzo assesses lifecycle health
trivy image --format cyclonedx my-app:latest \
  | ./uzomuzo scan --sbom - --fail-on eol-confirmed,eol-effective
```

This combines Trivy's comprehensive asset enumeration with uzomuzo's lifecycle intelligence, surfacing abandoned dependencies that traditional SCA scanners would report as "0 vulnerabilities — safe."

See [Integration Examples](integration-examples.md) for detailed workflows with Trivy, Syft, and Go module tracing.
