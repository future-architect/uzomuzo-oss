# 0026. A release burst across lines after a long silence needs a human

Date: 2026-10-08
Status: Accepted

## Context

uzomuzo judges the project behind a package: is it maintained, frozen, or ended.
That judgement says nothing about whether a particular release came from the
maintainer. Takeovers of dormant packages exploit exactly that gap: the project
looks frozen-but-fine, the attacker publishes a new version, and the new version
makes the project look *active*.

`pkg:npm/node-ipc@12.0.1` is the concrete case. In May 2026 three versions were
published from the account of a co-maintainer that had been idle for 1,476 days.
depi.security's analysis judges the likely route to be an e-mail takeover: the
account's expired e-mail domain was re-registered and the npm password reset,
though the reset itself was not observed
(<https://depi.security/blog/20260514-node-ipc-compromised/>). Before this change
uzomuzo reported that version as:

```
✅ ok       pkg:npm/node-ipc@12.0.1  Active
│ ✅ Active: Actively maintained with recent releases
```

The malicious release itself was the "recent release".

npm keeps the publish time of a version in the packument (the registry's full
JSON document for a package) under `time` after the version is unpublished, so the shape of the incident is still readable from
<https://registry.npmjs.org/node-ipc>:

```
12.0.0   2024-08-12 16:28   last legitimate release
12.0.1   2026-05-14 14:25:30  ┐
9.2.3    2026-05-14 14:26:01  ├ 639 days later, three versions in 55 seconds,
9.1.6    2026-05-14 14:26:25  ┘ on two release lines (12 and the old 9 line)
14.0.0   2026-08-24 22:17   next legitimate release
```

deps.dev does not have the three versions at all, so the signal has to come from
the registry.

## Decision

After the lifecycle decision tree has produced its label, apply one more rule.
When the label is **Active or Legacy-Safe** and the analysed version was published

1. after at least `RecentStableWindowDays` (365 by default) without any release,
2. in a burst — every release within `DormantBurstWindow` (24 hours) of the first
   release after the silence, counting only releases published by the moment
   of evaluation — that touches at least `DormantBurstMinLines` (2)
   release lines (the major version, or `0.minor` below 1.0), and
3. no more than `RecentStableWindowDays` ago,

the label becomes **Review Needed**, with the silence, the lines and the burst in
the reason and signals. The signals behind the replaced label are kept after
them. Prereleases (`2.0.0-alpha.0`) open no line: `^2` never
resolves to them.

The rule only replaces an ok outcome. Stalled, EOL-Effective, EOL-Confirmed,
EOL-Scheduled and Review Needed for another reason are left as they are, so a
`--fail-on stalled,eol-effective` gate keeps firing for a burst version; a gate
that should also catch bursts adds `review-needed`. An earlier draft placed the
rule before the archive branch and turned an archived (Stalled) or EOL-Effective
package into Review Needed, which silently disarmed those gates.

If the registry no longer serves the version, that is added to the reason for
display; it is never part of the decision.

npm only. The publish times come from the full packument
(`GET https://registry.npmjs.org/<name>`, one request per distinct package,
20-second timeout); the abbreviated install document carries no per-version
times. A PURL without a version is not fetched: the rule judges a version, so
an unversioned input (`pkg:npm/node-ipc`) never reaches it. A failed fetch leaves `Analysis.ReleaseHistory` nil, and the rule does nothing.

### Why lines, and not the silence alone

A single release after a year of silence is ordinary. Mature small packages ship
a fix every few years (`braces` 3.0.3 came five years after 3.0.2). Measured on
two real lockfiles, the silence alone matched about a fifth of all entries
(VS Code: 289 of 1,422; npm CLI: 230 of 993; see the evidence below) and is
unusable.

What separates the takeovers is that the attacker publishes to *every line in
use*, so that `^9` and `^12` users both resolve to a malicious version. A
maintainer returning after years usually ships one release.

### Why `RecentStableWindowDays`, and not `EolInactivityDays`

The burst is a return from the state this assessor already calls "no recent
stable release", so it reuses that threshold instead of inventing one.
`EolInactivityDays` (730) would have missed node-ipc (639 days). The same 365
days also cap how old a flagged version may be. This is a policy choice, not a
measurement: the flag describes how a version was published, and a fixed cap
keeps a one-time publication pattern from labelling a version forever. Without
it, a legitimate pair from 2018 (`extend` 3.0.2 and 2.0.2) would stay in Review
Needed indefinitely.

### Why Review Needed, and not a new label or verdict

A person has to decide whether the release is the maintainer's; nothing in the
registry data says so. Review Needed is the existing label for that, and
`--fail-on review-needed` (#506) already turns it into a CI gate. Changing only
the status column (ok / review) was rejected: `--fail-on` reads the lifecycle
label, not the status column, so that change would never stop CI.

## Evidence

Reproduce with `go run ./scripts/dormant-burst` (see its doc comment). Registry
data drifts; the figures below were taken on 2026-10-08.

### Incidents (`scripts/dormant-burst/cases.json`), evaluated one day after publication

| package@version | kind | fires | silent days | lines |
|---|---|---|---|---|
| node-ipc@12.0.1 (2026-05) | dormant takeover | **yes** | 639 | 9, 12 |
| rc@1.2.9 (2021-11) | dormant takeover | **yes** | 1,257 | 1, 2 |
| coa@2.0.3 (2021-11) | dormant takeover | **yes** | 1,059 | 2, 3 |
| is@3.3.1 (2025-07) | dormant takeover | **yes** | 2,408 | 3, 5 |
| event-stream@3.3.6 (2018-09) | dormant takeover | no — 4-day silence, one line | | |
| rand-user-agent@2.0.83 (2025-04) | dormant takeover | no — 195-day silence | | |
| colors@1.4.1, faker@6.6.6 (2022-01) | maintainer's own release | no | | |
| ua-parser-js, eslint-scope, eslint-config-prettier, chalk, debug, axios, @solana/web3.js | takeover of an active package | no (by design) | | |

4 of the 6 dormant takeovers. None of the 7 active-package takeovers, which is
the intended scope: those packages were never silent.

### False positives on real lockfiles, evaluated on 2026-10-08

"Entries" are the distinct `name@version` pairs in the lockfile for which the
registry returned a publish time (every pair in both lockfiles). An npm alias
entry is looked up under the registry package it installs, not its alias path.

| lockfile | entries | after a year's silence (silence alone) | and in a burst across lines, any age | and within the age limit (reported) |
|---|---|---|---|---|
| microsoft/vscode `package-lock.json` (b99bfafc0a36) | 1,422 | 289 | 15 | **1** (`test-exclude@7.0.2`) |
| npm/cli `package-lock.json` (b317f16c80df) | 993 | 230 | 9 | **1** (`minipass-flush@1.0.6`) |

Both remaining hits are legitimate: a final patch on the old line published
together with a new major. They are the cost of the rule. Both lockfiles are
tooling-heavy; other populations will differ.

## Consequences

- `pkg:npm/node-ipc@12.0.1` and its sibling versions are Review Needed until
  2027-05-14, with the reason "Possible hijacked release. This version came out
  after 639 days with no release, as part of a set published within a day on
  the 9.x, 12.x lines. Takeovers of npm packages that had gone quiet looked like
  this (node-ipc 2026, rc 2021), but so does a maintainer who returns and ships
  a last fix to an old line next to a new major. npm has since removed this
  version, so its publisher can no longer be looked up there; stay on the
  release before the silence unless the maintainer confirms this one." The
  reason names the risk, the harmless reading that looks the same (both
  measured false positives are that case), and what to do. For a version still
  on the registry the last sentence is instead "Before using it, check who
  published it (npm view <package>@<version> _npmUser) and what changed since
  the previous release."; npm drops `_npmUser` with a removed version.
- **The rule fires once the second line is published, not at the first.** On
  2026-05-14 it would have become true at 14:26:01, 31 seconds after 12.0.1.
- Every version of the burst is flagged, including the maintainer's clean
  follow-up when it lands inside the window (`is@3.3.2`, published 37 minutes
  after the malicious 5.0.0). The reason text cannot tell them apart; a person
  has to.
- Date-based versions (`20220101.0.0`, `20220102.0.0`) are separate majors, so
  two of them published within a day after a year's silence fire. Not seen in
  the measured lockfiles.
- The rule reads the version in the PURL. A GitHub-URL input is analysed at
  deps.dev's latest stable release, so it fires when that release is in a
  burst (an attack still in progress) and never for a version already removed,
  which deps.dev no longer lists. Today `node-ipc`'s repository URL evaluates
  14.0.0 and does not fire. A GitHub URL whose package is not found is analysed
  without a package and never reaches the rule.
- **An attacker can avoid the rule.** An attacker who publishes on one line only, or who first
  publishes a harmless release and waits more than a day, passes. event-stream
  did both: the attacker's clean 3.3.5 ended the silence, and the malicious
  3.3.6 came four days later on the same line. This is one signal, not a
  defence against takeovers.
- During an attack the reason may not appear at all. deps.dev does not list the
  new versions yet, so the tree judges the package by its old stable release
  and may reach Stalled or EOL-Effective instead of an ok label. The rule leaves
  those as they are, so `--fail-on stalled` still stops CI, but the output does
  not say "Possible hijacked release".
- The malicious versions' publish times stay in the history, so the next
  legitimate release (`node-ipc@14.0.0`, 102 days later) does not look like a
  return from silence.
- One extra full-packument request per distinct npm package per scan. A failed
  or timed-out fetch is logged at debug level only and leaves the label as the
  tree decided it.
- The lifecycle assessor now measures every age against one evaluation time
  (`AssessmentInput.Now`, wall clock when zero): release and commit recency,
  days since publish, and the burst's age limit. The burst rule also ignores
  releases published after that time. The inputs themselves (release info,
  repository state, scores) are whatever was fetched, so setting
  `AssessmentInput.Now` to a past date reproduces that date's result only when
  the release and commit data were also fetched on that date.
- Not covered, and left for later: other registries (RubyGems removes yanked
  versions from its API, so SleeperGem's malicious releases cannot be replayed);
  publisher changes (`_npmUser`), new dependencies and new install scripts in the
  burst, which are lost when the registry removes the version and so could not be
  checked against the incidents above.
