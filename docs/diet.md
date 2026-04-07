# Diet Command

[← Back to README.md](../README.md)

## Overview

`uzomuzo diet` analyzes your project's dependencies and produces a prioritized "diet plan" — ranking dependencies by removal impact, coupling effort, and health risk.

It answers: **which dependencies should I remove first, and how hard will it be?**

## Architecture

`uzomuzo diet` is distributed as a separate binary (`uzomuzo-diet`) because it uses tree-sitter (CGo) for multi-language source analysis. The main `uzomuzo` binary stays Pure Go and delegates to `uzomuzo-diet` transparently.

```
$ uzomuzo diet --sbom bom.json    # delegates to uzomuzo-diet on PATH
```

See [ADR-0014](adr/0014-diet-command-architecture.md) for the full architectural decision record.

## Installation

```bash
# Install both binaries
go install github.com/future-architect/uzomuzo-oss/cmd/uzomuzo@latest
go install github.com/future-architect/uzomuzo-oss/cmd/uzomuzo-diet@latest
```

> **Note:** `uzomuzo-diet` requires a C compiler (gcc/clang) for tree-sitter CGo compilation.

## Usage

### Prerequisites

Generate a CycloneDX SBOM for your project. The recommended tool depends on your ecosystem:

#### Go / Python / JavaScript / TypeScript

```bash
# Using syft (recommended)
syft . --source-name myproject -o cyclonedx-json > bom.json

# Using trivy
trivy fs . --format cyclonedx -o bom.json

# Using cdxgen
cdxgen -o bom.json
```

> **Note:** For JavaScript/TypeScript projects, a lockfile (`package-lock.json` or `yarn.lock`) is required for dependency graph resolution.

#### Java (Maven)

Static SBOM tools (syft, Trivy) **cannot resolve Maven's transitive dependency graph** without running Maven. Use the [CycloneDX Maven Plugin](https://github.com/CycloneDX/cyclonedx-maven-plugin) instead:

```bash
# Generate SBOM with full dependency resolution
mvn org.cyclonedx:cyclonedx-maven-plugin:2.9.1:makeBom \
  -DoutputFormat=json \
  -DoutputName=bom \
  -Dcyclonedx.skipNotDeployed=false

# The SBOM is generated at target/bom.json
uzomuzo diet --sbom target/bom.json --source .
```

#### Java (Gradle)

