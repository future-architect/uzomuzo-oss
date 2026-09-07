# 0025. RustSec "unmaintained" is a package-level fact, and it never reaches EOL-Confirmed

Date: 2026-09-07
Status: Accepted

## Context

uzomuzo's EOL rule chain (`internal/infrastructure/eolevaluator/evaluator.go:92-121`)
has a registry-deprecation rule for every ecosystem whose registry publishes one: npm
`deprecated`, PyPI `Development Status :: 7 - Inactive`, Packagist `abandoned`, NuGet
deprecation, Maven relocation. Cargo has no such rule, because crates.io has no
deprecation mechanism at all. The chain's only cargo rule is `applyCargoYanked`, and
[ADR-0021](0021-yank-is-version-specific.md) already settled that a yank is
evidence about the version yanked and nothing else.

The Rust ecosystem instead records "this crate is unmaintained" in the RustSec
advisory database, which reaches OSV.dev as
`affected[].database_specific.informational == "unmaintained"`. uzomuzo's
existing advisory source, deps.dev's `GET /v3alpha/advisories/{id}`, returns only
`{id, url, title, aliases, cvss3Score, cvss3Vector}` — the marker is dropped
before any decision sees it.

### What the marker actually says, measured

A full-population read of `rustsec/advisory-db` (1,219 advisories parsed; all 269
carrying the `unmaintained` marker read by hand, one advisory body at a time)
found no single class of evidence behind it:

| basis for the claim | count |
|---|---|
| author/maintainer statement | 71 |
| author action only (repo archived, crate deleted) | 15 |
| "deprecated", no attribution given | 13 |
| rename/merge/superseded by another crate | 58 |
| explicitly implicit (author unresponsive, contact attempt failed) | 35 |
| unattributed "is no longer maintained" | 77 |

No structured field separates these anywhere in the pipeline: not in OSV, not in
the upstream TOML (every key in the schema was enumerated; none carries
attribution), and not in the `rustsec` crate's own type
(`enum Informational { Notice, Unmaintained, Unsound, Other(String) }`, which
collapses all six rows above to one variant). OSV's `details` field equals the
upstream advisory body verbatim in all 269 cases, so no rewriting happens along
the way that could be undone. Classifying prose by rule leaves 90 of 269 (33%)
undecidable, and the newest filings are templated and unattributed, so the
undecidable share is growing, not shrinking.

### Why this matters downstream

`uzomuzo-catalog` assigns `PrimarySourceEOL = a.EOL.IsEOL()` and treats a
false-to-true flip of that field as authoritative: it demotes a human `not_eol`
judgement back to `pending`, discarding the human's `reason`, `reason_ja`,
`eol_date`, and `successor`. Any signal that sets `EOLState` to `EOLEndOfLife`
therefore overrides a person's prior decision, not just a machine label.

## Decision

Query OSV.dev directly for cargo packages (`POST /v1/query`,
`{"package":{"name":"<crate>","ecosystem":"crates.io"}}`) and record the
`unmaintained` marker as a package-level fact on the aggregate,
`Analysis.AdvisoryDBState`, read by the lifecycle assessor. The fact alone
yields `Stalled`. The fact combined with unpatched HIGH+ advisories yields
`EOL-Effective` through the existing `severityAwareLabel` helper. It **never**
yields `EOL-Confirmed`. Nothing is written to `EOLStatus`: `EOL.IsEOL()` stays
false, so — mirroring [ADR-0022](0022-all-releases-yanked-is-not-eol.md) — the
catalog's `PrimarySourceEOL` never flips to true on this fact alone, and no
human `not_eol` judgement is demoted because of it.

### Why never EOL-Confirmed

`EOLEndOfLife` is defined as "primary sources mark the package/project as
EOL/abandoned/sunset" (`internal/domain/analysis/eol.go:17`). RustSec is a
third-party curator forming an inference about a crate, not a primary source
making a statement about itself. [ADR-0020](0020-archived-registry-liveness.md)
reserves `EOL-Confirmed` for an explicit primary-source signal, and
[ADR-0022](0022-all-releases-yanked-is-not-eol.md) refused it even for a
first-party registry reporting full withdrawal — a stronger, first-party fact
than anything RustSec can offer. A curator's inference, which the measurement
above shows is unattributed or ambiguous one time in three, is strictly weaker
than either. `Stalled` states what is actually known: development looks to have
slowed or stopped. It says nothing about who decided that or why, which matches
the evidence.

