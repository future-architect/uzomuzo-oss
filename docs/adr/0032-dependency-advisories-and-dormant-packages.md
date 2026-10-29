# ADR-0032: A dependency advisory makes a dormant package EOL-Effective only after the dormancy period

Date: 2026-10-29
Status: Accepted

## Context

Since #123, the advisory list of the version the lifecycle assessor reads (`StableVersion`, or `MaxSemverVersion` as a fallback) also holds the advisories of its dependencies (`Relation = TRANSITIVE`, [ADR-0011](0011-transitive-advisory-version-scope.md)). The assessor counted them like the package's own. A dormant package with no advisory of its own could therefore be `EOL-Effective` (verdict `replace`) because a dependency had a HIGH advisory.

Example: `pkg:npm/exceljs@4.4.0` has no advisory of its own. Its dependency `uuid@8.3.2` has GHSA-w5hq-g745-h8pq, CVSS3 7.5. GitHub rates it MODERATE, and exceljs only calls `v4()` without arguments, which the advisory does not concern. exceljs was `EOL-Effective` with the reason "Unmaintained, unpatched vulnerabilities", and an AI agent using the `scan-before-add` skill replaced it.

But some such packages should stay `replace`. `spincycle@2.1.22` has 47 dependency advisories (CRITICAL among them) from `node-forge@0.7.6`, `bson@0.4.23` and `minimist@0.0.8`, and its last commit was in 2018.

[ADR-0001](0001-lifecycle-legacy-safe-relaxation.md) was written when only a package's own advisories existed. [ADR-0025](0025-rustsec-unmaintained-is-a-weak-cargo-signal.md) says a HIGH advisory next to the RustSec unmaintained marker yields `EOL-Effective`, without distinguishing own and dependency advisories. Issue #591 has the full investigation.

## Decision

The package's own advisories are decided as before, except that with `ResidualAdvisoryThreshold` above 1 the residual-vulnerability override needs the package's own advisories alone to reach the threshold for `EOL-Effective`; when only own and dependency advisories together reach it, the dependency rule below decides (the reason then says "too few own advisories"). In the four decision points below, a dormant package whose HIGH advisories are all in dependencies is `EOL-Effective` only when one of them is **aged**:

- its CVSS3 score is known and at or above `HighSeverityCVSSThreshold` (7.0);
- the advisory database's reviewed rating (OSV `database_specific.severity`) is HIGH, CRITICAL, or absent. RustSec records carry none. LOW, MODERATE, MEDIUM and any unrecognized value do not qualify;
- OSV does not mark it withdrawn;
- it was published (OSV `published`) more than `EolInactivityDays` (730) days before the scan, counted in whole UTC days. An advisory exactly 730 days old is not aged.

Otherwise the decision point returns `Stalled` itself; it does not fall through, because a recent release would then reach the `Active` branches and turn `replace` into `ok`. The verdict is `caution`, or `replace` when the repository is archived.

