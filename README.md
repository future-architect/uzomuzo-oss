# Uzomuzo

**Proactive lifecycle governance for OSS supply chains.** Detects abandoned, stalled, and effectively dead dependencies that traditional SCA tools report as "0 vulnerabilities — safe."

Integrates OpenSSF Scorecard, deps.dev API, and GitHub repository analysis across 8 ecosystems (npm / PyPI / Maven / Go / Cargo / NuGet / RubyGems / Packagist).

## The Problem: The CVE Blind Spot

Standard SCA tools (Trivy, Syft, Snyk) excel at flagging known CVEs. But they cannot answer: **is this package still maintained?**

A package with zero CVEs today may have been abandoned for years. No one is watching for new vulnerabilities, no one will patch them, and no one will respond to security reports. These stagnant dependencies are precisely the targets of supply chain attacks:

- **xz-utils (2024)**: A single burned-out maintainer was socially engineered into granting commit access to an attacker who inserted a backdoor. Traditional SCA showed 0 CVEs until the backdoor was discovered.
- **event-stream (2018)**: An actively maintained package was handed to a new maintainer who injected cryptocurrency-stealing malware. The npm registry showed no deprecation flag.

In both cases, **lifecycle signals** (maintainer count, commit patterns, maintenance trajectory) could have raised flags before the compromise. uzomuzo makes these signals actionable at scale.

## Key Findings: Production Dependency Analysis

Analysis of 961 packages from a real-world production project reveals the hidden composition of a typical dependency tree:

```text
  36%  Active        — healthy, no action needed
  34%  Legacy-Safe   — frozen, no known vulnerabilities (e.g. function-bind, concat-map)
  23%  Stalled       — maintenance declining, monitor or plan migration
   6%  EOL           — confirmed or effectively dead, migrate immediately
   1%  Review Needed — insufficient data for automated classification
```

**Key insight**: Over a third of dependencies are "Legacy-Safe" — intentionally complete utility packages with zero advisories. Without lifecycle classification, these would either trigger false alarms (incorrectly flagged as abandoned) or be invisible (silently included without assessment). uzomuzo distinguishes "safely frozen" from "dangerously abandoned."

## What Makes uzomuzo Different

| Capability | Trivy / Syft | OpenSSF Scorecard | endoflife.date | **uzomuzo** |
| --- | --- | --- | --- | --- |
| Asset enumeration (SBOM) | Yes | No | No | No (pipe integration) |
| Known vulnerability scanning | Yes | Partial | No | No (uses Scorecard) |
| Single-repo health scoring | No | Yes (17 checks) | No | Yes (via Scorecard) |
| **Dependency tree lifecycle assessment** | No | No | No | **Yes** |
| **Long Tail EOL detection** | No | No | ~400 projects | **Heuristic + catalog** |
| **Bot vs. human commit filtering** | No | No | N/A | **Yes** |
| Lifecycle classification granularity | N/A | N/A | Binary (EOL/not) | **7 actionable states** |
| Batch processing scale | N/A | 1 repo/run | N/A | **5,000+ PURLs/run** |
| Pluggable EOL catalogs | No | No | No | **Yes (AnalysisEnricher)** |

### Technical Novelty

| Innovation | Why it matters |
| --- | --- |
| **Human vs. bot commit separation** | Repositories with only Dependabot/Renovate commits masquerade as actively maintained. uzomuzo filters automated commits to reveal true human maintenance activity. |
| **7-state lifecycle model** | Binary "EOL or not" is insufficient for triage. Active / Stalled / Legacy-Safe / EOL-Confirmed / EOL-Effective / EOL-Scheduled / Review Needed each maps to a concrete remediation action. |
| **Ecosystem-aware delivery model** | Go modules deliver via `go get` (VCS-direct); npm delivers via registry publish. The same "commits without publish" signal means entirely different things depending on the ecosystem. |
| **Scorecard absence ≠ low score** | 85 out of 87 packages classified as "poorly maintained" actually had no Scorecard data at all — they were penalized for missing third-party metrics, not for proven low maintenance. |
| **Graduated precision** | Works without GitHub token (deps.dev only, basic precision); adding a token unlocks commit history and Scorecard for high-precision assessment. |
| **Evidence trails** | Every lifecycle label includes a reason string and decision trace, so security teams can audit *why* a package was flagged — not just *that* it was flagged. |

## Demo: 30-Second Value

```bash
# Pipe Trivy SBOM into uzomuzo — find abandoned deps hiding in production
trivy image --format cyclonedx -q my-app:latest \
  | jq -r '.components[].purl' \
  | ./uzomuzo --only-eol

# Result: packages with 0 CVEs but effectively dead — invisible to SCA tools
```

```bash
# Full lifecycle triage of all dependencies
trivy repo --format cyclonedx https://github.com/example/app \
  | jq -r '.components[].purl' \
  | ./uzomuzo
```

