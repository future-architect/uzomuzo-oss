# Library Usage (Programmatic Usage)

[← Back to README.md](../README.md)

## Installation

```bash
go get github.com/future-architect/uzomuzo
```

## Basic Usage

The library is built around a single high-level `Evaluator` that performs the following in one call:

1. Data collection (deps.dev, GitHub, registry metadata)
2. Lifecycle heuristics (activity / release freshness)
3. Primary source EOL integration (registry deprecation signals / successor detection)

```go
package main

import (
  "context"
  "fmt"
  "os"

  "github.com/future-architect/uzomuzo/pkg/uzomuzo"
)

func main() {
  ctx := context.Background()

  // Create full evaluator (GitHub token strongly recommended for commit-based assessment precision)
  client := uzomuzo.NewEvaluator(os.Getenv("GITHUB_TOKEN"))

  // Full evaluation (includes lifecycle heuristics + EOL / successor integration)
  analyses, err := client.EvaluatePURLs(ctx, []string{
    "pkg:npm/express@4.18.2",
    "pkg:pypi/requests@2.28.1",
  })
  if err != nil {
    panic(err)
  }

  for purl, a := range analyses {
    if a.Error != nil {
      fmt.Printf("%s: error: %v\n", purl, a.Error)
      continue
    }

    fmt.Printf("\nPackage: %s\n", a.DisplayPURL())
    fmt.Printf("Overall Score: %.1f/10  SecPolicy: %.1f  Vulns: %.1f\n",
      a.OverallScore,
      a.GetScore("Security-Policy"),
      a.GetScore("Vulnerabilities"),
    )
    stars, forks := 0, 0
    if a.Repository != nil { stars, forks = a.Repository.StarsCount, a.Repository.ForksCount }
    fmt.Printf("Repo: archived=%t disabled=%t stars=%d forks=%d\n", a.IsArchived(), a.IsDisabled(), stars, forks)

    // Lifecycle axis (optional)
    if lr := a.GetLifecycleResult(); lr != nil {
      fmt.Printf("Lifecycle: %s (%s)\n", lr.Label, lr.Reason)
    }

    // Project-level license (single ResolvedLicense)
    proj := a.ProjectLicense
    switch {
    case proj.IsZero():
      fmt.Println("Project License: (absent)")
    case proj.IsNonStandard():
      fmt.Printf("Project License: non-standard raw=%q source=%s\n", proj.Raw, proj.Source)
    default:
      fmt.Printf("Project License: SPDX=%s raw=%q source=%s\n", proj.Identifier, proj.Raw, proj.Source)
    }

    // Simple health rule (customize freely in your app)
    healthy := a.OverallScore >= 7.0 && a.GetScore("Security-Policy") >= 8.0 && !a.IsArchived()
    fmt.Printf("Healthy? %t\n", healthy)
  }
}
```

Key points of the example above:

1. Integrates fetch (licenses, repo metadata, scorecard) with lifecycle evaluation
2. Uses intention helpers for license inspection instead of branching on `Source` constants directly
3. Deterministic fallback / promotion is already applied within the returned `Analysis` object
4. Callers define their own "health" criteria — the library provides raw signals & lifecycle axis but no opinionated judgments

## Unified Final Reason (Library Usage)

When embedding the library, use the stable helpers:

```go
finalEn := analysis.EOL.FinalReason()   // English
finalJa := analysis.EOL.FinalReasonJa() // Japanese (if available); falls back to English
```

Selection priority within `FinalReason()`:

1. Evidence with highest Confidence (ties: first)
2. First evidence (fallback)
3. Empty string (no evidence/reason available)

Centralizing this logic keeps UI / export layers deterministic. Avoid re-implementing the ordering on the client side.

## Key Methods (Public API)