### The package-wide quantifier

RustSec advisories are per-crate but their affected ranges are per-version, and
an early draft of this decision conflated the two. The rule must ask "does this
advisory cover the whole package", not "does this advisory exist for this
package". The admission filters:

- ecosystem is cargo, and the OSV request names `crates.io` (OSV's own
  ecosystem identifier for it);
- the affected entry's `package.name` and `package.ecosystem` match the crate
  queried;
- the advisory ID carries the `RUSTSEC-` prefix — `database_specific` is
  source-defined by whoever files with OSV, so provenance is what justifies
  trusting the field at all;
- `informational` is exactly `"unmaintained"` (`"unsound"` and `"notice"` are
  different claims and are ignored);
- the advisory is not withdrawn (9 of 269 are, every one because the crate
  became maintained again);
- the affected range provably covers the whole package: a universal check over
  **every** affected entry naming the queried package — no `versions` array
  present, at least one `ranges` entry and every one of type `SEMVER`, every
  range containing exactly one `introduced` event whose value is `0` or
  `0.0.0-0`, and no range carrying `fixed`, `last_affected`, or a `limit`. Any
  shape not recognized by this check is a reject, not a best-effort match.

The counterexample that forced the last bullet: `ring`'s RUSTSEC-2025-0010
carries `informational: unmaintained` bounded by `fixed: 0.17.0` — a claim about
the 0.16.x line only. A version-less OSV query still returns the advisory; a
query scoped to `ring@0.17.14` does not. An earlier formulation of the
quantifier ("introduced is zero and there is no fixed") admitted this advisory
and would have marked a widely used, actively maintained crate as unmaintained.
About 5 of the 269 advisories are genuinely version-bounded this way and are
skipped by the filter above. The resulting failure mode is a missed detection,
never a false one — the same standard [ADR-0022](0022-all-releases-yanked-is-not-eol.md)
already set for withdrawal facts.

### The 14-day cooldown

Withdrawn advisories can be re-filed, and a re-file re-triggers the
false-to-true flip described above. `ring`'s RUSTSEC-2025-0007 was withdrawn two
days after filing. Nothing about a maintenance signal is urgent enough to
justify propagating a filing that might not survive the week, so the fact is
only admitted once the advisory has been published for at least 14 days. The
downstream catalog has no buffer of its own for this, so holding the delay here
is the one place it is guaranteed to apply. A missing or unparseable
publication date is treated as a non-match, not as an already-aged one.

### Placement

The fact is read by a single new branch in `assessInternal`
(`internal/domain/analysis/lifecycle_assessor.go`), numbered 1.4, between the
all-releases-yanked branch (1.25, [ADR-0022](0022-all-releases-yanked-is-not-eol.md))
and the archive/disable branch (1.5, [ADR-0020](0020-archived-registry-liveness.md)).
A single branch, rather than a condition threaded through the five exits
that can return `Active` or `Legacy-Safe`, keeps "a flagged package is never
Active and never Legacy-Safe" a property of one place instead of an emergent
property of several branches that a later lifecycle edit could forget to
update. It runs before the archive branch deliberately: the archive branch
returns `Stalled` unconditionally on its own reasoning, and running after it
would mask the `EOL-Effective` case (fact plus unpatched HIGH+ advisories) for
an archived-and-unmaintained crate.

### The marker is not counted as one of the vulnerabilities

`severityAwareLabel` reads the severity of the advisories deps.dev lists on the
version, and `hasHighSeverityAdvisories` treats any advisory of unknown severity
as potentially HIGH — a conservative fallback that predates this decision.

