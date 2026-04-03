# ADR-0007: Skip transitive Actions scanning (Phase 2 of #98)

## Status

Superseded by [ADR-0008](0008-transitive-composite-action-scanning.md) (2026-04-03)

## Context

Issue #98 defines three phases for GitHub Actions health scanning:

- **Phase 1** (completed, PR #101): Scan Actions directly referenced in a repository's `.github/workflows/*.yml` files via `--include-actions`.
- **Phase 2** (this decision): For each discovered Action, fetch its `action.yml`. If it is a Composite Action (`runs.using: composite`), recursively discover nested Action references up to `--depth N` levels.
- **Phase 3**: Scan dependency manifests (go.mod, package.json) of each Action repository for package lifecycle health.

Phase 2 was designed, and architecture/planning agents produced a full implementation plan. During review, we evaluated whether the feature provides sufficient value.

Note: ADR-0005 mentions "Phase 3 (`--depth N`) is planned" in its Consequences section. This ADR supersedes that statement — `--depth N` will not be implemented.

### Arguments for Phase 2

- **Security surface exists**: Composite Actions execute their nested `uses:` references on the caller's runner. A compromised transitive Action can access `GITHUB_TOKEN`, secrets (with `secrets: inherit`), and source code.
- **Abandoned transitive Actions are an indirect risk signal**: A stalled Action deep in the chain is more likely to be compromised or contain unpatched vulnerabilities.

### Arguments against Phase 2

1. **Composite Actions are rare**: The majority of GitHub Actions use `runs.using: node20` or `docker`. Composite Actions that reference other Actions via `steps[].uses` are a small minority. Deep chains (depth >= 2) are extremely rare in practice.
2. **Not actionable by the user**: If a transitive Action (depth >= 2) is flagged as stalled, the user cannot fix it. They would need to ask the Action author to update their internal dependencies. Information without a remediation path is noise.
3. **Responsibility boundary**: Transitive Action dependencies are the Action author's responsibility, not the consuming repository's. This is analogous to transitive Go module dependencies — the direct dependency author manages their own `go.mod`.
4. **uzomuzo's scope is lifecycle health, not compromise detection**: uzomuzo detects abandoned/EOL/stalled status. Detecting actual supply chain compromise is the domain of tools like OpenSSF Scorecard, StepSecurity Harden-Runner, and GitHub's own dependency graph. A "stalled" label on a transitive Action is a weak signal compared to these specialized tools.
5. **API cost is disproportionate**: Each depth level multiplies GitHub API calls (1-2 per Action for `action.yml`/`action.yaml` fetching). The information gained does not justify the API budget and rate limit consumption.

## Decision

**Skip Phase 2.** Do not implement transitive Actions scanning (`--depth N`).

- The `--depth` CLI flag will not be added.
- `--include-actions` continues to scan only direct Action references from workflow files (depth=1 behavior).
- Phase 3 (Action dependency package scanning) remains a separate consideration and is not blocked by this decision.

## Consequences

### Positive

- Simpler codebase: no `action.yml` parser, no recursive discovery logic, no cycle detection.
- No additional API rate limit pressure from recursive fetching.
- Users see only actionable results (Actions they directly chose to use).

### Negative

- A repository using a healthy-looking Action that internally depends on an abandoned Composite Action will not receive a warning from uzomuzo.
- If Composite Action adoption grows significantly in the future, this decision may need revisiting.

### Revisit criteria

Reconsider this decision if:

- Composite Action usage becomes mainstream (e.g., > 30% of popular Actions).
- A real-world supply chain incident traces through transitive Composite Action dependencies.
- GitHub provides an API for Action dependency graphs, eliminating the API cost concern.
