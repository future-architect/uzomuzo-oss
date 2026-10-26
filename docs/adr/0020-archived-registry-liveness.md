# 0020. Archive/disable is not end-of-life without an explicit EOL signal

Date: 2026-06-23
Status: Accepted

## Context

The lifecycle assessor (`internal/domain/analysis/lifecycle_assessor.go`,
`assessInternal`) classified **any** archived or disabled repository as
`EOL-Confirmed`, unconditionally, in its "archive/disable check" branch.

That is wrong: an archived *repository* is not the same as an end-of-life
*package*. Two cases break the old rule:

1. **Monorepo consolidation** — the package keeps publishing new versions from a
   different repository while the original repo is archived. Verified:
   `pkg:npm/google-auth-library`, `pkg:npm/google-gax`,
   `pkg:golang/github.com/grafana/k6deps` are all archived on GitHub yet still
   ship recent releases.
2. **Long-dormant but installable** — a package whose repo is archived and which
   has not published in years (`pkg:npm/through`, `pkg:gem/cancan`) is still
   installable under the same PURL. Consumers receive whatever they already
   depend on; nothing forces a rename.

In both cases `IsArchived()==true`, `EOL.IsEOL()==false`, yet
`FinalMaintenanceStatus()` returned `EOL-Confirmed` and told consumers to
"migrate immediately" (issue #451).

`FinalMaintenanceStatus()` re-derives EOL directly from `a.EOL` first (via
`IsEOL()` / `IsPlannedEOL()`) and only falls back to the lifecycle-axis label
when both are false, so the false `EOL-Confirmed` surfaces precisely when
`EOL.IsEOL()==false`.

## Decision

`EOL-Confirmed` requires an **explicit primary-source EOL signal**; the archive
flag alone is not one.

In the archive/disable branch:

- archived/disabled **&&** `!in.EOL.IsEOL()` → **`Stalled`** ("frozen, but not
  declared end-of-life"). This holds **regardless of release recency** — a
  still-publishing package and a long-dormant one are both `Stalled`, not EOL.
- archived/disabled **&&** `in.EOL.IsEOL()` → `EOL-Confirmed`. `IsEOL()` is true
  only for an explicit primary-source signal: npm `deprecated`, PyPI `yanked`,
  Packagist `abandoned`, Maven `<relocation>`. Those, and only those, are
  end-of-life.

The gate is a one-directional narrowing (`EOL-Confirmed → Stalled`) keyed solely
on `!IsEOL()`. It cannot hide a genuine EOL: every explicit signal sets
`IsEOL()==true` and is preserved (verified — Packagist `abandoned` resolves to
`EOL.IsEOL()==true` even without a declared successor).

### Rejected alternative — a registry-liveness / recency gate

An earlier draft only downgraded archived packages that still ship a *recent
non-deprecated stable* (a `HasLiveStableRelease(window)` check), and kept the
rest `EOL-Confirmed` as a conservative default. Rejected: a long-dormant but
**installable** package is not end-of-life either — it has no explicit EOL
declaration and consumers can still use the same PURL. Only an explicit signal
should drive `EOL-Confirmed`, so the recency check is unnecessary and would
mislabel dormant-but-installable packages. The simpler `!IsEOL()` rule is both
correct and easier to reason about.

## Consequences

- **Behavior change**: archived/disabled packages with `EOL.IsEOL()==false` now
  resolve to `Stalled` (was `EOL-Confirmed`), whether they are actively
  publishing or long-dormant. Packages with an explicit primary-source EOL are
  unchanged.
- **Explainability**: the Stalled branch emits trace
  `repo_archived_or_disabled_not_eol` plus the `repo_archived` / `repo_disabled`
  signals; reason `"Repository archived/disabled but not declared end-of-life"`.
- **Downstream consumers**: any consumer that previously treated the archive flag
  as end-of-life should re-check after upgrading. The maintenance status now
  distinguishes "archived but not declared EOL" (`Stalled`) from an explicit EOL
  signal (`EOL-Confirmed`). A consumer that needs a finer verdict (e.g. "archived
  but the package still publishes under the same identifier") should layer its own
  judgment on top of this mechanical signal rather than expecting the assessor to
  make that call — the assessor intentionally stays mechanical and conservative.
- **No config or API change**: `assessInternal` is unexported; no new flags.
