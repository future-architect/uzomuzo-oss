# ADR-0005: GitHub Actions Health Scanning Architecture

## Status

Accepted (2026-04-03)

## Context

uzomuzo evaluates lifecycle health of software dependencies identified by PURLs or GitHub URLs. However, GitHub Actions — a critical part of the CI/CD supply chain — were invisible to this analysis. A repository could have all direct dependencies healthy while relying on unmaintained or EOL Actions in its workflows.

Two approaches were considered for adding Actions scanning:

1. **Treat Actions as dependencies**: Make the workflow parser implement `DependencyParser`, producing `ParsedDependency` values with PURLs. This would integrate seamlessly with the existing batch pipeline.
2. **Standalone discovery, route as GitHub URLs**: Parse workflow YAML into GitHub URLs (`https://github.com/owner/repo`) and feed them through the existing `RunFromPURLs` pipeline separately.

The key tension was that GitHub Actions do not have a native PURL ecosystem. Actions are identified by `owner/repo@ref` in workflow YAML, and deps.dev often misresolves them (e.g., `actions/checkout` maps to the npm package `checkout`). Forcing Actions into the PURL-centric `ParsedDependency` model would pollute the domain type with non-PURL entities.

## Decision

### Phase 1 (PR #93): Standalone workflow file scanning

Add `--file .github/workflows/ci.yml` support via a standalone `ghaworkflow` infrastructure parser that:

- Extracts `owner/repo` from `uses:` directives (standard, monorepo, reusable workflow patterns)
- Skips `docker://` and `./local` references
- Outputs GitHub URLs (not PURLs), routed directly to `RunFromPURLs(nil, githubURLs)`
- Does **not** implement `DependencyParser` — preserving domain type purity

### Phase 2 (PR #101): Repository-level Actions discovery

Add `--include-actions` flag to `scan` that discovers Actions from a target repository's workflows:

- New `actionscan.DiscoveryService` in infrastructure layer fetches `.github/workflows/*.yml` via GitHub Contents API
- New `ActionsDiscoverer` interface in application layer decouples CLI from infrastructure
- New `EntrySource` domain type (`"direct"` / `"actions"`) distinguishes user inputs from discovered Actions
- New `RunFromPURLsWithActions` method (not modifying existing `RunFromPURLs`) avoids changing 4+ call sites
- Renderers show Actions results in a separate `--- GitHub Actions ---` section
- Opt-in (default off) because discovery multiplies API calls (1 Contents API call per workflow file + 1 analysis per Action)

### Round-trip validation (PR #100)

Added validation to detect and skip deps.dev misresolutions where a GitHub URL resolves to an unrelated package (e.g., `actions/checkout` → npm `checkout`). This is critical for Actions scanning because most Actions do not have corresponding registry packages.

## Consequences

### Positive

- **Supply chain visibility**: Actions are now first-class scan targets, closing a blind spot in lifecycle analysis.
- **Domain purity preserved**: `ParsedDependency` remains PURL-centric. Actions flow through the system as GitHub URLs.
- **Incremental adoption**: `--include-actions` is opt-in, so existing workflows are unaffected.
- **Composable**: `--include-actions` works with `--fail-on`, enabling CI gates on Actions health.

### Negative

- **No PURL ecosystem for Actions**: Actions without a corresponding registry package show "Review Needed" because deps.dev cannot resolve them. This is a fundamental limitation of the current PURL-based analysis model, not a design flaw.
- **Additional API calls**: `--include-actions` requires Contents API calls to fetch workflow files, increasing rate limit consumption. Mitigated by being opt-in.
- **Depth=1 only**: Phase 1-2 only discover direct Action references, not composite actions that themselves reference other Actions. Phase 3 (`--depth N`) is planned.

### Neutral

- ADR-0004 (scan subcommand unification) is extended — `scan` now accepts `--include-actions` alongside existing input modes.
- The `ghaworkflow` parser from Phase 1 is reused internally by Phase 2's `DiscoveryService`.
