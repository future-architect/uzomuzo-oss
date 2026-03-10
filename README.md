# Uzomuzo

A Go CLI and library for OSS supply chain risk observability. Integrates OpenSSF Scorecard, deps.dev API, and GitHub repository analysis across multiple ecosystems (npm / PyPI / Maven / Go / Cargo / NuGet / RubyGems / Packagist). Built with DDD (Domain-Driven Design) layered architecture.

## Why uzomuzo?

Standard SCA tools like Trivy and Syft excel at enumerating dependencies (SBOMs) and flagging known CVEs, but they cannot assess whether a package is still actively maintained. OpenSSF Scorecard evaluates the security posture of individual repositories, yet it cannot scale to analyze an entire transitive dependency tree. Meanwhile, endoflife.date covers only around 400 major frameworks — leaving the vast majority of OSS (the "Long Tail") completely unmonitored.

This creates a dangerous blind spot: packages with zero known CVEs are assumed safe, even when they have been abandoned for years. These stagnant dependencies are precisely the targets of supply chain attacks like xz-utils and event-stream. Our research found that a significant proportion of active production components are stagnant or effectively dead.

uzomuzo shifts supply chain security from reactive vulnerability patching to **proactive lifecycle governance** — because you cannot defend against what you cannot see.

Specifically, uzomuzo goes beyond the binary "deprecated or not" signal from package registries:

- **Detects effective EOL without official announcements.** Combines maintenance activity, human commit recency, and unpatched vulnerability signals to identify packages that are dead in practice — before registries catch up.
- **Distinguishes bot activity from human maintenance.** Filters out automated commits (Dependabot, Renovate, etc.) so a repository with only bot commits is not mistaken for an actively maintained project.
- **Classifies lifecycle into 7 actionable states** — not just "healthy" or "risky." Active, Stalled, Legacy-Safe, EOL-Confirmed, EOL-Effective, EOL-Scheduled, and Review Needed each maps to a concrete remediation action.
- **Provides evidence trails.** Every lifecycle label includes a reason string and a decision trace, so security teams can audit *why* a package was flagged — not just *that* it was flagged.
- **Pluggable enricher architecture.** Inject private or community-maintained EOL catalogs via the `AnalysisEnricher` hook without forking or modifying core logic.
- **Embeddable as a Go library.** The `pkg/uzomuzo/` facade lets SaaS platforms integrate lifecycle assessment directly, beyond CLI usage.

## Documentation

| Document | Overview |
|----------|----------|
| [Usage](docs/usage.md) | CLI commands, batch processing, filters, configuration, logging |
| [License Resolution](docs/license-resolution.md) | ResolvedLicense / normalization / fallback / promotion |
| [Library Usage](docs/library-usage.md) | Go library API, Evaluator, Analysis type |
| [PURL Identity Model](docs/purl-identity-model.md) | OriginalPURL / EffectivePURL / CanonicalKey 3-layer design |
| [Development Guide](docs/development.md) | SPDX updates, testing, performance, troubleshooting |
| [Data Flow](docs/data-flow.md) | API integration diagram, EOL detection systems |
| [Integration Examples](docs/integration-examples.md) | Trivy/SBOM integration, container scanning, dependency tracing |
| [Landscape Comparison](docs/landscape.md) | Problem space, tool comparison, complementary usage |

## Features

- **Multi-ecosystem support**: npm / PyPI / Maven / Cargo / Go modules / NuGet / RubyGems / Packagist
- **Full PURL (Package URL) support**: Spec-compliant stable identifiers
- **OpenSSF Scorecard integration**: Automated security maturity metrics
- **Parallel-optimized batch processing**: Designed to process 5,000+ PURLs per run with concurrent API orchestration
- **Flexible input**: Direct PURL / GitHub URL / file list / mixed
- **CSV / CLI reports**: Comprehensive output of metrics, licenses, and lifecycle status
- **Lifecycle assessment (AxisResults)**: Active / Stalled / Legacy-Safe / EOL / Review Needed classification
- **Filtering**: Ecosystem-specific / EOL-only / Review Needed-only / combined
- **Robust PURL identity model**: 3-layer separation of `OriginalPURL` / `EffectivePURL` / internal `CanonicalKey` — [details](docs/purl-identity-model.md)
- **Extensible via AnalysisEnricher hook**: Inject custom EOL catalog logic without modifying core — [details](docs/library-usage.md)

## Prerequisites

- Go 1.23+
- GitHub token (recommended for higher rate limits)
- Internet access (deps.dev / GitHub)

## Installation

```bash
git clone https://github.com/future-architect/uzomuzo.git
cd uzomuzo
go build -o uzomuzo main.go
cp config.template.env .env  # Set GITHUB_TOKEN, etc.
```

## Quick Start

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

## Architecture

```text
Interfaces → Application → Domain ← Infrastructure
```

- **Domain**: Business logic only (no external dependencies)
- **Application**: Use case orchestration
- **Infrastructure**: External APIs / parallel processing / I/O
- **Interfaces**: CLI / input validation (no parallel logic)

## Supported Ecosystems

| Ecosystem | Example |
|-----------|---------|
| npm | `pkg:npm/express@4.18.2` |
| PyPI | `pkg:pypi/requests@2.28.1` |
| Maven | `pkg:maven/org.springframework/spring-core@5.3.8` |
| Cargo | `pkg:cargo/serde@1.0.136` |
| Go | `pkg:golang/github.com/gin-gonic/gin@v1.9.1` |
| RubyGems | `pkg:gem/rails@7.0.4` |
| NuGet | `pkg:nuget/Newtonsoft.Json@13.0.1` |
| Composer | `pkg:composer/laravel/framework@10.0.0` |

## Lifecycle Classification

uzomuzo classifies each package into one of seven lifecycle states using a multi-signal decision tree (OpenSSF Scorecard metrics, human commit recency, release activity, registry EOL flags, and unpatched advisory counts):

| Label | Meaning | Typical action |
|-------|---------|----------------|
| **Active** | Recent human commits + releases + healthy maintenance score | No action needed |
| **Stalled** | Some activity signals present, but maintenance score is low or commits have stopped | Monitor; plan migration if trend continues |
| **Legacy-Safe** | No recent activity, but no known vulnerabilities — frozen and stable | Accept risk or pin version |
| **EOL-Confirmed** | Repository archived/disabled, or registry explicitly marks EOL | Migrate immediately |
| **EOL-Effective** | No official EOL, but 2+ years without human commits AND unpatched vulnerabilities remain | Migrate; treat as EOL in practice |
| **EOL-Scheduled** | Future EOL date announced (not yet reached) | Plan migration before the EOL date |
| **Review Needed** | Insufficient data for automated classification | Manual investigation required |

## About

Developed by kotakanbe, creators of [Vuls](https://github.com/future-architect/vuls) — an open-source vulnerability scanner with 12k+ GitHub stars.

## License

See [LICENSE](LICENSE) for details.
