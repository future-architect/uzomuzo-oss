# Uzomuzo

[![CI](https://github.com/future-architect/uzomuzo-oss/actions/workflows/ci.yml/badge.svg)](https://github.com/future-architect/uzomuzo-oss/actions/workflows/ci.yml) [![Dependency Scan](https://github.com/future-architect/uzomuzo-oss/actions/workflows/dependency-scan.yml/badge.svg)](https://github.com/future-architect/uzomuzo-oss/actions/workflows/dependency-scan.yml) [![Go Report Card](https://goreportcard.com/badge/github.com/future-architect/uzomuzo-oss)](https://goreportcard.com/report/github.com/future-architect/uzomuzo-oss) [![Go Reference](https://pkg.go.dev/badge/github.com/future-architect/uzomuzo-oss.svg)](https://pkg.go.dev/github.com/future-architect/uzomuzo-oss) [![Release](https://img.shields.io/github/v/release/future-architect/uzomuzo-oss)](https://github.com/future-architect/uzomuzo-oss/releases/latest) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**Find abandoned dependencies before they become vulnerabilities.** SCA tools report "0 CVEs — safe." uzomuzo detects the packages no one is maintaining anymore.

> `./uzomuzo scan pkg:npm/inflight@1.0.6` — inflight has 556K dependents, yet its repository is archived and npm has deprecated it. uzomuzo detects this as **EOL-Confirmed** in seconds.

## The Problem: The CVE Blind Spot

Standard SCA tools (Trivy, Syft, Snyk) excel at flagging known CVEs. But they cannot answer: **is this package still maintained?**

A package with zero CVEs today may have been abandoned for years — no one is watching for new vulnerabilities, no one will patch them, and no one will respond to security reports. These are precisely the targets of supply chain attacks (xz-utils 2024, event-stream 2018).

### What SCA misses — EOL-Effective

```text
── pkg:npm/dicer@0.3.1 ─────────────────────────────────────
│ A very fast streaming multipart parser for node.js
│ 🔴 EOL-Effective: Unmaintained with unpatched
│                  vulnerabilities
├─ Signals ─────────────────────────────────────────────────
│ Last Human Commit: (unavailable)
│ Maintained Score: 0/10
│ Days Since Release: 1567
│ Advisories: 1
│ Max Advisory Severity: HIGH 7.5
├─ Health ──────────────────────────────────────────────────
│ 188 stars
│ Used by: 1271 packages
│ Depends on: 1 direct, 0 transitive
│ Score: 2.8/10  Maintained: 0.0/10
├─ Releases ────────────────────────────────────────────────
│ Stable: 0.3.1 (2021-12-19)  ⚠️ 1 advisory
│   HIGH     (7.5)  GHSA-wm7h-9275-46v2
│   → https://deps.dev/npm/dicer/0.3.1
├─ Links ───────────────────────────────────────────────────
│ Repository: https://github.com/mscdex/dicer
│ Registry: https://www.npmjs.com/package/dicer
│ deps.dev: https://deps.dev/npm/dicer
└───────────────────────────────────────────────────────────
```

No official deprecation, no archived repository — yet `dicer` has an unpatched ReDoS vulnerability (CVSS 7.5 — HIGH severity) with zero human commits in over two years. SCA tools report "1 CVE" and move on. uzomuzo recognizes the combination of HIGH/CRITICAL unpatched advisory + maintenance absence as **effectively end-of-life**. This package sits in the Express dependency chain (via busboy → multer), meaning millions of applications silently depend on abandoned code.

### Real-world scan: OWASP Juice Shop

```bash
trivy image --format cyclonedx bkimminich/juice-shop:v14.5.1 \
  | ./uzomuzo scan --sbom - --fail-on eol-confirmed,eol-effective
```

```text
🏷️  LABEL SUMMARY (1,540 evaluated packages):
  🟢 Active:        630 (40.9%)
  🔵 Legacy-Safe:   556 (36.1%)
  ⚪ Stalled:       263 (17.1%)
  🔴 EOL-Confirmed:  88 (5.7%)
  🛑 EOL-Effective:    3 (0.2%)
```

**59% of dependencies have lifecycle concerns invisible to SCA tools.** See the [full scan result](docs/assets/juice-shop-eol-result.txt).

## Installation

### Pre-built binaries (recommended)

Download the latest release from [GitHub Releases](https://github.com/future-architect/uzomuzo-oss/releases).

### Go install

```bash
go install github.com/future-architect/uzomuzo-oss/cmd/uzomuzo@latest
```

### Build from source

```bash
git clone https://github.com/future-architect/uzomuzo-oss.git
cd uzomuzo-oss
go build -o uzomuzo ./cmd/uzomuzo
```

## Quick Start

```bash
export GITHUB_TOKEN=ghp_...  # optional; enables commit history and Scorecard
```

```bash
# Single package
uzomuzo scan pkg:npm/express@4.18.2

# GitHub repository
uzomuzo scan https://github.com/expressjs/express

# Scan project dependencies via SBOM (direct deps only — transitive issues
# are resolved by updating the direct dep that pulls them in)
trivy fs . --format cyclonedx | uzomuzo scan --sbom -
trivy fs . --format cyclonedx | uzomuzo scan --sbom - --show-transitive  # include transitive
uzomuzo scan                     # auto-detect go.mod in cwd
uzomuzo scan --format json       # JSON output for CI integration

# CI gate: exit 1 if any EOL dependency found
uzomuzo scan --sbom bom.json --fail-on eol-confirmed

# Batch from Trivy SBOM
trivy image --format cyclonedx bkimminich/juice-shop:v14.5.1 \
  | uzomuzo scan --sbom - --fail-on eol-confirmed,eol-effective

# Scan a repo's GitHub Actions dependencies
uzomuzo scan https://github.com/owner/repo --include-actions

# File input (one PURL per line)
uzomuzo scan --file input_purls.txt --sample 500
```

See [Usage](docs/usage.md) for full CLI reference and [Integration Examples](docs/integration-examples.md) for Trivy, Syft, and Go module workflows.

## Lifecycle Classification

uzomuzo classifies each package into one of seven lifecycle states using a multi-signal decision tree (OpenSSF Scorecard, human commit recency, release activity, registry EOL flags, advisory severity, and unpatched advisory counts):

| Label | Meaning | Action |
| --- | --- | --- |
| **Active** | Recent human commits + releases + healthy maintenance score | No action needed |
| **Legacy-Safe** | No recent activity, but zero vulnerabilities — frozen and stable | Accept risk or pin version |
| **Stalled** | Maintenance declining: low score or commits stopped | Monitor; plan migration |
| **EOL-Confirmed** | Repository archived/disabled, or registry explicitly marks EOL | Migrate immediately |
| **EOL-Effective** | No official EOL, but 2+ yrs without human commits AND HIGH/CRITICAL unpatched vulns | Migrate; treat as EOL |
| **EOL-Scheduled** | Future EOL date announced (not yet reached) | Plan migration before EOL date |
| **Review Needed** | Insufficient data for automated classification | Manual investigation required |

<a id="assessment-precision-by-data-availability"></a>

## What Makes uzomuzo Different

| Capability | Trivy / Syft | OpenSSF Scorecard | endoflife.date | **uzomuzo** |
| --- | --- | --- | --- | --- |
| Known vulnerability scanning | Yes | Partial | No | No (uses Scorecard) |
| Single-repo health scoring | No | Yes (17 checks) | No | Yes (via Scorecard) |
| **Dependency tree lifecycle assessment** | No | No | No | **Yes** |
| **Long Tail EOL detection** | No | No | ~400 projects | **Heuristic + catalog** |
| **Bot vs. human commit filtering** | No | No | N/A | **Yes** |
| Lifecycle classification granularity | N/A | N/A | Binary (EOL/not) | **7 actionable states** |
| Batch processing scale | N/A | 1 repo/run | N/A | **5,000+ PURLs/run** |

### Technical Novelty

| Innovation | Why it matters |
| --- | --- |
| **Human vs. bot commit separation** | Repositories with only Dependabot/Renovate commits masquerade as maintained. uzomuzo filters automated commits to reveal true human activity. |
| **7-state lifecycle model** | Binary "EOL or not" is insufficient. Each state maps to a concrete remediation action. |
| **Ecosystem-aware delivery model** | Go modules deliver via VCS-direct; npm via registry publish. The same "commits without publish" signal means different things per ecosystem. |
| **Evidence trails** | Every label includes a reason string and decision trace, so security teams can audit *why* a package was flagged. |
| **Graduated precision** | Works without GitHub token (deps.dev only); adding a token unlocks commit history and Scorecard for high-precision assessment. |

<details>
<summary><strong>Sample Output — All lifecycle states (detailed format)</strong></summary>

### Active — `express` (193K dependents)

```text
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

### Stalled — `moment` (2K+ dependents)

```text
── pkg:npm/moment@2.29.4 ───────────────────────────────────
│ Parse, validate, manipulate, and display dates in
│   javascript.
│ ⚠️ Stalled: Low maintenance, no commit data
├─ Signals ─────────────────────────────────────────────────
│ Last Human Commit: (unavailable)
│ Maintained Score: 0/10
├─ Health ──────────────────────────────────────────────────
│ 48019 stars
│ Used by: 2452 packages
│ Score: 3.1/10  Maintained: 0.0/10
├─ Releases ────────────────────────────────────────────────
│ Stable: 2.30.1 (2023-12-27)
│ Requested: 2.29.4 (2022-07-06)
├─ Links ───────────────────────────────────────────────────
│ Homepage: momentjs.com
│ Repository: https://github.com/moment/moment
│ Registry: https://www.npmjs.com/package/moment
│ deps.dev: https://deps.dev/npm/moment
└───────────────────────────────────────────────────────────
```

Scorecard says Maintained 0.0 — but zero advisories and does one thing perfectly. Watch for maintenance decline.

### Stalled — `gorilla/mux` (22K stars)

```text
── pkg:golang/github.com/gorilla/mux@1.8.1 ─────────────────
│ Package gorilla/mux is a powerful HTTP router and URL
│   matcher for building Go web servers with 🦍
│ ⚠️ Stalled: Low maintenance, no commit data
├─ Signals ─────────────────────────────────────────────────
│ Last Human Commit: (unavailable)
│ Maintained Score: 0/10
├─ Health ──────────────────────────────────────────────────
│ 21814 stars
│ Score: 4.9/10  Maintained: 0.0/10
├─ Releases ────────────────────────────────────────────────
│ Stable: v1.8.1 (2023-10-18)
│ Highest (SemVer): v1.8.2-0.20240619235004-fe14465e5077 (2024-06-19)
├─ Links ───────────────────────────────────────────────────
│ Homepage: https://gorilla.github.io
│ Repository: https://github.com/gorilla/mux
│ Registry: https://pkg.go.dev/github.com%2Fgorilla%2Fmux
│ deps.dev: https://deps.dev/go/github.com/gorilla/mux
└───────────────────────────────────────────────────────────
```

No release since 2023, Maintained 0.0. Not dead, not active — clearly declining.

### EOL-Effective — `dicer` (busboy → multer → express)

```text
── pkg:npm/dicer@0.3.1 ─────────────────────────────────────
│ A very fast streaming multipart parser for node.js
│ 🔴 EOL-Effective: Unmaintained with unpatched
│                  vulnerabilities
├─ Signals ─────────────────────────────────────────────────
│ Last Human Commit: (unavailable)
│ Maintained Score: 0/10
│ Days Since Release: 1567
│ Advisories: 1
│ Max Advisory Severity: HIGH 7.5
├─ Health ──────────────────────────────────────────────────
│ 188 stars
│ Used by: 1271 packages
│ Depends on: 1 direct, 0 transitive
│ Score: 2.8/10  Maintained: 0.0/10
├─ Releases ────────────────────────────────────────────────
│ Stable: 0.3.1 (2021-12-19)  ⚠️ 1 advisory
│   HIGH     (7.5)  GHSA-wm7h-9275-46v2
│   → https://deps.dev/npm/dicer/0.3.1
├─ Links ───────────────────────────────────────────────────
│ Repository: https://github.com/mscdex/dicer
│ Registry: https://www.npmjs.com/package/dicer
│ deps.dev: https://deps.dev/npm/dicer
└───────────────────────────────────────────────────────────
```

No deprecation, no archive — but unpatched ReDoS + zero maintenance. **SCA blind spot.**

### EOL-Effective — `dgrijalva/jwt-go` (archived repository)

```text
── pkg:golang/github.com/dgrijalva/jwt-go@3.2.0 ────────────
│ ARCHIVE - Golang implementation of JSON Web Tokens (JWT).
│   This project is now maintained at:
│ 🔴 EOL-Effective: Unmaintained with unpatched
│                  vulnerabilities
├─ Signals ─────────────────────────────────────────────────
│ Last Human Commit: (unavailable)
│ Maintained Score: 0/10
│ Days Since Release: 1706
│ Advisories: 2
│ Max Advisory Severity: HIGH 7.5
├─ Health ──────────────────────────────────────────────────
│ 10758 stars
│ Score: 2.8/10  Maintained: 0.0/10
├─ Releases ────────────────────────────────────────────────
│ Stable: v3.2.0+incompatible (2018-03-08)  ⚠️ 2 advisories
│   HIGH     (7.5)  GHSA-w73w-5m7g-f7qc
│                   GO-2020-0017
│   → https://deps.dev/go/github.com/dgrijalva/jwt-go/v3.2.0+incompatible
│ Highest (SemVer): v4.0.0-20210802184156-9742bd7fca1c+incompatible (2021-08-02)
├─ Links ───────────────────────────────────────────────────
│ Homepage: https://github.com/golang-jwt/jwt
│ Repository: https://github.com/dgrijalva/jwt-go
│ Registry: https://pkg.go.dev/github.com%2Fdgrijalva%2Fjwt-go
│ deps.dev: https://deps.dev/go/github.com/dgrijalva/jwt-go
└───────────────────────────────────────────────────────────
```

Successor is `golang-jwt/jwt`. **Migrate immediately.**

### EOL-Confirmed — `request` (186K dependents, npm deprecated)

```text
── pkg:npm/request@2.88.2 ──────────────────────────────────
│ 🏊🏾 Simplified HTTP request client.
│ 🔴 EOL-Confirmed: Stable version is deprecated in npm registry. Message: request has been deprecated, see https://github.com/request/request/issues/3142 UI: https://www.npmjs.com/package/request/v/2.88.2
├─ Signals ─────────────────────────────────────────────────
│ EOL Source: npmjs
├─ EOL ─────────────────────────────────────────────────────
│ Evidence (1):
│   [npmjs] Stable version is deprecated in npm registry. Message: request has been deprecated, see https://github.com/request/request/issues/3142 UI: https://www.npmjs.com/package/request/v/2.88.2 (confidence 0.90)
│     ↳ https://registry.npmjs.org/request
├─ Health ──────────────────────────────────────────────────
│ 25580 stars
│ Used by: 186349 packages
│ Depends on: 20 direct, 26 transitive
│ Score: 3.6/10  Maintained: 0.0/10
├─ Releases ────────────────────────────────────────────────
│ Stable: 2.88.2 (2020-02-11)  ⚠️ 1 advisory (+ 3 transitive) ⚠️ [DEPRECATED]
│   MEDIUM   (6.1)  GHSA-p8p7-x288-28g6
│   → https://deps.dev/npm/request/2.88.2
│   Transitive (via tough-cookie@2.5.0, qs@6.5.5, form-data@2.3.3):
│     MEDIUM   (6.5)  GHSA-72xf-g2v4-qvf3
│     LOW      (3.7)  GHSA-6rw7-vpxm-498p
│                     GHSA-fjxv-7rqg-78g4
│     → https://deps.dev/npm/tough-cookie/2.5.0
│     → https://deps.dev/npm/qs/6.5.5
│     → https://deps.dev/npm/form-data/2.3.3
├─ Links ───────────────────────────────────────────────────
│ Repository: https://github.com/request/request
│ Registry: https://www.npmjs.com/package/request
│ deps.dev: https://deps.dev/npm/request
└───────────────────────────────────────────────────────────
```

186K dependents. npm deprecated with deprecation message, 1 direct advisory + 3 transitive advisories from vulnerable sub-dependencies. Last release 2020. **Migrate immediately.**

</details>

<details>
<summary><strong>Sample Output — Table format (mixed statuses)</strong></summary>

```text
$ uzomuzo scan pkg:npm/express@4.18.2 pkg:npm/moment@2.29.4 \
    pkg:golang/github.com/gorilla/mux@1.8.1 pkg:npm/dicer@0.3.1 \
    pkg:golang/github.com/dgrijalva/jwt-go@3.2.0 pkg:npm/request@2.88.2 \
    -f table

STATUS      PURL                                          LIFECYCLE
✅ ok        pkg:npm/express@4.18.2                        Active
⚠️ caution  pkg:npm/moment@2.29.4                         Stalled
⚠️ caution  pkg:golang/github.com/gorilla/mux@1.8.1       Stalled
🔴 replace   pkg:npm/dicer@0.3.1                           EOL-Effective
🔴 replace   pkg:golang/github.com/dgrijalva/jwt-go@3.2.0  EOL-Effective
🔴 replace   pkg:npm/request@2.88.2                        EOL-Confirmed

── Summary ─────────────────────────────────────────────────
│ 6 dependencies | ✅ 1 ok | ⚠️ 2 caution | 🔴 3 replace | 🔍 0 review
└───────────────────────────────────────────────────────────
```

</details>

<details>
<summary><strong>Sample Output — go.mod input</strong></summary>

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

go.mod input adds a `RELATION` column showing `direct` or `indirect` dependency relationship.

</details>

<details>
<summary><strong>Sample Output — GitHub Actions workflow input</strong></summary>

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

── https://github.com/actions/setup-go ─────────────────────
│ Package: pkg:golang/github.com/actions/setup-go@v6.4.0+incompatible
│ Description: Set up your GitHub Actions workflow with a specific version of Go
│ ✅ Active
│ Reason: Recent stable package version published with recent human commits; maintenance score ≥ 3
├─ Health ──────────────────────────────────────────────────
│ 1673 stars
│ Score: 6.1/10  Maintained: 10.0/10
│ Last Commit: 2026-03-17
├─ Releases ────────────────────────────────────────────────
│ Stable: v6.4.0+incompatible (2026-03-17)
├─ License ─────────────────────────────────────────────────
│ MIT (depsdev)
├─ Links ───────────────────────────────────────────────────
│ Repository: https://github.com/actions/setup-go
│ Registry: https://pkg.go.dev/github.com%2Factions%2Fsetup-go
│ deps.dev: https://deps.dev/go/github.com/actions/setup-go
└───────────────────────────────────────────────────────────
```

Workflow scan extracts `uses:` directives and evaluates each referenced Action as a GitHub repository.

</details>

## Supported Ecosystems

npm / PyPI / Maven / Cargo / Go modules / NuGet / RubyGems / Packagist

## Features

- **Multi-ecosystem support**: 8 ecosystems with full PURL (Package URL) spec compliance
- **OpenSSF Scorecard integration**: Automated security maturity metrics
- **Parallel-optimized batch processing**: 5,000+ PURLs/run with concurrent API orchestration
- **Unified scan subcommand**: Single entry point for PURL, GitHub URL, SBOM, go.mod — with `--fail-on` CI exit code gating
- **Flexible input**: Direct PURL / GitHub URL / file list / mixed / stdin pipe
- **CSV / CLI reports**: Comprehensive output of metrics, licenses, and lifecycle status
- **Extensible via AnalysisEnricher hook**: Inject custom EOL catalog logic without modifying core — [details](docs/library-usage.md)
- **Embeddable as a Go library**: `pkg/uzomuzo/` facade for SaaS integration — [details](docs/library-usage.md)
- **Automated monthly scanning**: GitHub Actions workflow with Trivy SBOM generation and GitHub Issue publication, with Slack notifications available via GitHub issue subscriptions/integrations — [details](docs/integration-examples.md#github-actions-scheduled-scanning)

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
| [Data Flow](docs/data-flow.md) | API integration diagram, two-path assessment architecture |
| [Integration Examples](docs/integration-examples.md) | Trivy/SBOM integration, container scanning, dependency tracing, GitHub Actions scheduled scanning |
| [Landscape Comparison](docs/landscape.md) | Problem space, tool comparison, complementary usage |
| [Library Usage](docs/library-usage.md) | Go library API, Evaluator, Analysis type |
| [PURL Identity Model](docs/purl-identity-model.md) | OriginalPURL / EffectivePURL / CanonicalKey 3-layer design |
| [License Resolution](docs/license-resolution.md) | ResolvedLicense / normalization / fallback / promotion |
| [Development Guide](docs/development.md) | SPDX updates, testing, performance, troubleshooting |

## Why "Uzomuzo"?

Pronounced **oo-zoh-moo-zoh** — from the Japanese *uzōmuzō* (有象無象).

In Japanese Buddhist philosophy, *uzō* (有象) means "things with form" and *muzō* (無象) means "things without form." Together, *uzōmuzō* originally described "all things in the universe — the visible and the invisible."

Modern software supply chains are exactly that: a vast universe of seen (direct) and unseen (transitive) dependencies. **uzomuzo** illuminates this complexity — mapping every element of your dependency tree to bring clarity to the chaos.

## About

Developed by kotakanbe, creator of [Vuls](https://github.com/future-architect/vuls) — an open-source vulnerability scanner with 12,000+ GitHub stars. uzomuzo extends this mission from reactive vulnerability scanning to **proactive supply chain lifecycle governance**.

## Sponsor

If you find uzomuzo useful, consider [sponsoring the maintainer](https://github.com/sponsors/kotakanbe).

[![GitHub Sponsors](https://img.shields.io/github/sponsors/kotakanbe?style=for-the-badge&logo=github&label=Sponsor)](https://github.com/sponsors/kotakanbe)

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.
