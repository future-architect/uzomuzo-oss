# ADR-0036: Confirm withdrawn advisories before removal

Date: 2026-11-14
Status: Accepted

## Context

deps.dev can keep an advisory key on a version or dependency after its detail endpoint returns 404. A missing detail alone does not establish that the advisory was withdrawn: the endpoint can also fail temporarily. Keeping every such key gives it unknown severity, which can affect EOL-Effective, Stalled, Legacy-Safe, counts, and output. Issue #593 identifies a withdrawn OSV advisory in this state.

ADR-0010 limits direct severity enrichment to Stable and MaxSemver. ADR-0011 attaches dependency advisories to the version described by the dependency graph. ADR-0032 obtains OSV publication and reviewed severity facts only for dependency advisories with a known HIGH CVSS3 score. Those decisions remain in force.

## Decision

Remove an advisory only when its deps.dev detail lookup returned HTTP 404 and `GET /v1/vulns/{id}` returned a matching record whose `withdrawn` value is an RFC3339 timestamp. A deps.dev error, OSV error, OSV 404, malformed response, invalid withdrawal timestamp, ambiguous ID or withdrawal key, or different OSV ID retains the advisory with unknown severity. Invalid by-ID answers are lookup failures and are not cached. The existing OSV client supplies by-ID requests and its TTL cache. One bounded eight-worker lookup pass, with a 30-second default budget, handles the unique missing-detail IDs in each enrichment; the dependency pass unions them with its existing known-HIGH age-fact IDs. These are separate budgets for the direct and dependency passes.

Filter the shared `VersionDetail.Advisories` slices before assessment and rendering. Direct lookup and filtering cover Stable and MaxSemver, plus only the selected `LatestVersionDetail()` fallback when both are absent; severity enrichment still covers Stable and MaxSemver alone. Pass confirmed direct IDs to dependency enrichment so a removed ID cannot reappear, including when OSV caching is disabled or a later lookup times out. The dependency pass removes its own confirmed missing-detail IDs before append and from existing selected version lists, including a direct advisory whose earlier OSV lookup failed. Retained 404 dependency entries have no OSV age-fact fields; only known-HIGH entries with successful deps.dev detail keep those fields.

GitHub URL analysis first runs the PURL direct and dependency passes. Its final batch pass enriches severity only and makes no further OSV withdrawal lookup. A structured debug log records confirmed removal; no output field, flag, environment variable, or dependency is added.

## Alternatives

- Treat deps.dev 404 alone as withdrawal: risks removing active advisories and allowing an unfounded Legacy-Safe/ok result.
- Use OSV withdrawal alone: risks changing advisory scope when deps.dev still provides active detail.
- Add another OSV client or per-scan cache: duplicates the existing by-ID request and TTL cache; per-pass ID deduplication and direct-to-dependency propagation cover the needed calls.

## Consequences

Counts, lifecycle rules, and output read the same filtered list. Inconclusive lookups retain the prior conservative behavior. When OSV is unavailable, the two PURL passes can each use up to 30 seconds before retaining unanswered advisories.
