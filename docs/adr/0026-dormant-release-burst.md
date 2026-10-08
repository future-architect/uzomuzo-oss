# 0026. A release burst across lines after a long silence needs a human

Date: 2026-10-08
Status: Proposed

## Context

uzomuzo judges the project behind a package: is it maintained, frozen, or ended.
That judgement says nothing about whether a particular release came from the
maintainer. Takeovers of dormant packages exploit exactly that gap: the project
looks frozen-but-fine, the attacker publishes a new version, and the new version
makes the project look *active*.

`pkg:npm/node-ipc@12.0.1` is the concrete case. In May 2026 an attacker
re-registered the expired e-mail domain of a co-maintainer whose account had been
idle for 1,476 days, reset the npm password, and published three versions
(<https://depi.security/blog/20260514-node-ipc-compromised/>). Before this change
uzomuzo reported that version as:

```
✅ ok       pkg:npm/node-ipc@12.0.1  Active
│ ✅ Active: Actively maintained with recent releases
```

The malicious release itself was the "recent release".

npm keeps the publish time of a version in the packument's `time` object after
the version is unpublished, so the shape of the incident is still readable from
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

Add branch 1.2 to the lifecycle assessor: when the analysed version was published

1. after at least `RecentStableWindowDays` (365 by default) without any release,
2. in a burst — every release within `DormantBurstWindow` (24 hours) of the first
   release after the silence — that touches at least `DormantBurstMinLines` (2)
   release lines (the major version, or `0.minor` below 1.0), and
3. no more than `RecentStableWindowDays` ago,

the label is **Review Needed**, with the silence, the lines and the burst in the
reason and signals. If the registry no longer serves the version, that is added
to the reason for display; it is never part of the decision.

npm only. The publish times come from the full packument
(`GET https://registry.npmjs.org/<name>`, one request per distinct package,
20-second timeout); the abbreviated install document carries no per-version
times. A failed fetch leaves `Analysis.ReleaseHistory` nil and the branch silent.

### Why lines, and not the silence alone

A single release after a year of silence is ordinary. Mature small packages ship
a fix every few years (`braces` 3.0.3 came five years after 3.0.2). Measured on
two real lockfiles, the silence alone matched about a quarter of all entries
(VS Code: 331 of 1,353; npm CLI: 256 of 968, first prototype, 2026-10-08) and is
unusable.

What separates the takeovers is that the attacker publishes to *every line in
use*, so that `^9` and `^12` users both resolve to a malicious version. A
maintainer returning after years usually ships one release.

### Why `RecentStableWindowDays`, and not `EolInactivityDays`

The burst is a return from the state this assessor already calls "no recent
stable release", so it reuses that threshold instead of inventing one.
`EolInactivityDays` (730) would have missed node-ipc (639 days). The same value
bounds the age: the signal describes the moment of publication, and a version
that has stayed published for a year is no longer that moment. Without the age
limit, legitimate bursts from years ago (`extend` 3.0.2 and 2.0.2 in 2018) would
stay in Review Needed forever.

### Why Review Needed, and not a new label or verdict

A person has to decide whether the release is the maintainer's; nothing in the
registry data says so. Review Needed is the existing label for that, and
`--fail-on review-needed` (#506) already turns it into a CI gate. A verdict-only
change was rejected because `--fail-on` reads the lifecycle label.

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
| event-stream@3.3.5 (2018-09) | dormant takeover | no — one line | | |
| rand-user-agent@2.0.83 (2025-04) | dormant takeover | no — 195-day silence | | |
| colors@1.4.1, faker@6.6.6 (2022-01) | maintainer's own release | no | | |
| ua-parser-js, eslint-scope, eslint-config-prettier, chalk, debug, axios, @solana/web3.js | takeover of an active package | no (by design) | | |

4 of the 6 dormant takeovers. None of the 7 active-package takeovers, which is
the intended scope: those packages were never silent.

### False positives on real lockfiles, evaluated on 2026-10-08

| lockfile | entries | burst after silence, any age | also within the age limit |
|---|---|---|---|
| microsoft/vscode `package-lock.json` (b99bfafc0a36) | 1,414 | 15 | **1** (`test-exclude@7.0.2`) |
| npm/cli `package-lock.json` (b317f16c80df) | 993 | 9 | **1** (`minipass-flush@1.0.6`) |

Both remaining hits are legitimate: a final patch on the old line published
together with a new major. They are the cost of the rule. Both lockfiles are
tooling-heavy; other populations will differ.

## Consequences

- `pkg:npm/node-ipc@12.0.1` and its sibling versions are Review Needed until
  2027-05-14, with the reason "Released after 639 days without a release, in one
  burst across release lines 9, 12; this version has since been removed from the
  registry".
- **The rule fires once the second line is published, not at the first.** On
  2026-05-14 it would have become true at 14:26:01, 31 seconds after 12.0.1.
- **It is evadable.** An attacker who publishes on one line only (event-stream)
  passes. This is one signal, not a defence against takeovers.
- The malicious versions' publish times stay in the history, so the next
  legitimate release (`node-ipc@14.0.0`, 102 days later) does not look like a
  return from silence.
- One extra full-packument request per distinct npm package per scan.
- Not covered, and left for later: other registries (RubyGems removes yanked
  versions from its API, so SleeperGem's malicious releases cannot be replayed);
  publisher changes (`_npmUser`), new dependencies and new install scripts in the
  burst, which are lost when the registry removes the version and so could not be
  checked against the incidents above.
