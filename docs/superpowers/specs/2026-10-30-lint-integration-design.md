# Lint integration — design and phased plan

Date: 2026-10-30
Status: Draft (direction reviewed independently by Claude Fable and Codex
gpt-6-astra; the document itself checked by Codex gpt-6-astra — see "Review record")

## Goal

Let teams see uzomuzo's dependency health verdicts where they already look for
problems — PR annotations, their existing linter, their pre-commit hooks — and
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

## Phases

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
    the SBOM file without a region (GitHub display behavior: open question 2).
- **Fingerprint**: `github/codeql-action/upload-sarif` fills
  `partialFingerprints` from source when missing, but direct REST uploads do
  not. Emit a key (rule + versionless PURL + file) ourselves; whether GitHub
  uses it for alert matching is open question 3.
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

Why a flag and not GitHub's diff filtering: GitHub hides alerts on lines
outside the PR diff, but (a) code scanning on private repositories needs a
paid GitHub Code Security license, (b) GitLab / Bitbucket / pre-commit users
get nothing, (c) SBOM-derived results have no line to be "in the diff", and
(d) GitHub docs do not state that off-diff alerts are excluded from the
check's pass/fail. Flag rationale per project-conventions: recurring
operational need that cannot be inferred.

The baseline is a file, not a git ref, so uzomuzo does not shell out to git;
wrappers supply it (`git show <base-sha>:go.mod > base.go.mod`).

Scheduled full scans (existing `dependency-scan.yml`) remain the way to catch
deterioration of dependencies nobody touched.

### P3 — Distribution: reusable GitHub Action + pre-commit hook

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
"Code scanning results" check. Document both levers separately.

**pre-commit** (`.pre-commit-hooks.yaml`): hook `uzomuzo-new-deps`, matching
`go.mod`, default `stages: [manual, pre-push]` (stage names need
pre-commit ≥ 3.2). Document `pre-commit install --hook-type pre-push` — the
stage setting alone does not install the git hook. The hook takes the
baseline from the upstream branch (`@{upstream}`); manual runs accept an
explicit baseline argument.

**Docs**: `docs/integration-examples.md` gains a "Lint / PR integration"
section with one copy-paste snippet per team type (table below).

### P4 — Snapshot for golangci-lint: `--format gomodguard-v2`

Emits a `linters.settings.gomodguard_v2.blocked` list (requires golangci-lint
≥ 2.12.2: `gomodguard` is deprecated since v2.12.0 in favor of
`gomodguard_v2`, and 2.12.2 fixes blocked-list handling). `match-type`
accepts `exact`, `prefix`, `regex`; we emit `exact`.

```yaml
# BEGIN uzomuzo generated 2026-10-30 (uzomuzo scan --fail-on eol-confirmed,eol-effective)
- module: github.com/pkg/errors
  match-type: exact
  recommendations: [errors]
  reason: "uzomuzo EOL-Confirmed: <evidence summary> (as of 2026-10-30)"
# END uzomuzo generated
```

golangci-lint has no config include, so the block is regenerated between
markers in the user's `.golangci.yml`. A scheduled workflow example
regenerates it and opens a PR, so verdict changes are reviewable diffs.
Limits to document: direct modules only; snapshot staleness between runs.

### P5 — ESLint snapshot: `--format eslint`

`no-restricted-imports` with, per package, an exact `paths[]` entry for the
package root **and** a `patterns[]` entry for its subpaths (`pkg/*`;
`@scope/pkg/*`), each with a `message`. Same BEGIN/END marker with
generation date and command as P4, and the same scheduled-regeneration
example. Source of npm packages is a CycloneDX SBOM (uzomuzo has no
package.json parser). Limits to document: dynamic `import()` and `require()`
are not checked by that rule.

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
| Any repo on GitHub (public, or private with Code Security) | `uses: future-architect/uzomuzo-oss@<sha>` in a PR workflow | PR annotations; uzomuzo fails only on added deps in `fail-on` | P1–P3 |
| Private GitHub repo without Code Security | Same Action with `upload-sarif: false` | Job exit code via `--baseline` | P2–P3 |
| Go team on golangci-lint | Generated `gomodguard_v2` block + scheduled regeneration workflow | Offline, in editor and CI | P4 |
| JS/TS team on ESLint | Generated `no-restricted-imports` entries + scheduled regeneration | Offline, static imports only | P5 |
| GitLab / Bitbucket | CLI in CI job; optionally reviewdog with SARIF | Exit code; MR comments via reviewdog | P1–P2 |
| pre-commit users / no CI | `.pre-commit-config.yaml` entry + `pre-commit install --hook-type pre-push` | Local, needs network | P3 |
| Monorepo | One Action step per manifest, distinct `category` each | Per manifest | P3 |

## Risks and controls

- **Changing answers / rate limits**: PR gating only on added dependencies;
  snapshot formats carry their generation date; unavailable evidence is
  reported as "unknown", never as healthy.
- **Heuristic labels blocking PRs**: default `fail-on` is empty; docs
  recommend `eol-confirmed,eol-effective` at most for gating, and point out
  the separate repository-level check threshold.
- **Snapshot staleness** (P4/P5): scheduled regeneration as a PR; date stamped
  in the marker line.
- **SARIF limits**: 25,000 results per run, 5,000 displayed; fine for single
  manifests, document for monorepos.

## What would change this plan

- Most adopters want offline Go enforcement first → move P4 before P1.
- Most adopters have no SARIF consumer (private repos without Code Security,
  non-GitHub) → ship P2 before P1.
- uzomuzo gains a package.json parser → npm results get line locations in P1.

## Open questions to verify during implementation

1. Does GitHub's "Code scanning results" check fail on `error` alerts that lie
   outside the PR diff? (Docs: such alerts are not *shown*; failure behavior
   not stated.)
2. How GitHub displays SARIF results without a `region` (SBOM-derived).
3. Whether GitHub uses our own `partialFingerprints` key for alert matching.

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