The four decision points are the residual-vulnerability override (Scorecard missing or incomplete), the RustSec unmaintained branch (this supersedes, for dependency advisories, ADR-0025's statement that a HIGH advisory with the marker yields `EOL-Effective`), and the two branches used without human-commit data, C2a and C3a. ADR-0001's `Legacy-Safe` rule for `advisoryCount == 0` was written before dependency advisories existed; this ADR decides that it counts them too, so a package with only dependency advisories never becomes `Legacy-Safe` through it. The Scorecard `Vulnerabilities` score branches do not change; none of the 184 measured packages with Scorecard data in deps.dev had a `Vulnerabilities` check (#124).

Why this line: a dependency advisory says nothing about whether the package reaches the vulnerable code. An old one suggests the dependency was not updated during the dormancy period, which is a statement about maintenance, what uzomuzo measures (ADR-0011). It is an inference: uzomuzo does not know when the dependency was added or whether a usable fix existed. The reviewed rating matters because CVSS3 cannot express preconditions; GHSA-w5hq is HIGH by CVSS3 and MODERATE in GitHub's review, so exceljs does not return to `replace` when that advisory ages in 2028. No threshold, flag or configuration is added.

### Data

For each dependency advisory whose CVSS3 score is known and reaches the threshold the assessor applies, the scan fetches `GET https://api.osv.dev/v1/vulns/{id}` and records on the advisory what the lookup found: `found`, `not_found`, `withdrawn` or `lookup_failed` (`Advisory.RecordLookup`), with the publication date and reviewed rating. The assessor reads the default lifecycle configuration, so the lookup uses the same threshold; a lookup threshold that differed would leave an advisory the assessor counts without a date. Lookups run eight at a time within a 30-second budget per enrichment (one per analysis batch); IDs not answered within it, or cut off by cancellation, are `lookup_failed`. Answers, including 404, are cached for the OSV client's cache TTL and dropped by `SetCacheTTL`; errors are not cached. Concurrent lookups of one ID are not merged: one enrichment already asks each ID once.

### Outcome and output

`ClassifyDependencyAdvisoryAge` gives each candidate advisory one status and picks one advisory to decide and explain the result. Statuses in priority order, highest first: `aged`, `unknown` (lookup failed: the label might have been `EOL-Effective`), `not_aged`, `rated_below_high`, `rating_unrecognized`, `undated` (not in OSV, or no date), `withdrawn`, `not_checked` (no lookup was made, for example a library caller without an OSV client). Among aged advisories the oldest wins, among not-aged ones the one that ages first, otherwise the lowest ID, so the result does not depend on list order.

- Each status has its own reason, naming the advisory and the dependency (except `not_checked`, which keeps the plain "HIGH advisories only in dependencies"), for example `Unmaintained; HIGH dependency advisory published 2018-04-26, over 730 days ago (GHSA-jp4x-w63m-7wgm in hoek@2.16.3)` and `Unmaintained; HIGH advisories only in dependencies; oldest published 2026-02-26, counts from 2028-02-27 (GHSA-23c5-xmqv-rm74 in minimatch@3.0.8)`. In the RustSec branch the reason keeps the marker and its summary in front, since the summary often names a successor.
- `AssessmentResult.DependencyAdvisoryAge` carries the deciding advisory; JSON shows it as `dependency_advisory_age` (`status`, `advisory_id`, `dependency`, `published`, `eligible_on`, `reviewed_severity`). It is absent when the package's own data decided.
- When any package ends `unknown`, `scan` and `diet` print one WARN line with the count. `--fail-on` does not change: an OSV outage lets those packages pass `--fail-on eol-effective` for that scan.
- Signals: `transitive_advisory_count` when above 0, and `aged_dependency_advisory` when an aged advisory decides.

## Rejected

| Option | Why not |
|---|---|
| Removing dependency advisories from every lifecycle check | exceljs would have no advisory and reach `Legacy-Safe` (`ok`) through ADR-0001. |
| Never letting a dependency advisory make a package `EOL-Effective` | spincycle-like packages lose `replace`; all 94 packages of the sample stop failing `--fail-on eol-effective`. |
| The same rule without the reviewed rating | 43 instead of 18 of the measured packages return to `replace` by 2028, exceljs among them. |
| Counting only when the fix is outside the declared range | exceljs's `^8.3.0` excludes every fixed uuid version. |
| Counting only CRITICAL, or a count threshold | The line is arbitrary and says nothing about maintenance. |
| Escalating when the vulnerable dependency version is deprecated, old, or direct | Each sends exceljs to `replace`. |
| Honoring VEX or "not affected" statements | None found for the measured packages (2026-10-28); the targeted packages are by definition not maintained. Revisit if VEX becomes common on npm. |
| A separate `--fail-on` fact for dependency advisories | Kept in reserve: it would also fail on active packages (19 `ok` packages of the sample have dependency advisories). |
| Merging concurrent lookups of one ID, or a per-scan cache | One enrichment already deduplicates IDs; the OSV client's existing TTL cache is the convention, and only its reset was missing. |

## Measurement

453 packages selected towards old ones: 389 npm packages (dependents of uuid, minimist, lodash and tough-cookie, and exceljs), 62 crates with the RustSec unmaintained marker, and two npm controls, dicer and xlsx; measured with and without `GITHUB_TOKEN`. The selection shows what changes, not ecosystem-wide rates.

| | with token | without token |
|---|---|---|
| `EOL-Effective` before only because of dependency advisories | 94 | 115 |
| stays `EOL-Effective` (an aged advisory) | 27 | 32 |
| `Stalled` | 67 (59 `caution`, 8 `replace` because archived) | 83 |
| to `Active` or `Legacy-Safe` | 0 | 0 |

The one aged advisory that is not a GHSA ID, RUSTSEC-2020-0027, is CRITICAL in its GHSA alias. Of the 67, 18 will return to `EOL-Effective` by the end of 2028 if nothing changes, for example jshint on 2028-02-27 and zip-extract on 2028-10-09. Cost, with token: 162 OSV lookups for the 453 packages, none failed; the run took 372 s against 371 s before, and 328 s against 330 s without a token. With only `/v1/vulns/` failing (through a local proxy), the 70 packages whose decision used a looked-up dependency advisory, the 27 aged ones among them, became `unknown` and the WARN line counted 70; no package decided by its own advisories changed, the malicious check was unaffected, and the run took 17 s longer.

## Limits

- Reachability is not answered; an exceljs-like package with an aged HIGH advisory gets `replace`.
- A new advisory about an old bug waits 730 days; an old advisory whose fix shipped recently counts at once (fix dates are unknown).
- Labels change without a release as advisories age; `eligible_on` and the reason say when.
- RustSec records carry no rating, so for crates the rating check never stops escalation. The one aged RustSec advisory measured is CRITICAL in its GHSA alias; following aliases is a follow-up if that stops holding.
- A dependency advisory without a known CVSS3 score (for example when the deps.dev advisory lookup failed) is not looked up; a package whose only dependency advisories are such is `Stalled` with "dependency advisories of unknown severity", without the WARN line. Advisories against the package itself keep the conservative treatment of an unknown score as HIGH.
- An OSV outage leaves the affected packages `Stalled`, with a WARN line and `unknown` in JSON, and they pass `--fail-on eol-effective` for that scan.