See [Integration Examples](docs/integration-examples.md) for Trivy, Syft, and Go module tracing workflows.

## Sample Output

Real results from a production dependency analysis — four packages that illustrate each lifecycle state:

### Active — `express` (193K dependents)

```text
📦 Package: pkg:npm/express@4.22.1
⚖️  Result: 🟢 Active
💭 Reason: Recent stable package version published with recent human commits; maintenance score ≥ 3
📊 GitHub Info: Normal (⭐ 68954 stars)
👥 Dependents: 192926
🏆 Overall Score: 8.3/10
  🔧 Maintained: 10.0/10
📦 Latest Stable Release: 5.2.1 (2025-12-01)
💻 Latest Commit: 2026-03-01
```

Healthy on all signals: recent publish, recent human commits, high Scorecard. No action needed.

### Legacy-Safe — `function-bind` (1M+ dependents)

```text
📦 Package: pkg:npm/function-bind@1.1.2
⚖️  Result: 🔵 Legacy-Safe
💭 Reason: No known advisories; no human commits for > 2 yrs
👥 Dependents: 1061436
🏆 Overall Score: 4.5/10
  🔧 Maintained: 0.0/10
📦 Latest Stable Release: 1.1.2 (2023-10-12)
   ↳ Stable Advisories: 0
💻 Latest Commit: 2023-10-12
```

Found in over 1 million packages. Scorecard says Maintained 0.0 — but it has zero advisories and does one thing perfectly. Traditional SCA tools either miss this entirely or flag it as risky. uzomuzo correctly classifies it as **frozen and safe**.

### Stalled — `grunt` (12K stars)

```text
📦 Package: pkg:npm/grunt@1.6.1
⚖️  Result: ⚪ Stalled
💭 Reason: Recent human commits, no recent package publishing, maintenance score < 3
📊 GitHub Info: Normal (⭐ 12253 stars)
🏆 Overall Score: 4.0/10
  🔧 Maintained: 0.0/10
📦 Latest Stable Release: 1.6.1 (2023-01-31)
💻 Latest Commit: 2025-11-05
```

The once-dominant JS task runner. Repo still has occasional commits, but no new npm release since 2023 and Maintained 0.0. Not dead, not active — clearly declining. **Monitor and plan migration.**

### EOL-Confirmed — `inflight` (556K dependents)

```text
📦 Package: pkg:npm/inflight@1.0.6
⚖️  Result: 🔴 EOL-Confirmed
💭 Reason: Repository is archived or disabled on GitHub
📚 EOL Evidence:
   • [npmjs] Stable version is deprecated in npm registry.
     Message: This module is not supported, and leaks memory. Do not use it.
🔁 Successor: lru-cache
📊 GitHub Info: 📦 Archived (⭐ 76 stars)
👥 Dependents: 556304
📦 Latest Stable Release: 1.0.6 (2016-10-13) [DEPRECATED]
```

In virtually every Node.js project's dependency tree — 556K dependents. GitHub archived, npm deprecated with an explicit warning. Last release was 2016. An npm-owned package that npm itself has disowned. **Migrate immediately.**

## Lifecycle Classification

uzomuzo classifies each package into one of seven lifecycle states using a multi-signal decision tree (OpenSSF Scorecard metrics, human commit recency, release activity, registry EOL flags, and unpatched advisory counts):

| Label | Meaning | Typical action |
| --- | --- | --- |
| **Active** | Recent human commits + releases + healthy maintenance score | No action needed |
| **Stalled** | Some activity signals present, but maintenance score is low or commits have stopped | Monitor; plan migration if trend continues |
| **Legacy-Safe** | No recent activity, but no known vulnerabilities — frozen and stable | Accept risk or pin version |
| **EOL-Confirmed** | Repository archived/disabled, or registry explicitly marks EOL | Migrate immediately |
| **EOL-Effective** | No official EOL, but 2+ years without human commits AND unpatched vulnerabilities remain | Migrate; treat as EOL in practice |
| **EOL-Scheduled** | Future EOL date announced (not yet reached) | Plan migration before the EOL date |
| **Review Needed** | Insufficient data for automated classification | Manual investigation required |

## Assessment Precision by Data Availability

Lifecycle assessment accuracy improves with `GITHUB_TOKEN`. The tool works without a token using only deps.dev data, but setting a token unlocks commit-based signals that significantly refine classification.

| Data source | Without token | With token |
| --- | --- | --- |
| **deps.dev** (publish dates, advisories, licenses) | Available | Available |
| **GitHub** (human commit history, archive/disable status) | Unavailable | Available |
| **OpenSSF Scorecard** (Maintained, Vulnerabilities scores) | Unavailable | Available |

| Capability | Without token | With token |
| --- | --- | --- |
| Recent publish → Active | Yes | Yes |
| Commit activity without publish → Active | No (commits invisible) | **Yes** |
| VCS-direct ecosystem detection (Go, Composer) | No effect | **Active even without publish** |
| Scorecard absence vs. low score distinction | No effect | **Prevents false Stalled** |
| Zero-advisory dormant → Legacy-Safe | Partial (publish date only) | **Precise (commit timestamp)** |
| Unpatched vulns + dormant → EOL-Effective | No | **Yes** |

