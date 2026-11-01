# Lint integration — design and phased plan

Date: 2026-10-30
Status: Draft (direction reviewed independently by Claude Fable and Codex
gpt-6-astra; the document itself checked by Codex gpt-6-astra — see "Review record")

## Goal

Let teams see uzomuzo's dependency health verdicts where they already look for
problems — PR annotations and their pre-commit hooks — and
let them gate PRs on **dependencies the PR introduces**, without making every
PR fail on pre-existing debt.

## Constraint that shapes everything

uzomuzo's verdicts need the network (deps.dev, GitHub API, registries) and can
change without any code change. Example: a commit passes on Monday; on Tuesday
upstream publishes an EOL notice for `github.com/foo/bar`; the same commit now
fails. A linter is expected to be offline, fast, and give the same answer for
the same code. Therefore:

- **The network judgment stays in `uzomuzo scan`** (CI job or scheduled job).
- **Linters only enforce a snapshot** that uzomuzo produced and a human merged.

## Rejected: running uzomuzo inside a linter (golangci-lint module plugin, ESLint plugin)

- Live network calls during lint: latency, `GITHUB_TOKEN` on every laptop,
  rate-limit failures, answers that change between runs.
- golangci-lint module plugins need a custom golangci-lint binary
  (`golangci-lint custom` + `.custom-gcl.yml`). It can be built centrally and
  distributed, but someone must maintain and distribute that binary for every
  golangci-lint upgrade.

Revisit only if a deterministic offline data source exists (e.g. a dated,
signed catalog bundle shipped with the binary).

## Order (revised 2026-11-02)

An adoption review (see "Review record") found that the next problem is not a
missing format. The results and the setup path have to work first, so the order
is now:

