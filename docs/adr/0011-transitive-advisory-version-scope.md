# ADR-0011: Transitive Advisory Version Scope

## Status

Accepted

## Context

uzomuzo fetches transitive dependency advisories by querying the deps.dev dependency graph for a specific package version, then collecting advisories from each transitive dependency node.

When a user scans a versioned PURL (e.g., `pkg:npm/express@4.18.2`), the dependency graph is fetched for that specific version. However, the Releases section displays multiple version lines:

- **StableVersion**: latest stable release (e.g., `5.2.1`)
- **MaxSemverVersion**: highest semver (often same as stable)
- **RequestedVersion**: the version the user asked about (e.g., `4.18.2`)

The question: which VersionDetail should receive transitive advisories from the fetched dependency graph?

## Decision

**Transitive advisories are attached only to the VersionDetail whose version matches the dependency graph's SELF node version.** In practice, this means:

- `pkg:npm/request@2.88.2` → Stable is `2.88.2`, graph is `2.88.2` → advisories shown on Stable (match)
- `pkg:npm/express@4.18.2` → Stable is `5.2.1`, graph is `4.18.2` → advisories **not shown** (no match)
- `pkg:npm/express` (no version) → graph fetched for Stable version → advisories shown on Stable (match)

Transitive advisories are **never** attached to RequestedVersion, even if the graph version matches, because:

1. **uzomuzo is an OSS health assessment tool, not a vulnerability scanner.** Its purpose is to evaluate the current health of a package (latest release), not audit a specific old version. Users who need version-specific vulnerability scanning should use dedicated SCA tools (Trivy, Snyk, etc.).

2. **Showing old-version vulnerabilities on RequestedVersion is misleading.** The advisories reflect the transitive dependency tree of an older version that may have been fixed in the current release. Displaying them implies the package currently has these issues, when in fact the maintainers may have already resolved them.

3. **The dependency graph is a side effect of dependency counting, not a dedicated vulnerability scan.** It is fetched once for `enrichDependencyCounts` and reused opportunistically. Fetching additional graphs for RequestedVersion would double API calls without aligning with uzomuzo's purpose.

## Consequences

- When `RequestedVersion != StableVersion`, no transitive advisories are displayed. This is intentional — the user sees the Stable version's advisory status (the package's current health) without stale data from an older version.
- The `Depends on: N direct, M transitive` count still reflects the requested version's dependency graph (since that data was already fetched for counting). This minor inconsistency is acceptable because dependency counts are structural metadata, not security assessments.
- Future work could add a `--audit` mode that explicitly scans the requested version's transitive tree, clearly separated from the health assessment output.
