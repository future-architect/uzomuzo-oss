# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Build the binary (output named "uzomuzo")
go build -o uzomuzo main.go

# Run directly without building
go run . pkg:npm/express@4.18.2
go run . https://github.com/gin-gonic/gin
go run . input_purls.txt 500   # file mode with optional sample size
```

## Testing

```bash
# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/domain/analysis/...

# Run a specific test
go test ./internal/domain/analysis/... -run TestLifecycleAssessor
```

## Code Quality

```bash
# Format imports
goimports -w .

# Lint
golangci-lint run
```

## Subcommands

```bash
# Update embedded SPDX license list (downloads upstream, regenerates spdx_generated.go)
go run . update-spdx
```

## Configuration

Copy `config.template.env` to `.env`. The app auto-loads `.env` via `godotenv`. Key variables:

- `GITHUB_TOKEN` — required for GitHub API (falls back to `gh auth login`)
- `LOG_LEVEL` — `debug|info|warn|error` (default: `info`)
- `LIFECYCLE_ASSESS_TYPE` — `lifecycle` (default)

All settings have documented defaults in `config.template.env`.

## Architecture

The project follows strict DDD (Domain-Driven Design) layering:

```text
Interfaces → Application → Domain ← Infrastructure
```

- **`internal/domain/`** — Pure business logic, no external dependencies. Core types: `Analysis` (aggregate root in `analysis/aggregates.go`), `ResolvedLicense`, `EOLStatus`, `AssessmentResult`. The `AxisResults map[AssessmentAxis]*AssessmentResult` field on `Analysis` is the extensible lifecycle assessment output.

- **`internal/application/`** — Use case orchestration. `AnalysisService` (full eval including EOL) and `FetchService` (data fetch only, no lifecycle assessment). Wired via `NewAnalysisServiceFromConfig` / `NewFetchServiceFromConfig`. Supports `AnalysisEnricher` hook pattern for injecting additional EOL evaluation between Phase 1 (registry) and Phase 3 (lifecycle assessment).

- **`internal/infrastructure/`** — External API clients and I/O:
  - `depsdev/` — deps.dev API client (package metadata, licenses, versions, security advisories)
  - `github/` — GitHub GraphQL/REST client (repo metadata, commits, scorecard scores)
  - `eolevaluator/` — Evaluates EOL status from registry heuristics (PyPI classifiers, Packagist abandoned, NuGet deprecated, Maven relocated, npm deprecated)
  - `integration/` — `IntegrationService` orchestrates concurrent fetching from multiple sources
  - `export/csv/` — CSV output

- **`internal/interfaces/cli/`** — CLI entry points (`ProcessDirectMode`, `ProcessFileMode`, subcommand handlers). No concurrent logic here.

- **`pkg/uzomuzo/`** — Public Go library facade. `NewEvaluator(githubToken)` → `EvaluatePURLs` / `EvaluateGitHubRepos` / `ExportCSV`.

## Extensibility: AnalysisEnricher Hook

The `AnalysisService` supports an enricher hook pattern for injecting custom EOL evaluation logic:

```go
type AnalysisEnricher func(ctx context.Context, analyses map[string]*domain.Analysis) error

// Inject via functional options:
svc := application.NewAnalysisServiceFromConfig(cfg,
    application.WithEnricher(myCustomEnricher),
)
```

This enables downstream projects to add proprietary or community-maintained EOL catalog data without modifying the core codebase.

## PURL Identity Model

Three distinct PURL fields on `Analysis`:
- `OriginalPURL` — exact caller-supplied identifier, never rewritten
- `EffectivePURL` — resolved/normalized form used for API calls (may add version, expand coordinates)
- `CanonicalKey` — lowercase versionless key for internal dedup/maps (computed by `purl.CanonicalKey`)

## Generated Files

`internal/domain/licenses/spdx_generated.go` is auto-generated — never edit manually. Regenerate with `go run . update-spdx` or `go generate ./internal/domain/licenses`.

## Language Policy

See `.claude/rules/language-policy.md` for full details. Key points:

- **Source code**: English only (identifiers, comments, error messages, CLI output)
- **Documentation**: English (`README.md`, `docs/*.md`)

## Coding Standards

See `.claude/rules/coding-standards.md` (error handling, testing, performance) and `.claude/rules/project-conventions.md` (test data, config policy, tooling constraints) for full details.

## Key Documentation (docs/)

- [data-flow.md](docs/data-flow.md) — overall data flow and component interactions
- [development.md](docs/development.md) — development workflow and conventions
- [library-usage.md](docs/library-usage.md) — public library (`pkg/uzomuzo/`) usage
- [purl-identity-model.md](docs/purl-identity-model.md) — detailed PURL identity model
- [license-resolution.md](docs/license-resolution.md) — license resolution logic
