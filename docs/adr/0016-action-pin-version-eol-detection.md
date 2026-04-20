# ADR-0016: Pinned GitHub Action Version EOL Detection

## Status

Deferred (2026-04-20)

## Context

`--include-actions` (ADR-0005) evaluates Actions discovered in a target repository's workflows, but its verdicts are derived from **repository-level** lifecycle signals alone. The parser captures the `@ref` suffix (`v2`, `v3`, a commit SHA, ...), but the evaluator receives only `owner/repo`, so every pinned version of a still-active action is treated as `ok`. This leaves the most strongly-documented class of Action deprecations — dated GitHub announcements such as `actions/upload-artifact@v3` (force-sunset 2025-01-30) and `actions/checkout@v2` (Node 12 runtime removed 2023-06-30) — invisible.

A working prototype (PR #320) was built to close this gap via a static, source-cited catalog in `internal/domain/actions/` with ref propagation through the scan pipeline.

## Decision

**Defer** version-level Action EOL detection. The prototype is not merged.

## Reasons

1. **Tier-1 hit rate is zero.** Rescanning the 5 Tier-1 OSS targets (goreleaser, gitleaks, flask, jabref, axios) with the prototype produced `replace: 0` across every repo: 4 of 5 use Scorecard-recommended SHA pins (catalog correctly abstains), and JabRef pins only current majors. The catalog stays silent exactly where it should, but no user-visible improvement materializes until the long tail (repos still tag-pinning deprecated majors) is scanned.
2. **Priority vs. maintenance cost.** The surface covers ~2–3 announcements per year. The prototype adds +1227 lines that (a) change the public `DiscoverActions` signature, (b) add an optional `ActionRefs` field to `AuditEntry` meaningful only for three `Source` values, (c) duplicate the ref-aggregation pattern across `DiscoveryResult`, `discoverFromRepo`, and `resolveLocalActions`, and (d) leave `ParseWorkflowAll` as a scaffolding wrapper around `ParseWorkflowAllWithRefs`. Dependency-side work (transitive advisories, new ecosystems, fuzz-found parser bugs) has higher user need per engineering hour.
3. **Auditability is load-bearing and manual.** Every catalog entry ships as a claim in external PR bodies, so each date and major must be verified against an upstream announcement. Automated sourcing (alternative D in the prototype) was already rejected as YAGNI. With the static path, catalog maintenance is an ongoing manual task that is only justified by a proportional hit rate.

## Revisit condition

Reopen when **any** of the following holds:

- The SHA → tag resolver (prototype §Alternative E) lands. That closes the Tier-1 gap, so version-level detection gains observable hits to justify the plumbing.
- A concrete outreach target (a well-known OSS repo pinning a known-deprecated major by tag) is identified where a uzomuzo-authored PR would land.
- Dogfood sweeps on Tier 2/3 repositories show a materially higher rate of tag-pinned deprecated majors than Tier 1.

On revisit, prefer a narrower plumbing shape than the prototype: collapse the three ref-aggregation sites into one, and migrate callers off `ParseWorkflowAll` before adding `ParseWorkflowAllWithRefs` rather than keeping both.

## References

- Prototype implementation and empirical validation: closed [PR #320](https://github.com/future-architect/uzomuzo-oss/pull/320).
- Related: [ADR-0005](0005-github-actions-health-scanning.md) (repository-level Actions evaluation), [ADR-0007](0007-skip-transitive-actions-scanning.md) / [ADR-0008](0008-transitive-composite-action-scanning.md) (Actions scope evolution).
