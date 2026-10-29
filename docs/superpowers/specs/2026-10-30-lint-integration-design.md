# Lint integration — design and phased plan

Date: 2026-10-30
Status: Draft (direction reviewed independently by Claude Fable and Codex gpt-6-astra; see "Review record")

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
- golangci-lint module plugins require every developer and CI runner to build
  a custom golangci-lint binary (`golangci-lint custom` + `.custom-gcl.yml`).

Revisit only if a deterministic offline data source exists (e.g. a dated,
signed catalog bundle shipped with the binary).

## Phases

### P1 — `--format sarif`

SARIF 2.1.0 output from `uzomuzo scan`. No new flag; new value of `--format`.

- **Rules**: one SARIF rule per lifecycle label (`uzomuzo/eol-confirmed`,
  `uzomuzo/eol-effective`, `uzomuzo/eol-scheduled`, `uzomuzo/stalled`, ...).
  `ruleId` must stay stable across runs (GitHub matches alerts by it).
- **Level**: labels in the `--fail-on` set → `error`; verdict `replace`
  outside the set → `warning`; `caution` / `review` → `note`; `ok` → omitted.
- **Message**: label, the evidence summary, and the successor when known
  (`Replace with errors (stdlib)`).
- **Location**:
  - go.mod: keep `modfile.Require.Syntax.Start.Line` in the parser
    (`internal/infrastructure/depparser/gomod/parser.go` currently discards it).
    Requires adding an optional position to `domain/depparser.ParsedDependency`.
  - GitHub Actions workflow YAML: parse via `yaml.Node` to keep the `uses:` line
    (`ghaworkflow` currently unmarshals into structs).
  - CycloneDX SBOM / PURL list: no source location exists. Do **not** invent
    line numbers. Emit the result against the input file without a region and
    document that GitHub will not annotate it in PRs (verify display behavior
    during implementation).
- **Fingerprint**: emit `partialFingerprints` keyed on rule + versionless PURL +
  file, so an alert survives line shifts and version bumps of the same package.
- ADR-0021 records the format decisions (level mapping, location policy).

### P2 — "new dependencies only": `--baseline <path>`

