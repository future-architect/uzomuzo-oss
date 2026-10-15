# 0029. Archived and disabled are facts next to the label, with their own --fail-on values

Date: 2026-10-15
Status: Proposed

## Context

[ADR-0020](0020-archived-registry-liveness.md) keeps an archived or disabled
repository out of EOL unless a primary source says so; such a repository is
usually labelled Stalled. The verdict, however, turns an archived repository
into `replace` whenever its analysis succeeded (`internal/domain/audit/verdict.go`;
an analysis error makes it `review` first). Before this change the
table therefore showed two rows that differ only in their status, with nothing
saying why (v0.7.0, 2026-10-15):

```
STATUS      SOURCE  PURL                                        LIFECYCLE  BUILD
🔴 replace  action  https://github.com/tibdex/github-app-token  Stalled    —
⚠️ caution  action  https://github.com/actions/github-script    Stalled    —
```

Only `tibdex/github-app-token` is archived. The fact was in JSON (`archived`)
and the detailed view, but not in the table or CSV, and no `--fail-on` value
selected it.

This matters most for GitHub Actions. An Action has no registry, so no
primary-source EOL signal exists and `--fail-on eol-confirmed` never fires for
one. `--fail-on stalled` catches archived Actions but also fails quiet ones: in
a scan of grafana/grafana's workflows (2026-10-15, v0.7.0), four Actions were
Stalled, none archived, `actions/github-script` among them.

[ADR-0027](0027-osv-malicious-advisories.md) and
[ADR-0028](0028-dormant-release-marker.md) already carry a fact next to the
label instead of changing it. ADR-0027 describes this as "the way `archived`
already is", but archived was the one such fact without a table marker or a gate.

## Decision

Archived and disabled are repository facts carried next to the lifecycle label.
The label does not change.

- **Gate.** `--fail-on archived` fails when `Analysis.IsArchived()` is true,
  `--fail-on disabled` when `Analysis.IsDisabled()` is true, whatever the label
  or verdict, including entries whose analysis failed. Neither is implied by any
  other value, and neither implies the other. Like every non-empty selection,
  each also fails on a malicious version (ADR-0027). They are evaluated over all
  entries, independent of `--show-only`.
- **Table.** The LIFECYCLE cell gets ` · archived` and/or ` · disabled`, after
  any malicious (`☠`) or dormant-release (`⚠`) marker. They are appended rather
  than competing for the cell, because an archived repository that suddenly
  publishes is exactly when both matter. Plain words, not `📦`: a double-width
  glyph breaks tabwriter alignment.
- **JSON.** `lifecycle` stays the bare label. `archived` stays, and `disabled`
  is added; both are always serialized.
- **CSV.** `archived` and `disabled` columns at the end of each row.

## Alternatives considered

- **`Stalled (archived)` as the label string.** The label is library API and what
  `--fail-on stalled` matches; changing it breaks both.
- **An `Archived` lifecycle state.** Rejected by ADR-0020.
- **`--fail-on replace`.** It would mix the verdict vocabulary into a flag of
  labels and facts, and its meaning would drift as new causes of `replace` are
  added. A separate `--fail-on-verdict` flag is tracked in #541.

## Consequences

- CI can gate on archived Actions without also failing quiet ones, and without
  `--format json | jq`.
- CSV rows grow by two columns. Readers that index columns by position from the
  end must be updated; readers that use the header are unaffected.
- Open questions this ADR leaves as they are (#540):
  - `verdict.go` maps archived to `replace`, while ADR-0020 says archive alone
    is not EOL. That is defensible for Actions, where the repository is the
    distribution channel, and weaker for registry packages (e.g.
    `pkg:npm/through@2.3.8`: finished, no advisories, archived).
  - A disabled repository gets `caution`, since the verdict only checks
    `IsArchived()`, although `RepoState` documents disabled as more severe.
  - `replace` blanks the BUILD column, hiding build-integrity data for
    archived libraries that are otherwise fine.
  - `uzomuzo diet` shows `Archived` as a STATUS value by overwriting the label,
    while scan never does.