**Recommendation**: Always set `GITHUB_TOKEN` for production use. Without it, packages with active commits but no recent publish will be misclassified as Stalled or Review Needed.

## Supported Ecosystems

| Ecosystem | Example |
| --- | --- |
| npm | `pkg:npm/express@4.18.2` |
| PyPI | `pkg:pypi/requests@2.28.1` |
| Maven | `pkg:maven/org.springframework/spring-core@5.3.8` |
| Cargo | `pkg:cargo/serde@1.0.136` |
| Go | `pkg:golang/github.com/gin-gonic/gin@v1.9.1` |
| RubyGems | `pkg:gem/rails@7.0.4` |
| NuGet | `pkg:nuget/Newtonsoft.Json@13.0.1` |
| Composer | `pkg:composer/laravel/framework@10.0.0` |

## Quick Start

### Prerequisites

- Go 1.23+
- GitHub token (strongly recommended — enables commit-based assessment; see [Assessment Precision](#assessment-precision-by-data-availability))
- Internet access (deps.dev / GitHub)

### Installation

```bash
git clone https://github.com/future-architect/uzomuzo.git
cd uzomuzo
go build -o uzomuzo main.go
cp config.template.env .env  # Set GITHUB_TOKEN, etc.
```

### Usage

```bash
# Single package analysis
./uzomuzo pkg:npm/express@4.18.2

# GitHub repository analysis
./uzomuzo https://github.com/expressjs/express

# Multiple inputs
./uzomuzo pkg:npm/express@4.18.2 pkg:pypi/requests@2.28.1

# File input (one PURL per line)
./uzomuzo --sample 500 input_purls.txt
```

See [Usage](docs/usage.md) for full CLI commands, batch processing, filters, configuration, and logging.

## Features

- **Multi-ecosystem support**: npm / PyPI / Maven / Cargo / Go modules / NuGet / RubyGems / Packagist
- **Full PURL (Package URL) support**: Spec-compliant stable identifiers
- **OpenSSF Scorecard integration**: Automated security maturity metrics
- **Parallel-optimized batch processing**: Designed to process 5,000+ PURLs per run with concurrent API orchestration
- **Flexible input**: Direct PURL / GitHub URL / file list / mixed / stdin pipe
- **CSV / CLI reports**: Comprehensive output of metrics, licenses, and lifecycle status
- **Robust PURL identity model**: 3-layer separation of `OriginalPURL` / `EffectivePURL` / internal `CanonicalKey` — [details](docs/purl-identity-model.md)
- **Extensible via AnalysisEnricher hook**: Inject custom EOL catalog logic without modifying core — [details](docs/library-usage.md)
- **Embeddable as a Go library**: The `pkg/uzomuzo/` facade lets SaaS platforms integrate lifecycle assessment directly — [details](docs/library-usage.md)

## Architecture

```text
Interfaces → Application → Domain ← Infrastructure
```

- **Domain**: Pure business logic — lifecycle decision tree, ecosystem models, entity definitions (no external dependencies)
- **Application**: Use case orchestration with `AnalysisEnricher` hook pattern for pluggable EOL catalogs
- **Infrastructure**: External APIs (deps.dev, GitHub GraphQL, Scorecard) / parallel processing / I/O
- **Interfaces**: CLI entry points / input validation (no concurrent logic)

See [Data Flow](docs/data-flow.md) for API integration diagram and two-path assessment architecture.

## Documentation

| Document | Overview |
| --- | --- |
| [Usage](docs/usage.md) | CLI commands, batch processing, filters, configuration, logging |
| [Data Flow](docs/data-flow.md) | API integration diagram, two-path assessment architecture, EOL detection |
| [Integration Examples](docs/integration-examples.md) | Trivy/SBOM integration, container scanning, dependency tracing |
| [Landscape Comparison](docs/landscape.md) | Problem space, tool comparison, complementary usage |
| [Library Usage](docs/library-usage.md) | Go library API, Evaluator, Analysis type |
| [PURL Identity Model](docs/purl-identity-model.md) | OriginalPURL / EffectivePURL / CanonicalKey 3-layer design |
| [License Resolution](docs/license-resolution.md) | ResolvedLicense / normalization / fallback / promotion |
| [Development Guide](docs/development.md) | SPDX updates, testing, performance, troubleshooting |

## About

Developed by kotakanbe, creator of [Vuls](https://github.com/future-architect/vuls) — an open-source vulnerability scanner with 12,000+ GitHub stars. uzomuzo extends this mission from reactive vulnerability scanning to **proactive supply chain lifecycle governance**: identifying risks that exist before CVEs are published.

## License

See [LICENSE](LICENSE) for details.
