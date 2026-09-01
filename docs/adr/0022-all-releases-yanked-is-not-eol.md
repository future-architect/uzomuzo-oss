# 0022. Every release yanked is a package-level fact, and it is not end-of-life

Date: 2026-09-01
Status: Accepted

## Context

[ADR-0021](0021-yank-is-version-specific.md) decided that a yank is evidence about
the version that was yanked and about nothing else, which made `applyRegistryYanked`
a no-op for unversioned PURLs. That leaves the complementary case unanswered: when
**no non-yanked release is left**, the registry *is* making a statement about the
package. Nothing in the codebase detected it.

Found while verifying #489 against the 22 packages the downstream catalog had
flagged. Four are in this state (verified live 2026-08-31):

| package | releases | all yanked | label before this change |
|---|---|---|---|
| `pkg:pypi/conda` | 19 | 19 | Stalled |
| `pkg:pypi/conda-build` | 4 | 4 | Stalled |
| `pkg:pypi/python-apt` | 2 | 2 | Review Needed |
| `pkg:cargo/normal` | 1 | 1 | **Legacy-Safe** (rendered as a green "ok") |

`pkg:cargo/normal` has no installable release at all and was reported as healthy.

Before #489 these four came out `EOL-Confirmed`, because the removed
`ReleaseInfo.StableVersion` fallback happened to pick a yanked release when every
release was yanked. That was right by accident: the same fallback produced false
positives for the other 16 packages, which is what #489 fixed. This is therefore
not a regression — it is a rule that never existed.

The two registries expose the fact differently:

- **PyPI** — `GET /pypi/{name}/json` reports `info.yanked`. Warehouse selects the
  release it reports under `info` with `.order_by(Release.yanked.asc(),
  Release.is_prerelease.nullslast(), Release._pypi_ordering.desc())`
  (`warehouse/legacy/api/json.py`), so a non-yanked release always wins when one
  exists. `info.yanked == true` is therefore equivalent to "every release row is
  yanked". `info.yanked_reason` may be null even so (`conda-build`), and it is
  free text written by the package maintainer, so it is stripped of control and
  format characters and collapsed to one line before it reaches the domain — the
  CLI prints it verbatim, and either class can repaint or visually reorder it. The same PyPI client instance and its cache are shared by
  `enrichPyPISummary`, which runs earlier in the same pass for packages with a
  resolved repository, and by `applyPyPIClassifier`, which runs later during EOL
  evaluation; whichever fires first pays for the fetch.
- **crates.io** — `GET /api/v1/crates/{name}` carries a top-level `crate.yanked`
  that the server defines as "every published version is yanked". The empty
  `include=` parameter suppresses the versions array, cutting a popular crate's
  response from ~440 KB to under 1 KB.

## Decision

A registry reporting that **every** published release is yanked is a package-level
fact, and its outcome is **`Review Needed`** — not `EOL-Confirmed`, and not
`Stalled`.

- The fact is stored on the aggregate as `Analysis.RegistryState`
  (`internal/domain/analysis/models.go`), a value object distinct from `RepoState`.
  The two describe different things and can disagree: `conda`'s repository is
  actively developed while its PyPI distribution is fully withdrawn.
- It is populated in the infrastructure layer by `enrichRegistryState`
  (`internal/infrastructure/integration/populate_registry_state.go`), alongside the
  existing `RepoState.IsArchived` population and before assessment runs. A
  successful fetch always writes `RegistryState`, including the
  `AllReleasesYanked = false` case; `nil` means "not fetched, failed, or an
  ecosystem we do not ask".
- `LifecycleAssessorService.assessInternal` reads it in a branch placed after the
  primary-source EOL override and **before** the archived/disabled branch, and
  returns `LabelReviewNeeded` with the registry's yank reason as the assessment
  `Reason` (`All releases yanked on PyPI: Unmaintained`).
- Nothing is written to `EOLStatus`. `State` stays non-EOL and no evidence is
  appended.

### Why not `EOL-Confirmed`

"Do not install this from here" and "this project has ended" are different claims.
`python-apt`'s yank reason is literally `Unmaintained`, but `conda`'s is "pip
installing conda leads to broken UX; please install using miniconda or miniforge" —
conda is very much alive and simply not meant to be installed from PyPI. Nothing in
the yank mechanism distinguishes the two, and [ADR-0020](0020-archived-registry-liveness.md)
reserves `EOL-Confirmed` for an explicit primary-source end-of-life signal. Emitting
`Review Needed` states exactly what is known: the package cannot be installed from
its registry, and a human has to decide what that means.

