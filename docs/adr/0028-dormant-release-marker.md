# 0028. A return from silence is a marker next to the label, and a fresh one counts on any line

Date: 2026-10-09
Status: Proposed

## Context

[ADR-0026](0026-dormant-release-burst.md) flags a release that comes after a year
without releases, in a burst across at least two release lines. It reports this
by turning an Active or Legacy-Safe label into Review Needed. Two problems
showed up when the rule was reviewed against a larger set of incidents.

**The signal disappears when the label is not ok.** During an attack, deps.dev
has not yet listed the new version, so a package that had gone quiet is often
labelled Stalled or EOL-Effective. ADR-0026 leaves those labels alone so that
`--fail-on stalled` keeps working. As a result, the reason never mentions the
burst, and a CI job that gates on `review-needed` passes at exactly the moment
the burst matters. ADR-0026 records this in its Consequences.

**Most takeovers of quiet packages touch one line.** The OpenSSF
malicious-packages data (commit `5d18840712`, 2026-10-08) has npm records that
name only some versions of a package. 1,556 of them are packages that existed
for at least a year and had at least three non-malicious releases before the
first malicious one. In 737 of those (47%), the malicious version came at least
365 days after the previous non-malicious release. Only 58 of the 1,556 touched
two or more lines within a day. The 737 are not 737 incidents: 724 fall in 13
bulk account-compromise waves (Shai-Hulud and others), and 13 are single
incidents. They include the packages hit in the 2025-09-08 chalk incident
(`ansi-styles`, `strip-ansi` and others), which ADR-0026 classed as takeovers of
active packages; at the package level, those packages had been quiet for years.

The 47% is not evidence that attackers prefer quiet packages. At any date,
32–53% of the same packages had been quiet for a year (the compromised accounts
owned many quiet packages). What it supports is narrower: per release, a return
from a year of silence is rare among normal releases (3.1–5.5% of the releases of
the packages in two real lockfiles, below) and common among malicious ones.

[ADR-0027](0027-osv-malicious-advisories.md) replaces versions named in a
malicious-package advisory. An advisory needs time to appear. Measured as the
time from the first malicious publish to the OSV record, the median over the
1,556 packages is about 6 hours, 89% appear within a day and 98–99% within 7
days. Outside the bulk waves the tail is longer: 5 of the 13 single incidents
took more than a day.

## Decision

### The marker

`Analysis.DormantRelease` records that the analysed version returned from
silence. It is computed after enrichment, for every analysis that has a release
history, including one whose deps.dev lookup failed. It is carried next to the
lifecycle label in the same way as ADR-0027's malicious state, so it reaches
the output whatever the label is.

| Kind | Condition | Flagged for |
|---|---|---|
| `burst_across_lines` | ADR-0026: a burst touching ≥ 2 lines after ≥ `DormantSilenceDays` | `DormantSilenceDays` after publication |
| `fresh` | a stable version from the first burst after ≥ `DormantSilenceDays`, on any number of lines | `DormantFreshDays` after publication |

When both match, `burst_across_lines` wins. The silence is measured at the
start of the burst. A follow-up released the next day therefore carries the
same silence and is flagged too, instead of looking like a release one day
after the previous one. A prerelease is never flagged as `fresh`.

ADR-0026's label change stays as it was: an Active or Legacy-Safe outcome with
a burst across lines still becomes Review Needed. The marker adds to it and
does not replace it.

### What the user sees

| Where | Effect |
|---|---|
| table | `Stalled ⚠ 639d silence` in the LIFECYCLE cell; the malicious `☠` takes the cell when both apply |
| detailed view | `⚠ Release after silence: <reason>` above the lifecycle line, unless the lifecycle reason already explains the burst or a malicious advisory names the version |
| JSON | `dormant_release` object (`kind`, `published_at`, `silent_days`, `burst_versions`, `lines`, `removed`, `until`) |
| CSV | trailing `dormant_release` column with the kind |
| `--fail-on dormant-release` | fails on any marked version, whatever its label |

The `fresh` reason says what the OSV check found. If the check came back
clean, it says "No OSV malicious-package record names it yet". If the lookup
failed, it says that the check did not complete. The reason also gives the date
the marker clears.

### `--fail-on dormant-release` is opt-in

Unlike `malicious`, no other `--fail-on` value implies it. A malicious advisory
is a public finding about one version. A return from silence is a heuristic:
on the two lockfiles below, a team that adopts every release within a week
would see the `fresh` rule fire 186 and 146 times a year, and none of those
releases is known to be malicious.

### Why 7 days

The 7 days are not the time until a malicious version is removed. npm does not
record removal times, and some malicious versions are never removed: 30 of the
1,556 packages still serve one. The 7 days are the time an OSV record usually
takes to appear: 98–99% of the 1,556 had one within 7 days. Once a record
appears, ADR-0027 takes over and the version becomes `replace`. A version that
reaches 7 days without a record loses the `fresh` marker. The marker therefore
covers the gap before the advisories have had time to look.

