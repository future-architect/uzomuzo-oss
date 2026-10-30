# ADR-0034: Report dependency counts with the version they describe

Date: 2026-10-30
Status: Accepted

## Context

`--format detailed` prints `Depends on: N direct, M transitive` from the deps.dev `:dependencies` graph, but `--format json` has no equivalent field. Agents that read only JSON to choose between candidate packages therefore had no measured dependency footprint and fell back to recalling it from memory.

The counts are not equally meaningful across ecosystems. For npm, Maven, and PyPI the deps.dev graph describes what installing the version pulls in (Maven: compile and runtime scopes, which matched the published POMs of jackson-databind and guava; PyPI includes indirect nodes, for example `fastapi@0.116.1` has 3 direct and 5 indirect). For cargo the graph also contains dev-dependencies and optional dependencies: `serde_json@1.0.140` has 13 direct nodes while its required runtime dependencies number 4. The node list carries no dependency kind, so the graph alone cannot separate them.

The version that produced the graph was discarded after enrichment, and the scorecard CSV printed `0` both for a measured zero and for an unknown count.

## Decision

Store the version the graph describes (its SELF node) on `Analysis.DependencyGraphVersion` and gate every rendering of the counts on `Analysis.HasRuntimeDependencyCounts()`. It is true only when a resolved graph was returned, that version is known, and the ecosystem is not cargo. The SELF version is used rather than the requested one because the 404 version fallback can count a different release. Batch results are keyed by the exact queried PURL so that two versions of one package keep separate graphs.

- JSON adds `dependencies: {source, version, direct, transitive}`, shaped like `dependents` (ADR-0033). It is present only when the predicate holds; a measured zero is printed as `0`. No flat count field is added, so absence always means "not reported".
- The detailed `Depends on:` line uses the same predicate, so cargo no longer prints it.
- The scorecard CSV leaves `directDeps` and `transitiveDeps` blank when the predicate is false and appends a `dependencies_version` column. This changes those two columns from `0` to blank for unknown counts and for every cargo row; readers should treat blank as unknown.
- `DirectDepsCount`, `TransitiveDepsCount`, and `HasDependencyGraph` keep their raw values for library callers.

cargo is hidden rather than labelled because a larger number next to an explanatory note is still compared as a number. The cargo graph keeps feeding transitive advisory enrichment unchanged; restricting that input is left to a separate change.

## Limits

Counts describe the queried version's graph as resolved by deps.dev, not the consumer's lockfile. They are evidence for a reader and do not change any verdict or lifecycle rule. Reporting cargo requires a graph restricted to normal dependencies (for example via the `:requirements` endpoint's dependency kinds), which is a separate change; when it lands, a `scope` field can be added alongside cargo.