### Why not `Stalled`

`Stalled` means "development has slowed or ceased" and its rationale explicitly
assumes the package "remains installable under the same PURL". Full withdrawal
falsifies that assumption, and `conda` shows the label can be plainly wrong: an
actively developed project would be reported as stalled.

### Why not a new label

A new `MaintenanceStatus` value (e.g. `Withdrawn`) is the most precise answer, but
`MaintenanceStatus` is part of the public API in `pkg/uzomuzo` and is consumed
downstream. Adding a value forces every consumer's switch to handle it. `Review
Needed` already means "a human must look at this", which is the accurate outcome.

### Depends on ADR-0021 landing first

Until [ADR-0021](0021-yank-is-version-specific.md)'s change is on `main`,
`applyRegistryYanked` still falls back to `ReleaseInfo.StableVersion` for an
unversioned PURL. That fallback resolves to a yanked release for exactly the
packages this ADR is about, so the EOL branch fires first and the withdrawal
branch is never reached. The two changes are independent in code and touch no
common file, but the order matters: this decision only takes effect once ADR-0021
has landed.

### Relationship to ADR-0021

ADR-0021 rejected routing the unversioned-yank case to `Review Needed`, on
**layering grounds**: the evaluator returns `EOLStatus`, which cannot express a
lifecycle label, so the evaluator must not request one. That decision stands. This
ADR detects a *different fact*, stores it on the aggregate, and lets the lifecycle
assessor — the actor ADR-0021 named as entitled to decide `Review Needed` — read it.
Versioned PURLs are untouched: a user pinned to a yanked release still resolves to
`EOL-Confirmed` through `applyRegistryYanked`, and that branch runs first.

### Collisions

- **An explicit primary-source EOL wins.** A `Development Status :: 7 - Inactive`
  classifier, or a pinned-and-yanked version, is a stronger and more specific claim.
- **An archived repository does not change the lifecycle label**, but it still
  short-circuits `deriveLifecycleVerdict` (`internal/domain/audit/verdict.go`) to
  `Replace`, as it does for every other label. That is pre-existing archive
  behavior, not a statement about the yank.
- **The residual-vulnerability override is suppressed.** A fully yanked, dormant
  package with unpatched advisories now reports `Review Needed` instead of
  `EOL-Effective`. The archived branch already masks that override the same way;
  the behavior is pinned by test rather than left to chance.

## Consequences

- **Behavior change**: `pkg:cargo/normal` moves off `Legacy-Safe`, and the audit
  verdict for it moves from `OK` to `Review`. `conda` and `conda-build` move off
  `Stalled`. Consumers that stored the old label must re-derive.
- **Cost**: for a PyPI package whose repository was resolved, no extra request —
  `enrichPyPISummary` has already fetched and cached the same project endpoint in
  the same pass. For one with no resolved repository, which `enrichPyPISummary`
  skips, this is the first fetch of that endpoint and a genuine additional
  request. One sub-kilobyte request per cargo package. The cargo fetch is not
  restricted to unversioned PURLs, because `AnalyzeFromGitHubURL` synthesises a
  version from the deps.dev stable release and reaches the same enrichment
  through `FetchAnalysis` — gating on "unversioned only" would drop the fact for
  that entry path alone.
- **Concurrency**: lookups are deduplicated by ecosystem and lowercased package
  name and run under a bounded worker pool, with dispatch stopping on context
  cancellation.
- **Known limitation**: a project whose newest non-yanked release has no files left
  reports `info.yanked = false` on PyPI. That is a different condition ("no
  installable file") and is not detected here. Deriving it would mean walking every
  release's files, and a project's JSON can reach several megabytes (numpy: 3.6 MB),
  so the cheap fact is preferred over the exhaustive one. The failure mode is a
  missed detection, never a false one.
- **Not addressed here**: a package that deps.dev cannot find keeps its analysis
  error and is skipped before assessment, so the withdrawal branch is never reached
  for it. `AnalyzeFromGitHubURL`'s synthesised version is also still treated as the
  "requested version" by the version-yank rules, so a fully yanked package analysed
  from a GitHub URL resolves to `EOL-Confirmed` rather than `Review Needed`. Both
  are tracked separately.
