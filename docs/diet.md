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

Generate a CycloneDX SBOM for your project:

```bash
# Using syft
syft . -o cyclonedx-json > bom.json

# Using trivy
trivy fs . --format cyclonedx -o bom.json

# Using cdxgen
cdxgen -o bom.json
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

  Unused direct deps:  4
  Easy wins:           2
  Estimated removable: 6

RANK  PRIORITY  DIFFICULTY  PURL                              EXCLUSIVE  FILES  CALLS  LIFECYCLE
────  ────────  ──────────  ────                              ─────────  ─────  ─────  ─────────
1     0.48      easy        github.com/joho/godotenv          0          1      1      Active
2     0.40      trivial     github.com/smacker/go-tree-sitter 0          0      0      Active
3     0.08      trivial     gopkg.in/yaml.v3                  0          0      0      Active
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
    "estimated_removable": 6
  },
  "dependencies": [
    {
      "rank": 1,
      "purl": "pkg:golang/github.com/joho/godotenv@v1.5.1",
      "name": "github.com/joho/godotenv",
      "priority_score": 0.48,
      "difficulty": "easy",
      "exclusive_transitive": 0,
      "import_file_count": 1,
      "call_site_count": 1,
      "lifecycle": "Active"
    }
  ]
}
```

## Integration with LLMs

The JSON output is designed for LLM consumption. Feed it to Claude Code or similar tools to get replacement code suggestions:

```bash
# Generate diet plan
uzomuzo diet --sbom bom.json --source . --format json > diet.json

# Feed to Claude Code for replacement suggestions
claude "Based on this diet plan, suggest code changes to remove the top 3 dependencies: $(cat diet.json)"
```

## Supported Languages

| Language | Import Detection | Call Site Counting | Status |
|----------|-----------------|-------------------|--------|
| Go | ✓ | ✓ | v0.1 |
| Python | ✓ | ✓ | v0.1 |
| JavaScript | ✓ | ✓ | v0.1 |
| TypeScript | ✓ | ✓ | v0.1 |
| Java | ✓ | ✓ | v0.1 |