Compares the scanned inventory with a baseline inventory of the same kind
(e.g. the base branch's `go.mod` or SBOM) and reports / gates only
dependencies that are **added** (package not present in the baseline).
Version-only changes of an existing package are not "added" (reported, not gated).

Why a flag and not GitHub's diff filtering: GitHub hides alerts on lines
outside the PR diff, but (a) code scanning on private repositories needs a
paid GitHub Code Security license, (b) GitLab / Bitbucket / pre-commit users
get nothing, (c) SBOM-derived results have no line to be "in the diff", and
(d) GitHub docs do not state that off-diff alerts are excluded from the
check's pass/fail. Flag rationale per project-conventions: recurring
operational need that cannot be inferred.

The baseline is a file, not a git ref, so uzomuzo does not shell out to git;
wrappers supply it (`git show origin/main:go.mod > base.go.mod`).

Scheduled full scans (existing `dependency-scan.yml`) remain the way to catch
deterioration of dependencies nobody touched.

### P3 — Distribution: reusable GitHub Action + pre-commit hook

- `action.yml` at repo root (composite): installs a pinned uzomuzo release,
  runs `scan` with `--baseline` from the PR base, writes SARIF, uploads via
  `github/codeql-action/upload-sarif`. Inputs: `file`/`sbom`, `fail-on`
  (default empty = advisory only), `upload-sarif` (default true).
- `.pre-commit-hooks.yaml`: hook `uzomuzo-new-deps` on `go.mod` changes,
  `stages: [manual, pre-push]` by default (network call; not on every commit).
- Docs: `docs/integration-examples.md` gains a "Lint / PR integration" section
  with one copy-paste snippet per persona (below).

### P4 — Snapshot for golangci-lint: `--format gomodguard-v2`

Emits a `linters.settings.gomodguard_v2.blocked` block (golangci-lint
≥ v2.12.0; the old `gomodguard` linter is deprecated since v2.12.0):

```yaml
# BEGIN uzomuzo generated 2026-10-30 (uzomuzo scan --fail-on eol-confirmed,eol-effective)
- module: github.com/pkg/errors
  match-type: exact
  recommendations: [errors]
  reason: "uzomuzo EOL-Confirmed: <evidence summary> (as of 2026-10-30)"
# END uzomuzo generated
```

golangci-lint has no config include, so the block is pasted / regenerated
between markers in the user's `.golangci.yml`. A scheduled workflow example
regenerates it and opens a PR, so verdict changes are reviewable diffs.
Limits to document: direct modules only; snapshot staleness between runs.

### P5 — ESLint snapshot: `--format eslint`

`no-restricted-imports` `paths[]` with `message`. Source of npm packages is a
CycloneDX SBOM (uzomuzo has no package.json parser). Limits to document:
dynamic `import()` and `require()` are not checked by that rule.

### Deferred — Ruff `banned-api`

Ruff bans import names, not PyPI distribution names; the mapping
(`ResolveImportNames`) skips large wheels and namespace packages (#548).
Revisit after #548.

### Deferred — GitLab Code Quality JSON

GitLab's Code Quality report wants CodeClimate JSON, not SARIF. Until asked
for, GitLab users use the exit code from P2 or reviewdog `-f=sarif` with the
`gitlab-mr-discussion` reporter.

## How each kind of team adopts it

| Team | What they add | Gate | Phase |
|---|---|---|---|
| Any repo on GitHub (public, or private with Code Security) | `uses: future-architect/uzomuzo-oss@<sha>` in a PR workflow | PR annotations; fail only on new deps in `fail-on` | P1–P3 |
| Private GitHub repo without Code Security | Same Action with `upload-sarif: false` | Job exit code via `--baseline` | P2–P3 |
| Go team on golangci-lint | Generated `gomodguard_v2` block + monthly regeneration workflow | Offline, in editor and CI | P4 |
| JS/TS team on ESLint | Generated `no-restricted-imports` entries | Offline, static imports only | P5 |
| GitLab / Bitbucket | CLI in CI job; optionally reviewdog with SARIF | Exit code; MR comments via reviewdog | P1–P2 |
| pre-commit users / no CI | `.pre-commit-config.yaml` entry, `pre-push` stage | Local, needs network | P3 |
| Monorepo | One Action step per manifest (matrix) | Per manifest | P3 |

## Risks and controls

- **Changing answers / rate limits**: PR gating only on added dependencies;
  snapshot formats carry their generation date; unavailable evidence is
  reported as "unknown", never as healthy.
- **Heuristic labels blocking PRs**: default `fail-on` is empty (advisory);
  docs recommend `eol-confirmed,eol-effective` at most for gating.
- **Snapshot staleness** (P4/P5): scheduled regeneration as a PR; date stamped
  in the marker line.
- **SARIF limits**: 25,000 results per run, 5,000 displayed; fine for single
  manifests, document for monorepos.

## What would change this plan

- Most adopters want offline Go enforcement first → move P4 before P1.
- Most adopters have no SARIF consumer (private repos without Code Security,
  non-GitHub) → ship P2 before P1.
- uzomuzo gains package.json / pyproject parsers → JS/Python get line
  locations in P1 and Ruff stops being deferred.

## Open questions to verify during implementation

1. Does GitHub's "Code scanning results" check fail on `error` alerts that lie
   outside the PR diff? (Docs: such alerts are not *shown*; failure behavior
   not stated.)
2. How GitHub handles SARIF results without a `region` (SBOM-derived).

## Review record

- Codex gpt-6-astra: order C (SARIF) → D (new-only) → B (linter snapshots) → E
  (pre-commit); reject live plugins. Flagged `gomodguard` deprecation
  (verified in golangci-lint `pkg/lint/lintersdb/builder_linter.go`).
- Claude Fable: SARIF first, snapshots second; proposed relying on GitHub
  diff-anchoring instead of a new-only flag. Not adopted: anchoring needs a
  paid license on private repos and does not cover non-GitHub or SBOM inputs.