RustSec files its `unmaintained` marker as an ordinary advisory, and deps.dev
lists it on the version with no CVSS score. Read naively, the branch would
therefore receive its own evidence back as an unpatched vulnerability, every
flagged crate would reach `EOL-Effective`, and the `Stalled` outcome decided
above would be unreachable. `term_size@0.3.2` has exactly this shape in
production: one advisory, `RUSTSEC-2020-0163`, which is the marker itself.

Branch 1.4 therefore evaluates severity with that one advisory ID excluded
(`hasHighSeverityAdvisoriesExcluding`). The exclusion is scoped to the branch's
own evidence and to nothing else: a genuine advisory alongside the marker still
yields `EOL-Effective`, and other RustSec informational records (`unsound`,
`notice`) keep counting exactly as they did before, since changing that would be
a separate decision about the residual-vulnerability path.

### Rejected alternatives

- **Mapping the marker straight to `EOL-Confirmed`.** Would have marked `ring`
  end-of-life on the strength of a filing that describes only its 0.16.x line.
- **Splitting explicit from implicit attribution and trusting only the
  explicit half.** Not machine-decidable from what OSV or the upstream TOML
  expose: 33% of advisories are undecidable by rule, no structured field
  records the distinction anywhere in the pipeline, and the newest filings are
  the least attributed, so the gap is widening rather than closing.
- **Recording the fact on `RegistryState`.** That type
  (`internal/domain/analysis/models.go`) is scoped to facts a package registry
  asserts about itself; its `Registry` enum has no RustSec member, and RustSec
  is not a registry.
- **Treating the marker as hard evidence because withdrawals are rare.** The
  withdrawal rate does not distinguish explicit from implicit attribution
  (2 of 86 explicit filings withdrawn versus 3 of 35 implicit ones), the sample
  is too small to draw a rate from (n=9 withdrawals total), exposure time
  differs across cohorts, and a low withdrawal rate says nothing about whether
  an un-withdrawn filing was accurate in the first place. Provenance and claim
  semantics — not durability — are what rule out `EOL-Confirmed`.
- **Querying deps.dev's BigQuery export instead of OSV's REST API.** Everything
  this decision needs comes from OSV's free, unauthenticated REST endpoint. The
  BigQuery path would need a GCP project, Workload Identity Federation from
  GitHub Actions, and query billing, in exchange for a dataset that carries
  neither Scorecard, GitHub archived state, nor the deps.dev findings layer we
  already depend on elsewhere.
- **Treating the 58 rename/merge advisories as unmaintained.** They describe
  generational change, not abandonment, and typically name a successor crate.
  They are left out of scope here and are recorded as a candidate for a
  separate `Superseded` fact, which is a different claim entirely.

## Consequences

- **Behavior change**: a cargo crate carrying a package-wide, 14-day-old,
  non-withdrawn `unmaintained` RustSec advisory now reports `Stalled` (or
  `EOL-Effective`, if it also carries an unpatched HIGH+ advisory) instead of
  whatever the rest of the rule chain would otherwise have produced. It never
  reports `EOL-Confirmed` on the strength of this fact alone.
- **Staleness**: a crate that resumes releasing keeps reporting `Stalled` until
  RustSec withdraws the advisory. This is deliberate and directly precedented
  by [ADR-0020](0020-archived-registry-liveness.md), which rejected a
  release-recency gate on the archive signal for the identical reason — the
  fact describes what a curator asserted, not the crate's current pulse.
- **Coverage gap**: version-bounded advisories (about 5 of 269, `ring` among
  them) are ignored by design, so a crate that is unmaintained on an old line
  only stays unflagged on that line.
- **Delay**: the 14-day cooldown delays every signal admitted through this
  path, including ones that turn out durable.
- **Scope**: cargo only. No other ecosystem is touched by this decision.
- **Fragility to upstream rename**: a RustSec rename or retirement of the
  `informational` field fails silently — a fixture pinned to the literal
  string `"unmaintained"` cannot detect an upstream rename.
- **Fetch failure**: a failed or skipped OSV lookup leaves `AdvisoryDBState`
  unset, which reads as "not flagged" — consistent with how every other
  optional package-level fact in this codebase behaves on fetch failure.