### Thresholds

The settings `DormantSilenceDays` (365) and `DormantFreshDays` (7) are separate
from `RecentStableWindowDays`. Under ADR-0026, `RecentStableWindowDays` set the
lifecycle recency check, the silence and the burst's flag period all at once,
so changing one moved the others. 365 days is not an optimum, and the trade-off
has no knee:

| silence ≥ | takeovers matched / 1,556 | VS Code releases flagged / year | npm CLI releases flagged / year |
|---|---|---|---|
| 90 days | 926 | 448 | 398 |
| 180 days | 823 | 294 | 263 |
| 365 days | 737 | 162 | 134 |
| 730 days | 529 | 81 | 52 |

This table measures each version's gap to the release before it. The 186 and
146 elsewhere in this ADR measure from the start of the burst, as the rule does.
365 days is kept because uzomuzo already uses "a year without a release" as one
idea elsewhere.

Like the other lifecycle thresholds, these two are read from the defaults. The
scan path builds the assessor with `NewLifecycleAssessorService()` and never
reads a user's lifecycle configuration. That gap predates this change and is
not fixed here.

## Evidence

Lockfile figures come from `go run ./scripts/dormant-burst lockfile -now
2026-10-08 <package-lock.json>`. Incident figures come from `go run
./scripts/dormant-burst cases scripts/dormant-burst/cases.json`.

| lockfile | stable releases in the year before 2026-10-08 | flagged by `fresh` | entries flagged on 2026-10-08 with a 7 / 30 / 90-day period |
|---|---|---|---|
| microsoft/vscode `package-lock.json` (b99bfafc0a36) | 6,083 | 186 | 0 / 1 / 5 |
| npm/cli `package-lock.json` (b317f16c80df) | 2,637 | 146 | 0 / 0 / 0 |

In-situ precision on these two lockfiles is 0 of 186 and 0 of 146: none of the
flagged releases is known to be malicious.

| incident (`cases.json`) | ADR-0026 | `fresh` |
|---|---|---|
| node-ipc, rc, coa, is | yes | yes |
| event-stream (4-day silence), rand-user-agent (195 days) | no | no |
| colors 1.4.1 (the maintainer's own sabotage) | no | **yes** |
| faker, and 7 takeovers of active packages (chalk 5.6.1, axios 1.14.1, …) | no | no |

On the named incidents, `fresh` adds no takeover that ADR-0026 missed. What it
adds is in the bulk waves: single-line releases of quiet packages, such as the
chalk-incident packages above.

The corpus counts, the removal-lag proxies and the per-incident primary
sources were measured outside this repository. They are kept with the talk
material in vuls-lab (`events/ows-2026/data/`).

### Prior art

Waiting before adopting a new release is established practice:

- pnpm `minimumReleaseAge`, on by default (1 day) from v11 (<https://pnpm.io/settings/dependency-resolution>)
- npm CLI `min-release-age`, added in 11.10.0 (<https://docs.npmjs.com/cli/v11/using-npm/config>)
- Dependabot `cooldown`, 3 days by default for version updates (<https://docs.github.com/en/code-security/reference/supply-chain-security/dependabot-options-reference>)
- Renovate `minimumReleaseAge` (<https://docs.renovatebot.com/key-concepts/minimum-release-age/>)

Socket's alert types include "Unmaintained" and "Recently published" but not a
return from silence (<https://docs.socket.dev/docs/alert-types>). Zahan et al.,
"What are Weak Links in the npm Supply Chain?" (<https://arxiv.org/abs/2112.10165>),
found maintainer accounts that can be taken over through expired e-mail
domains. node-ipc was taken over that way.

The difference from these tools is scope. Their cool-downs apply to every
release. This marker selects the 3–6% of releases that return from silence,
labels them and leaves blocking to the user. It does nothing for takeovers of
packages that were never quiet: there, the cool-downs above and ADR-0027 are
the defence.

## Consequences

- A burst across lines shows up under every label. `--fail-on dormant-release`
  stops CI for it even when the label is Stalled.
- A release from a quiet package is marked for a week, and the marker's text
  says whether an OSV record exists yet.
- About 3–6% of the releases in the two measured dependency sets are marked during
  their first week. The marker does not change the verdict, and it fails a scan
  only if the user selects `dormant-release`.
- The trigger condition does not cover takeovers of packages that were never
  quiet, such as axios 1.14.1 (3 days after the previous release). Nor does it
  cover a takeover published within the silence threshold, such as
  rand-user-agent (195 days). The marker does flag a maintainer who returns
  after a year to sabotage their own package (colors 1.4.1).
- "Wait a week and it is gone" is not claimed. Some malicious versions stayed
  up for days (eslint-config-prettier, about 59 hours; part of Shai-Hulud 2.0,
  more than 65 hours). Others are still served today.
- The marker is computed from the registry's publish times, not from
  deps.dev, so it is available for npm only.