- `NewEvaluator(token, opts...)` — Build a full evaluation client (accepts `WithEnricher` options). Token enables commit history and Scorecard data for higher assessment precision (see [Assessment Precision](../README.md#assessment-precision-by-data-availability))
- `EvaluatePURLs(ctx, purls)` — Full PURL evaluation (score + lifecycle + EOL)
- `EvaluateGitHubRepos(ctx, urls)` — Full evaluation for GitHub repositories
- `ExportCSV(results, path)` — Export full metrics as CSV

### Standalone Fetch Helpers

Thin wrappers for fetching raw data without running the full evaluation pipeline:

- `FetchGitHubREADME(ctx, owner, repo, defaultBranch)` — Fetch raw README text from a GitHub repository (tries README.md, README.MD, README, README.txt, README.rst)
- `FetchPyPIProject(ctx, name)` — Fetch project metadata from PyPI JSON API (returns `*PyPIProjectInfo`)
- `IsGitHubURL(rawURL)` — Check whether a URL points to a GitHub repository
- `ParseGitHubURL(rawURL)` — Extract owner and repo from a GitHub URL

**Note**: `ResolvedLicense` (used by `Analysis.ProjectLicense` and `Analysis.RequestedVersionLicenses`) is defined in `internal/domain/analysis` and is not re-exported from `pkg/uzomuzo`. You can call its methods (e.g., `IsZero()`, `IsNonStandard()`) through the field, but cannot reference the type directly in your own signatures.

## Analysis Type: Key Fields / Methods

The `Analysis` type provides rich domain methods:

- `OverallScore` — Overall scorecard score (0-10)
- `GetScore(name)` — Specific check score (-1 if not found)
- `GetCheckMap()` — Map of all scores (name → value)
- `IsArchived()` / `IsDisabled()` — Repository status
- `AxisResults` — Assessment axis → result map (lifecycle axis key: `"lifecycle"`); use `GetLifecycleResult()` helper if only lifecycle is needed
- `HasError()` / `GetErrorMessage()` — Error handling
- Direct field access to all analysis data

Users should define their own health/risk criteria rather than relying on opinionated convenience methods.

## Switching Assessor Implementations

Currently only the lifecycle assessment axis is provided. Additional axes (e.g., Build Health) can be added by implementing `AssessmentService` and composing with `NewCompositeAssessor`. Place business logic in the domain layer and inject thresholds via `LifecycleAssessmentConfig` to maintain DDD compliance.

## Custom EOL Catalog via AnalysisEnricher

uzomuzo provides built-in EOL detection from registry signals (PyPI classifiers, npm deprecated, Packagist abandoned, NuGet deprecated, Maven relocated). For **comprehensive EOL coverage**, you can inject your own EOL catalog enricher using the `WithEnricher` hook.

### Architecture

The evaluation pipeline runs in three phases:

1. **Phase 1** — Registry heuristics (built-in, automatic)
2. **Phase 2** — Enrichers (your custom catalog logic runs here)
3. **Phase 3** — Lifecycle assessment (built-in, uses Phase 1+2 results)

Enrichers run between Phase 1 and Phase 3, so catalog-based EOL decisions feed into the final lifecycle label.

### Catalog JSON Format (Example)

Your EOL catalog can use any format you prefer. Below is a recommended structure per ecosystem shard:

```json
{
  "schema_version": 2,
  "generated_at": "2025-06-01T00:00:00Z",
  "records": [
    {
      "purl": "pkg:npm/left-pad",
      "judgments": {
        "human": {
          "status": "eol",
          "reason": "Unpublished from npm in 2016 and abandoned. No updates since.",
          "successor": "pkg:npm/pad-left",
          "confidence": "high"
        }
      }
    },
    {
      "purl": "pkg:pypi/some-future-lib",
      "judgments": {
        "human": {
          "status": "scheduled",
          "eol_date": "2026-12-31",
          "reason": "Official deprecation announcement. Migrate to v2.",
          "successor": "pkg:pypi/some-future-lib-v2",
          "confidence": "high"
        }
      }
    },
    {
      "purl": "pkg:maven/junit/junit",
      "judgments": {
        "human": {
          "status": "not_eol",
          "reason": "Still maintained. JUnit 4 receives critical fixes.",
          "confidence": "medium"
        }
      }
    }
  ]
}
```

Field reference:

| Field | Required | Values |
| ----- | -------- | ------ |
| `purl` | yes | Versionless PURL (e.g., `pkg:npm/express`) |
| `judgments.human.status` | yes | `eol`, `not_eol`, `scheduled`, `pending`, `unknown` |
| `judgments.human.eol_date` | for scheduled | `YYYY-MM-DD` format |
| `judgments.human.reason` | recommended | Human-readable rationale |
| `judgments.human.successor` | optional | Successor PURL or URL |
| `judgments.human.confidence` | optional | `high`, `medium`, `low` |

The enricher you write is responsible for loading and matching this JSON — uzomuzo does not prescribe the loader implementation.

### Basic Enricher Setup

```go
package main

import (
  "context"
  "fmt"
  "os"

  "github.com/future-architect/uzomuzo/pkg/uzomuzo"
)

func main() {
  ctx := context.Background()

  evaluator := uzomuzo.NewEvaluator(
    os.Getenv("GITHUB_TOKEN"),
    uzomuzo.WithEnricher(myCatalogEnricher),
  )

  results, err := evaluator.EvaluatePURLs(ctx, []string{"pkg:npm/left-pad@1.0.0"})
  if err != nil {
    panic(err)
  }
  for purl, a := range results {
    fmt.Printf("%s: lifecycle=%s eol=%s\n", purl, a.FinalLifecycleLabel(), a.EOL.HumanState())
  }
}
```

### Writing an Enricher

An `AnalysisEnricher` receives the full analysis map after Phase 1. It may set or override `Analysis.EOL` fields:

```go
func myCatalogEnricher(ctx context.Context, analyses map[string]*uzomuzo.Analysis) error {
  for key, a := range analyses {
    if a == nil {
      continue
    }

    // Look up PURL in your catalog (implement your own loader/matcher)
    match := myCatalog.Lookup(a.EffectivePURL)
    if match == nil {
      continue
    }

    switch {
    case match.IsEOL:
      a.EOL = uzomuzo.EOLStatus{
        State:     uzomuzo.EOLEndOfLife,
        Successor: match.Successor,
        Reason:    match.Reason,
        Evidences: []uzomuzo.EOLEvidence{{
          Source:     "MyCatalog",
          Summary:    match.Reason,
          Reference:  match.ReferenceURL,
          Confidence: 0.95,
        }},
      }

    case match.IsScheduled:
      scheduledAt := match.EOLDate
      a.EOL = uzomuzo.EOLStatus{
        State:       uzomuzo.EOLScheduled,
        ScheduledAt: &scheduledAt,
        Successor:   match.Successor,
        Reason:      match.Reason,
      }

    case match.IsNotEOL:
      // Explicitly mark as not-EOL (overrides registry heuristic false positives)
      a.EOL = uzomuzo.EOLStatus{
        State:  uzomuzo.EOLNotEOL,
        Reason: match.Reason,
      }
    }
  }
  return nil
}
```

### Enricher Contract

- **MAY** set or override `Analysis.EOL` (State, Successor, ScheduledAt, Reason, ReasonJa, Evidences)
- **MAY** clear `Analysis.Error` (e.g., when catalog knows about a package that deps.dev doesn't)
- **MUST NOT** modify other aggregate fields (scores, repository metadata, etc.)
- Multiple enrichers run in the order they are registered; later enrichers can override earlier ones

### Available EOL Types and Constants

All types are re-exported via `pkg/uzomuzo`:

| Type | Description |
| ---- | ----------- |
| `uzomuzo.EOLStatus` | Aggregates EOL decision + evidence |
| `uzomuzo.EOLState` | Enum: `EOLUnknown`, `EOLNotEOL`, `EOLEndOfLife`, `EOLScheduled` |
| `uzomuzo.EOLEvidence` | Single evidence item (Source, Summary, Reference, Confidence) |

### How EOL Flows into Lifecycle Labels

After enrichers run, Phase 3 converts `EOLStatus` into the final lifecycle label:

| EOL State | Lifecycle Label |
| --------- | --------------- |
| `EOLEndOfLife` | `EOL Confirmed` |
| `EOLScheduled` | `EOL Scheduled` |
| `EOLNotEOL` | (determined by activity heuristics) |
| `EOLUnknown` | (determined by activity heuristics) |

Access the final label via `a.FinalLifecycleLabel()` or the richer `uzomuzo.BuildLifecycleSummary(a)`.

## Sample Code

See `examples/eol_detection` for a runnable demo of the full evaluation pipeline (scores, lifecycle labels, EOL evidences, successor info):

```bash
go run ./examples/eol_detection
```