Similarly, use the [CycloneDX Gradle Plugin](https://github.com/CycloneDX/cyclonedx-gradle-plugin):

```groovy
// build.gradle
plugins {
    id 'org.cyclonedx.bom' version '2.2.0'
}
```

```bash
gradle cyclonedxBom
uzomuzo diet --sbom build/reports/bom.json --source .
```

### Basic Usage

```bash
# Table output (default)
uzomuzo diet --sbom bom.json

# With source coupling analysis
uzomuzo diet --sbom bom.json --source .

# JSON output (for CI/LLM consumption)
uzomuzo diet --sbom bom.json --source . --format json

# Detailed per-dependency breakdown
uzomuzo diet --sbom bom.json --source . --format detailed
```

### Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--sbom` | Yes | — | Path to CycloneDX SBOM JSON |
| `--source` | No | `.` | Root directory for source coupling analysis |
| `--format`, `-f` | No | `table` | Output format: `json`, `table`, `detailed` |

## Analysis Pipeline

The diet command runs a 4-phase pipeline:

### Phase 1: Dependency Graph (SBOM)

Parses the CycloneDX SBOM to build a dependency DAG. For each direct dependency, computes:

- **Exclusive transitive count** — dependencies removed *only* if this dep is removed
- **Shared transitive count** — dependencies shared with other direct deps
- **Total transitive count** — all reachable transitive dependencies

### Phase 2: Source Coupling (tree-sitter)

Analyzes your source code to measure how deeply each dependency is integrated:

- **Import file count** — number of files importing the dependency
- **Call site count** — total usage sites across all files
- **API breadth** — number of distinct APIs used from the dependency

Supported languages: Go, Python, JavaScript/TypeScript, Java.

### Phase 3: Health Signals (API)

Reuses the existing `uzomuzo scan` infrastructure to fetch:

- Lifecycle status (Active, Stalled, EOL)
- OpenSSF Scorecard score
- Known vulnerabilities (advisories)

### Phase 4: Scoring

Combines all signals into a priority score:

```
PriorityScore = GraphImpact × HealthRisk × (1 - CouplingEffort)
```

| Score | Range | Meaning |
|-------|-------|---------|
| **GraphImpact** | 0–1 | How much the dependency tree shrinks |
| **HealthRisk** | 0–1 | How risky keeping this dependency is |
| **CouplingEffort** | 0–1 | How hard it is to remove from code |

Difficulty labels:

| Label | CouplingEffort | Meaning |
|-------|---------------|---------|
| trivial | 0.0 | Unused — just delete the import |
| easy | < 0.25 | 1–2 files, few call sites |
| moderate | 0.25–0.59 | Several files, moderate API usage |
| hard | ≥ 0.60 | Deeply integrated |

## Output Examples

### Table

```
── Diet Plan (8 direct dependencies) ─────────────────────────

  Unused (0 imports):  4
  Quick wins:          2  (trivial/easy + high impact)

RANK  PRIORITY  DIFFICULTY  PURL                              ONLY-VIA-THIS  FILES  CALLS  LIFECYCLE
────  ────────  ──────────  ────                              ─────────────  ─────  ─────  ─────────
1     0.48      easy        github.com/joho/godotenv          0              1      1      Active
2     0.40      trivial     github.com/smacker/go-tree-sitter 0              0      0      Active
3     0.08      trivial     gopkg.in/yaml.v3                  0              0      0      Active
...
```

### JSON

```json
{
  "summary": {
    "total_direct": 8,
    "total_transitive": 0,
    "unused_direct": 4,
    "easy_wins": 2,
    "actionable_direct": 6,
    "transitive_only_by_one": 0
  },
  "dependencies": [
    {
      "rank": 1,
      "purl": "pkg:golang/github.com/joho/godotenv@v1.5.1",
      "name": "github.com/joho/godotenv",
      "priority_score": 0.48,
      "difficulty": "easy",
      "transitive_only_by_one": 0,
      "import_file_count": 1,
      "call_site_count": 1,
      "lifecycle": "Active"
    }
  ]
}
```

## Diet Workflow: scan → diet → LLM → remove

The diet family of tools forms a pipeline from detection to removal:

```
uzomuzo scan           "この依存ヤバい"         CI/CD で常時
        ↓
uzomuzo diet           "この順番で消せ"         四半期の棚卸し
        ↓
/diet-assess-risk      "残すリスクはこう"       EOL + hard な依存に
/diet-evaluate-removal "消すコスパはこう"       moderate で迷ったとき
        ↓
/diet-remove           "安全に消す"             実際の除去作業
```

| Tool | Role | Scope | When |
|------|------|-------|------|
| `uzomuzo scan` | **Detect** — find EOL/Stalled deps | All deps, automated | Every CI build |
| `uzomuzo diet` | **Prioritize** — rank by removability | All deps, automated | Quarterly review |
| `/diet-assess-risk` | **Assess risk** — trace data flows, attack scenarios | One dep, LLM-powered | EOL deps with non-trivial coupling |
| `/diet-evaluate-removal` | **Plan removal** — 6-axis evaluation, replacement options | One dep, LLM-powered | When unsure if removal is worth the effort |
| `/diet-remove` | **Execute** — safe removal with verification | One dep, LLM-powered | Actual removal work |

### Typical workflow

```bash
# Step 1: Generate the priority ranking
syft . --source-name myapp -o cyclonedx-json > bom.json
uzomuzo diet --sbom bom.json --source . --format json > diet.json

# Step 2: Trivial dependencies (0 imports) — just remove them
# No LLM needed. Delete from go.mod/package.json and run `go mod tidy`.

# Step 3: EOL/Stalled deps with source coupling — assess risk first
/diet-assess-risk pkg:golang/github.com/foo/bar@v1.0.0

# Step 4: Moderate deps you're unsure about — evaluate removal cost
/diet-evaluate-removal github.com/foo/bar

# Step 5: Execute the removal with safety checks
/diet-remove github.com/foo/bar
```

### JSON output for LLM consumption

```bash
# Feed diet plan to Claude Code for batch replacement suggestions
uzomuzo diet --sbom bom.json --source . --format json > diet.json
claude "Based on this diet plan, suggest code changes to remove the top 3 dependencies: $(cat diet.json)"
```

## Understanding "Unused" Dependencies

Diet reports dependencies as "unused" when no `import` statement is found in source code. However, **not all "unused" dependencies are removable**. There are three common patterns:

### 1. Dev/build dependencies included in SBOM

SBOM tools may include `devDependencies`, test dependencies, and build tools alongside production dependencies. These are genuinely unused in production source code:

- Linters and formatters (`eslint`, `mypy`, `black`)
- Test frameworks (`jest`, `pytest`, `vitest`)
- Documentation tools (`sphinx`, `mkdocs`)
- Build tools (`webpack`, `rollup`, `conventional-changelog-cli`)

**These are often the best candidates for removal from production SBOMs**, as they inflate the dependency tree without contributing to runtime. See [SBOM Tool Comparison](#sbom-tool-comparison) for how different tools handle this.

### 2. Config-driven / runtime-loaded dependencies

Some dependencies are used via configuration files, annotations, or runtime class loading rather than explicit `import` statements:

- **Spring Boot starters** — auto-configured via `spring.factories`, not imported directly
- **JDBC drivers** (`postgresql`, `mysql-connector-j`) — loaded by URL string
- **Cache providers** (`caffeine`) — specified in `application.properties`
- **Template engines** (`thymeleaf`) — resolved by Spring MVC at runtime

These show 0 files / 0 calls in the coupling analysis, which is **expected behavior, not a false positive**. Diet still ranks them correctly: config-driven deps are easy to swap (low coupling) but may bring many transitive deps (high graph impact).

### 3. Leftover dependencies (genuine waste)

Dependencies that were once used but whose `import` was removed without cleaning up `package.json` / `go.mod` / `pom.xml`. **These are the most valuable findings** — they can be removed immediately with zero code changes.

## SBOM Tool Comparison

The quality of diet analysis depends heavily on what the SBOM tool includes. Different tools handle development dependencies very differently:

| Tool | Dev deps included? | Scope metadata? | Notes |
|------|-------------------|-----------------|-------|
| **syft** | **Yes (all)** | No | Includes everything — devDependencies, test deps, build tools. No way to filter. |
| **Trivy** | **No (default)** | No | Excludes dev deps by default. Use `--include-dev-deps` to include them. |
| **cdxgen** | **Yes (all)** | **Yes** (`scope` field) | Includes all deps but marks them as `required`, `optional`, or `excluded`. |
| **CycloneDX Maven Plugin** | Configurable | Yes (`scope` field) | Respects Maven scopes (compile/test/provided/runtime). |

### Real-world impact (Vue.js core)

| Tool | Components | Notes |
|------|-----------|-------|
| syft | 723 | All deps, no scope info |
| Trivy (default) | 34 | Dev deps excluded |
| Trivy (`--include-dev-deps`) | 684 | All deps included |
| cdxgen | 698 | All deps, with `scope` (required: 38, optional: 645) |

### Recommendations

- **For accurate production dependency analysis**: Use Trivy (default mode) or configure CycloneDX Maven/Gradle plugins to exclude test scope
- **For comprehensive diet analysis** (including dev dep cleanup): Use syft or cdxgen to capture everything, then use diet's coupling analysis to distinguish genuinely unused deps from dev tools
- **For the most actionable results**: Run diet twice — once with production-only SBOM (Trivy default) and once with full SBOM (syft) — to see both perspectives

## Supported Languages

| Language | Import Detection | Call Site Counting | Status |
|----------|-----------------|-------------------|--------|
| Go | ✓ | ✓ | v0.1 |
| Python | ✓ | ✓ | v0.1 |
| JavaScript | ✓ | ✓ | v0.1 |
| TypeScript | ✓ | ✓ | v0.1 |
| Java | ✓ | ✓ | v0.1 |