1. **P0** — results people can trust, and CI instructions that work.
2. **P1** — `--format sarif` (PR #605), with its upload recipe.
3. **Measure** — scan 20 well-maintained Go and npm projects with the P0 binary
   and count the `Stalled` labels a maintainer would dispute.
4. **P3a** — a GitHub Action that reports and never fails the job.
5. **P2** — `--baseline`.
6. **P3b** — the Action's gate on added dependencies, and the pre-commit hook.

P4 and P5 are deferred: see "Deferred — linter snapshots".

If under 10% of the `Stalled` labels in step 3 are disputed, trust is not the
blocker, and the remaining steps should be re-ordered around the setup path and
non-Go input.

## Phases

### P0 — Trustworthy results and working setup

Measured on uzomuzo's own `go.mod` (14 direct dependencies) on 2026-11-01 and
2026-11-02:

| Dependency | Label without a token | What is actually true | Cause |
|---|---|---|---|
| `github.com/Masterminds/semver/v3` | Stalled, "Maintained but no commit data" | v3.5.0 was published on 2026-04-30 | Go path case, below |
| `gopkg.in/yaml.v3` | Stalled, "No vulnerabilities but no recent releases" | The repository is archived | Only a token run can read the repository state (ADR-0029) |

**Go module path case.** `packageurl-go` lowercases `pkg:golang` paths, and
deps.dev treats Go module names as case-sensitive:

- `github.com/masterminds/semver/v3` returns 404, so uzomuzo sees no releases.
- `github.com/burntsushi/toml` returns an old record with 111 versions, against
  323 for `github.com/BurntSushi/toml`, so uzomuzo reports v0.3.1 as the stable
  version when the latest is v1.6.0.

SBOMs can already hold the lowercase path (`testdata/diet/test-sbom.json`), so
keeping the input spelling is not enough. The Go proxy serves a lowercase path
and its `.mod` file states the real spelling (`module github.com/BurntSushi/toml`).

**Work in P0:**

- Look up Go modules on deps.dev by their real spelling.
- Warn on stderr when a scan runs without a token. `ConfigService.Validate`
  says a banner exists; it does not.
- Stop advising people to start without a token (`docs/usage.md`, `README.md`).
- Make the copy instruction in `docs/integration-examples.md` work. The workflow
  it points to runs `go build ./cmd/uzomuzo`, which fails in any other repository.

**Not in P0:** changing the labels of the "no commit data" branches in
`assessInactiveNoCommitData`. The only measured wrong label there was semver, and
the first item above is expected to remove it. Decide after the measurement,
using a binary that has the fix.

### P1 — `--format sarif`

SARIF 2.1.0 output from `uzomuzo scan`. No new flag; new value of `--format`.

- **Rules**: stable `ruleId`s covering everything `--fail-on` can trigger, not
  only lifecycle labels: lifecycle labels (`uzomuzo/eol-confirmed`, ...),
  malicious version, and the fact triggers (`archived`, `disabled`,
  `dormant-release`) — see `internal/domain/scan/policy.go` and ADR-0027.
- **Level**: derived from the actual `FailPolicy` evaluation of each entry,
  not from a parallel mapping: entry triggers the policy → `error`; verdict
  `replace` without triggering → `warning`; `caution` / `review` → `note`;
  `ok` → omitted. With `--baseline` (P2), only *added* dependencies can be
  `error`; existing ones are capped at `warning`.
- **Message**: label / fact, evidence summary, successor when known.
- **Location** — provenance must flow end to end, not only through the parser:
  `domain/depparser.ParsedDependency` gains an optional source position, and
  the application scan result (`internal/application/scan/service.go`, which
  today keeps only PURLs and relation metadata) must carry it to the renderer.
  - go.mod: keep `modfile.Require.Syntax.Start.Line`
    (`depparser/gomod/parser.go` discards it today).
  - GitHub Actions workflow YAML: parse via `yaml.Node` to keep each `uses:`
    line; the parser currently returns deduplicated URLs, so repeated
    references of the same action need one location each.
  - PURL list files: the reader already counts lines
    (`internal/interfaces/cli/input_helpers.go`); keep the line of each PURL.
  - CycloneDX SBOM: the SBOM is not where the dependency is declared, so no
    source location. Do **not** invent line numbers; emit the result against
    the SBOM file without a region. GitHub shows such a result at line 1 of
    the SBOM file (measured, see "Measured GitHub behavior").
- **Fingerprint**: `github/codeql-action/upload-sarif` fills
  `partialFingerprints` from source when missing, but direct REST uploads do
  not. Emit a key (rule + versionless PURL + file) ourselves; GitHub uses it
  for alert matching (measured, see "Measured GitHub behavior").
- A new ADR (next free number after ADR-0033) records the rule set, level
  derivation, and location policy.

### P2 — "new dependencies only": `--baseline <path>`

Compares the scanned inventory with a baseline inventory of the same input
kind (e.g. the base branch's `go.mod` or SBOM).

- **Identity**: canonical versionless package key (`CanonicalKey`, see
  `docs/purl-identity-model.md`).
- **Gating set** (can fail the run / be `error`): packages whose key is absent
  from the baseline.
- **Reporting-only set** (at most `warning`): packages present in both,
  including version changes.
- **Edge cases to specify in the ADR**: a package becoming direct after being
  transitive (treated as added for gating); `replace` directives (identity is
  the replacement target); baseline file missing (new manifest → everything
  is added) vs unreadable (error, never silently "no baseline").

Why a flag and not GitHub's diff filtering: an `error` on a line the PR did not
change does not fail GitHub's check (measured, see "Measured GitHub behavior"),
but (a) uzomuzo's own exit code is not filtered by GitHub, so a job using
`--fail-on` still fails on existing dependencies, (b) code scanning on private
repositories needs a paid GitHub Code Security license, (c) GitLab / Bitbucket /
pre-commit users get nothing, and (d) SBOM-derived results sit at line 1 of the
SBOM file, so they count as changed only when a PR changes that line. Flag
rationale per project-conventions: recurring operational need that cannot be
inferred.

The ADR must state one expected difference between the two gates. When a PR
bumps the version of a dependency that is already flagged, GitHub is expected
to treat the changed line as code the PR touched (not measured), while
`--baseline` caps that dependency at `warning`.

The baseline is a file, not a git ref, so uzomuzo does not shell out to git;
wrappers supply it (`git show <base-sha>:go.mod > base.go.mod`).

Scheduled full scans (existing `dependency-scan.yml`) remain the way to catch
deterioration of dependencies nobody touched.

### P3 — Distribution: reusable GitHub Action + pre-commit hook

P3 ships in two parts. **P3a** comes before P2: the Action with pinned install,
token setup and SARIF upload, with `fail-on` empty and no baseline. It reports
and never fails the job on existing dependencies, so it does not need P2. **P3b**
comes after P2: the baseline inputs below and the pre-commit hook.

**Action** (`action.yml` at repo root, composite):

- Caller prerequisites to document: `actions/checkout` with enough history to
  read the base SHA, and `permissions: security-events: write` for upload.
- Baseline: on `pull_request`, read the manifest at
  `github.event.pull_request.base.sha`; if absent there, run with an empty
  baseline (new manifest). Non-PR events (`push`, `schedule`): no baseline,
  full scan. SBOM inputs: the caller supplies both the head SBOM and a
  baseline SBOM path (the action does not generate SBOMs).
- Inputs: `file` / `sbom`, `baseline-sbom`, `fail-on` (default empty),
  `upload-sarif` (default true), `category`.
- Order of steps: run scan and capture its exit code → upload SARIF even if
  the scan failed the policy → exit with the captured code.
- Monorepos: one step per manifest, each with a distinct `category` and
  output path (GitHub requires distinct categories for separate analyses of
  the same commit).

Note: an empty `fail-on` keeps **uzomuzo's** exit code 0, but repository
code-scanning settings can still make `warning`/`note` alerts fail the
"Code scanning results" check. That check did not fail for an alert on a line
the PR did not change (measured); whether it fails on a changed line is open
question 1. Document both levers separately.

**pre-commit** (`.pre-commit-hooks.yaml`): hook `uzomuzo-new-deps`, matching
`go.mod`, default `stages: [manual, pre-push]` (stage names need
pre-commit ≥ 3.2). Document `pre-commit install --hook-type pre-push` — the
stage setting alone does not install the git hook. The hook takes the
baseline from the upstream branch (`@{upstream}`); manual runs accept an
explicit baseline argument.

**Docs**: `docs/integration-examples.md` gains a "Lint / PR integration"
section with one copy-paste snippet per team type (table below).

### Deferred — linter snapshots (golangci-lint, ESLint)

Two generated formats were planned as P4 and P5:

- `--format gomodguard-v2`: a `linters.settings.gomodguard_v2.blocked` list
  (golangci-lint 2.12.2 or later), regenerated between markers in `.golangci.yml`.
- `--format eslint`: `no-restricted-imports` entries with an exact path and a
  subpath pattern per package.

Both were dropped from the near-term plan. Nobody has asked for them, and a
generated blocklist copies any wrong label into every lint run. Reopen when an
adopter asks for offline enforcement and the step 3 measurement shows that the
labels are good enough to block on.

### Deferred — Ruff `banned-api`

Ruff bans import names, not PyPI distribution names. A reliable
distribution → import-name mapping is the prerequisite
(`internal/infrastructure/pypi/wheel.go`, `ResolveImportNames`; accuracy gaps
tracked in #548). A pyproject parser alone would not remove this prerequisite.

### Deferred — GitLab Code Quality JSON

GitLab's Code Quality report wants CodeClimate JSON, not SARIF. Until asked
for, GitLab users use the exit code from P2 or reviewdog `-f=sarif` with the
`gitlab-mr-discussion` reporter.

## How each kind of team adopts it

| Team | What they add | Gate | Phase |
|---|---|---|---|
| Any repo on GitHub (public, or private with Code Security) | `uses: future-architect/uzomuzo-oss@<sha>` in a PR workflow | PR annotations first (P3a); uzomuzo fails only on added deps in `fail-on` (P3b) | P1–P3b |
| Private GitHub repo without Code Security | Same Action with `upload-sarif: false` | Job exit code via `--baseline` | P2–P3b |
| GitLab / Bitbucket | CLI in CI job; optionally reviewdog with SARIF | Exit code; MR comments via reviewdog | P1–P2 |
| pre-commit users / no CI | `.pre-commit-config.yaml` entry + `pre-commit install --hook-type pre-push` | Local, needs network | P3b |
| Monorepo | One Action step per manifest, distinct `category` each | Per manifest | P3a |

## Risks and controls

- **Changing answers / rate limits**: PR gating only on added dependencies;
  snapshot formats carry their generation date; unavailable evidence is
  reported as "unknown", never as healthy.
- **Heuristic labels blocking PRs**: default `fail-on` is empty; docs
  recommend `eol-confirmed,eol-effective` at most for gating, and point out
  the separate repository-level check threshold.
- **SARIF limits**: 25,000 results per run, 5,000 displayed; fine for single
  manifests, document for monorepos.

## What would change this plan

- Most adopters want offline Go enforcement first → reopen the deferred
  golangci-lint snapshot.
- Most adopters have no SARIF consumer (private repos without Code Security,
  non-GitHub) → ship P2 before P1.
- uzomuzo gains a package.json parser → npm results get line locations in P1.

## Measured GitHub behavior

Measured on 2026-10-30 by uploading SARIF from the P1 branch through the REST
API to this repository (`main` and PR #605), then deleting every uploaded
analysis. `main` received a `go.mod` result set without `gopkg.in/yaml.v3`; the
PR received the same set with yaml.v3 as `error` on `go.mod:17`. The PR does not
change `go.mod`.

- An `error` that is new against `main`, on a line the PR did not change, does
  not fail the check. The check passed with "No new alerts in code changed by
  this pull request".
- A result without a `region` (SBOM-derived) is accepted and shown at line 1 of
  the SBOM file.
- GitHub uses our `partialFingerprints` key. After the yaml.v3 result was moved
  to line 18 and uploaded again, the same alert number moved to line 18; no
  alert was opened or closed.

## Open questions to verify during implementation

1. Does GitHub's check fail on an `error` alert on a line the PR changed? If it
   does not, `--baseline` is the only gate.
2. What does GitHub do with a PR that bumps the version on the line of an
   already-flagged dependency, or that changes an SBOM file?
3. How does GitHub display results with no `locations` (transitive
   dependencies)?

## Review record

- Direction (before the plan was written), with the author's preference
  withheld:
  - Codex gpt-6-astra: order SARIF → new-only → linter snapshots →
    pre-commit; reject live plugins. Flagged `gomodguard` deprecation
    (verified in golangci-lint `pkg/lint/lintersdb/builder_linter.go`).
  - Claude Fable: SARIF first, snapshots second; proposed relying on GitHub
    diff-anchoring instead of a new-only flag. Not adopted: anchoring needs a
    paid license on private repos and does not cover non-GitHub or SBOM inputs.
- Document check (Codex gpt-6-astra, 15 findings, all applied): ADR number,
  provenance through the application layer, PURL-list line numbers, full
  `--fail-on` trigger set, P2 gating vs reporting sets, Action baseline /
  permissions / categories, repository check thresholds, fingerprint
  fallback scope, gomodguard_v2 `match-type` and minimum version, ESLint
  subpaths, pre-commit install, Ruff prerequisite, plugin distribution.
- P2 re-check after the measurements above (Claude Fable, given the evidence and
  both positions, with the author's preference withheld): keep P2 and build it
  before P3. P3's baseline step and pre-commit hook depend on it, and the exit
  code is not filtered by GitHub. This would flip if the owner limits gating to
  public or licensed GitHub repositories and accepts no gating for SBOM and
  transitive inputs; P2 would then be postponed and P3 redesigned without the
  hook.
- Adoption review on 2026-11-01 (Claude Fable and Codex gpt-6-astra, same brief).
  Both predicted narrow adoption as it stands, ranked trust and a working setup
  path first, and advised dropping P4 and P5. They differed on P2's position, and
  this plan settles that by splitting P3. The maintainer picked this order on
  2026-11-02.
- P0 label check on 2026-11-02 (Claude Fable, author's preference withheld):
  recommended changing the one branch that produced semver's label. Not adopted
  for now: semver's label came from the Go path case bug, which Fable listed as a
  condition that would change its view.
