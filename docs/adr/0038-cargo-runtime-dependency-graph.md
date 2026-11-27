# ADR-0038: Scope cargo dependency graphs to default features

Date: 2026-11-27
Status: Accepted

## Context

deps.dev's cargo `:dependencies` graph includes dev dependencies and optional dependencies that a consumer would not build by default. This inflates dependency counts and transitive advisory results (issue #608). This decision relates to ADR-0034, which hides cargo dependency counts in output.

## Decision

For cargo PURLs, fetch the matching version's `:requirements` response and retain declared normal and build dependencies. An optional dependency is retained only when the crate's `default` feature enables it, including through nested features, explicit `dep:x`, implicit dependency names, and `x/feat`; weak `x?/feat` does not enable it. Follow the retained direct dependencies through the graph to keep their transitive nodes. This behavior is on by default with no flag. If requirements are unavailable or cannot be matched to the graph, keep the full graph so advisories are not hidden by an uncertain reduction.

## Limits

The root crate's requirements cannot identify inactive optional dependencies of indirect crates, so those cannot be pruned. Target-specific dependencies remain in scope. Cargo counts remain hidden as decided in ADR-0034; showing them again requires a separate decision.
