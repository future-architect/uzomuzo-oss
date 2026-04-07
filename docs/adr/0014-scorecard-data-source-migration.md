# ADR-0014: Scorecard Data Source Migration — deps.dev to scorecard.dev API

## Status

Accepted

## Context

Build Integrity grading (ADR-0013) and Lifecycle assessment both consume OpenSSF Scorecard data. The data was previously sourced from the deps.dev Project API, which returns only **14 of 18** Scorecard checks.

### Missing checks from deps.dev

| Check | Impact |
|-------|--------|
| Vulnerabilities | Lifecycle assessor reads this check; its absence forces the "partial scorecard" code path |
| CI-Tests | Build integrity signal for CI/CD pipeline detection |
| Contributors | Community health indicator |
| Dependency-Update-Tool | Build integrity signal for automated dependency management |

The scorecard.dev API (`https://api.scorecard.dev/projects/{host}/{owner}/{repo}`) returns **all 18 checks** with comparable freshness (~6 days vs ~9 days for deps.dev).

### Key design questions

1. Should we replace deps.dev scorecard or supplement it?
2. How to handle scorecard.dev unavailability?
3. Where to place the new API client in DDD layers?
4. How to avoid increased latency?

## Decision

### Enrichment-with-fallback pattern

deps.dev continues to provide baseline scorecard data (14 checks) during its existing batch fetch. A new parallel enrichment step fetches from scorecard.dev and **overwrites** the deps.dev data when the scorecard.dev response contains at least as many checks. If scorecard.dev is unavailable, the deps.dev data remains untouched.

### Architecture

- **Infrastructure layer**: New `internal/infrastructure/scorecard/` package containing the HTTP client
- **Infrastructure layer**: `enrichScorecardFromAPI()` in the integration package follows the established enrichment pattern (`enrichAdvisorySeverity`, `enrichDependentCounts`)
- **Domain layer**: No changes — reuses existing `ScoreEntity` and `Analysis.Scores`
- **Config layer**: New `ScorecardConfig` with env var overrides (`SCORECARD_BASE_URL`, etc.)

### Parallel execution

The scorecard.dev fetch runs as a third goroutine in the existing parallel enrichment block alongside `enrichDependentCounts` and `enrichDependencyCounts`. This adds zero wall-clock latency for the common case.

### Safety guards

- **Check count guard**: Only overwrite if `len(scorecardResult.Scores) >= len(existingScores)` to prevent stale/partial scorecard.dev responses from degrading data quality
- **Graceful nil**: When `scorecardClient` is nil, enrichment is a no-op
- **Per-project error isolation**: Individual fetch failures are logged at DEBUG and skipped

## Consequences

### Positive

- All 18 Scorecard checks available in `Analysis.Scores`
- `Vulnerabilities` check now populates the lifecycle assessor's vulnerability path
- Build integrity signals gain access to `CI-Tests`, `Contributors`, and `Dependency-Update-Tool`
- No latency increase (parallel fetch)
- No regression when scorecard.dev is down (graceful fallback)

### Negative

- Additional external API dependency (scorecard.dev)
- Slightly higher network usage (one extra HTTP request per unique GitHub repository)
- Two data sources for the same conceptual data may cause confusion if they diverge significantly

### Neutral

- deps.dev remains the source for all non-scorecard data (versions, advisories, dependencies, PURL resolution)
- CSV export dynamically discovers check columns, so the 4 new checks appear automatically
