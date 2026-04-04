# ADR-0010: Limit Advisory Severity Enrichment to Lifecycle-Relevant Versions

## Status

Accepted (2026-04-04)

## Context

`enrichAdvisorySeverity` fetches CVSS3 scores from the deps.dev advisory API and populates `Title`, `CVSS3Score`, and `Severity` on each `Advisory` struct. This data drives two consumers:

1. **Lifecycle assessor** (`getStableOrMaxVersionDetail`) — determines whether high-severity advisories should escalate toward a "Replace" verdict
2. **CLI output** (JSON/CSV `advisory_count`, `max_advisory_severity`, `max_cvss3_score`) — shows advisory risk summary to the user

### Prior behavior

The initial implementation enriched advisories on **all four** version slots:

- `StableVersion`
- `MaxSemverVersion`
- `PreReleaseVersion`
- `RequestedVersion`

### Problem

- The lifecycle assessor only inspects `StableVersion` and `MaxSemverVersion` (via `getStableOrMaxVersionDetail`). PreRelease and RequestedVersion are never used for lifecycle classification.
- CLI output uses `LatestVersionDetail()` which resolves `Stable > MaxSemver > PreRelease > Requested`. In practice, if Stable or MaxSemver exists, PreRelease/RequestedVersion are never reached.
- The Detailed format (`printReleaseInfo`) only displays advisories from `StableVersion`.
- Enriching unused versions wastes API calls — each advisory ID requires a separate HTTP request to deps.dev. For large batches (1000+ PURLs) with many advisories per package, the unnecessary calls can be significant.

### Scope distinction

uzomuzo's purpose is **OSS health assessment** (repository-level lifecycle signals), not Software Composition Analysis (SCA). Advisory data on the latest version tells users "is this project managing its security posture?". Advisory data on a specific requested version answers "am I currently exposed?" — a fundamentally different question that belongs to SCA tooling (e.g., Trivy, Grype, OSV-Scanner).

## Decision

Limit `enrichAdvisorySeverity` to **StableVersion** and **MaxSemverVersion** only.

### Rationale

1. **Alignment with consumers**: Both the lifecycle assessor and the primary CLI output path only inspect Stable/MaxSemver. Enriching PreRelease and RequestedVersion has no observable effect.
2. **API efficiency**: Reducing the set of advisory IDs to fetch decreases HTTP round-trips proportionally.
3. **Scope clarity**: Advisory severity on the latest version is an OSS health signal. Per-version vulnerability assessment is out of scope for uzomuzo.

## Consequences

### Positive

- Fewer API calls per batch run (proportional to the number of PreRelease/RequestedVersion-only advisories that were previously fetched)
- Clearer code intent — the enrichment scope matches the consumption scope
- No user-visible output changes (no current code path renders PreRelease/RequestedVersion advisory severity)

### Negative

- If a future feature needs severity data on RequestedVersion (e.g., "am I using a vulnerable version?"), the enrichment scope must be explicitly expanded. This is intentional — such a feature should be a deliberate decision with its own ADR, not an accidental side effect of over-fetching.

### Neutral

- `LatestVersionDetail()` still falls back to PreRelease/RequestedVersion when Stable and MaxSemver are both nil. In that edge case, advisory severity will be unenriched (zero values). The lifecycle assessor's existing count-based fallback handles this gracefully.
