# ADR-0016: Pinned GitHub Action Version EOL Detection

## Status

Accepted (2026-04-19)

## Context

`--include-actions` (ADR-0005) evaluates Actions discovered in a target repository's workflows, but its verdicts are derived from **repository-level** lifecycle signals alone. Empirical scans against goreleaser, gitleaks, flask, jabref, and axios produced zero `replace` verdicts even though these repositories can ship workflows pinned to known-deprecated versions such as `actions/upload-artifact@v3` (force-sunset by GitHub on 2025-01-30) or `actions/checkout@v2` (Node 12 runtime removed in 2023-06).

The root cause: the parser captures the `@ref` suffix (`v2`, `v3`, a commit SHA, ...) but the evaluator receives only `owner/repo`, so every pinned version of a still-active action is treated as `ok`. This prevents uzomuzo from surfacing the most strongly-documented class of Action deprecations — those with a fixed GitHub announcement date and an obvious upgrade target.

A secondary consideration is reputational: because uzomuzo is used to author PRs against external OSS, every EOL claim must be grounded in a verifiable primary source. Any version-level detection must ship with load-bearing, auditable evidence rather than heuristics.

## Decision

Add a static, pure-domain catalog of GitHub Action version deprecations and propagate ref information through the scan pipeline so every action-sourced entry can be matched against it.

### Data flow

1. `ghaworkflow.ParseWorkflowAllWithRefs` preserves the `@ref` suffix as `ActionRef`. `ParseWorkflowAll` becomes a thin URL-dedup wrapper so existing callers keep their signature.
2. `actionscan.DiscoveryService` threads refs through direct, local composite, and transitive BFS paths into a new `map[string][]string` return (GitHub URL → sorted distinct refs).
3. `application/scan.Service` attaches refs to each `AuditEntry.ActionRefs` (new field) and, before fail-policy evaluation, calls `applyActionPinCatalog` to flip the entry's `EOLStatus` to `EOLEndOfLife` when any pinned ref matches a catalog entry. `DeriveVerdict` then yields `VerdictReplace` without any verdict-specific branching.
4. Renderers expose two additive fields: `eol_reason` (from `EOL.FinalReason()`) and `action_refs` in JSON and CSV output.

### Catalog design

- **Location**: `internal/domain/actions/` — pure domain, standard library only.
- **Shape**: `DeprecatedEntry{Owner, Repo, DeprecatedMajors, Reason, EOLDate, SuggestedVersion, ReferenceURL}` — each entry cites an upstream GitHub announcement.
- **Matching**: major-version prefix (`v2` matches `v2`, `v2.3`, `v2.3.1`) via `MatchesMajor` + `MajorOf`. Non-tag refs (commit SHAs, branch names, empty) are rejected by `IsTagRef` and fall back to repository-level evaluation.
- **Seed scope**: **hard** deprecations with dated GitHub announcements only (`actions/upload-artifact` v1–v3, `actions/download-artifact` v1–v3, `actions/checkout` v1–v2 at initial release). Entries with only "recommended upgrade" warnings are excluded to prevent false EOL claims in PR bodies.
- **Invariants enforced by tests**: every entry has a valid `ReferenceURL`, a `SuggestedVersion` not itself in `DeprecatedMajors`, and a `Reason` containing its `EOLDate` when known.

## Alternatives considered

### A. Encode the ref inside the PURL

Rejected. Rewriting `https://github.com/actions/checkout` as `pkg:githubactions/actions/checkout@v3` would ripple through deps.dev clients, license resolvers, CSV/JSON schema consumers, the `pkg/uzomuzo` public facade, and the successor-fallback logic. Action-scoped behavior would leak into every ecosystem-neutral code path.

### B. Add `ActionRefs` directly on `domain/analysis.Analysis`

Rejected. `Analysis` is used by every ecosystem (npm, Maven, Go modules, ...). Placing Action-specific data on it couples the generic domain type to a single scan mode. Keeping `ActionRefs` on `AuditEntry` (a scan-orchestration type) preserves domain purity.

### C. Use the existing `AnalysisEnricher` hook

Rejected for the same reason as (B). The enricher receives `map[string]*Analysis` and has no access to the per-entry ref context. Running catalog application inside Application-layer orchestration (in `RunFromPURLsWithActions`, after entries are built) keeps the mutation target close to the data it needs.

### D. Auto-fetch deprecation announcements from GitHub

Rejected as YAGNI. The announcements are formal, low-frequency events (2–3 per year for the core `actions/*` family). Static catalog entries are auditable in code review and unit-tested; a dynamic fetcher adds API calls, rate-limit budgeting, HTML parsing, and a failure mode that has no analogue in the current scan pipeline.

### E. Resolve SHA pins to tags via the GitHub API

Deferred. Many repositories pin actions to a commit SHA (often with a `# v4.2.0` comment). Resolving SHA → tag requires a new `github.Client.ListTags` method, cache, and rate-limit budget. For the initial PR, SHA and branch refs (`main`, `master`, ...) deterministically fall back to repository-level evaluation, producing the same verdicts as before this change. A follow-up ADR can cover SHA resolution when the volume of false negatives justifies it.

## Consequences

### Positive

- **Catches the highest-value deprecations**: `upload-artifact@v3`, `checkout@v2`, and related force-sunset majors are now `VerdictReplace` with a dated, citable reason suitable for PR bodies.
- **Auditable claims**: each catalog entry carries a `ReferenceURL` to the authoritative GitHub announcement; tests enforce this invariant.
- **Additive schema change**: `ActionRefs` on `AuditEntry`, `eol_reason` and `action_refs` in JSON/CSV — no removal of existing fields. CSV consumers that index by column position see new columns appended at the tail.
- **Composable with `--fail-on`**: because the catalog is applied before fail-policy evaluation, `--fail-on eol-confirmed` now trips on pinned-version deprecations as expected.

### Negative

- **Catalog authoring is load-bearing**: a wrong date or a mis-categorized major would appear verbatim in external PR bodies. Mitigated by (a) tight seed scope (3 action families at release), (b) test-enforced invariants, and (c) per-entry `ReferenceURL` review.
- **SHA-pinned actions remain invisible**: pipelines that follow the Scorecard-recommended SHA pin pattern will not see version-level findings until the SHA → tag resolver lands. The existing repository-level verdict still applies.
- **Follow-up work**: the `--file .github/workflows/ci.yml` path currently bypasses `RunFromPURLsWithActions` and does not receive the catalog treatment. Extending that path requires a parallel `WorkflowRefParser` injection point in `interfaces/cli` and is tracked separately.

### Neutral

- No new CLI flag, no new environment variable. Catalog is always active for action-sourced entries, matching the `project-conventions.md` "Configuration & Flags Policy".
