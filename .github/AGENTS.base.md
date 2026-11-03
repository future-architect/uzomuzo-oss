# AGENTS.md

Entry point for coding agents that read `AGENTS.md` (OpenAI Codex CLI and
compatible tools). Claude Code reads `CLAUDE.md` and `.claude/rules/`; GitHub
Copilot reads `.github/copilot-instructions.md`. The canonical rule text for all
of them lives in **`.github/`** — `.claude/rules/*.md` and this file's rendered
output are generated from it, while `CLAUDE.md` and
`.github/copilot-instructions.md` are hand-maintained entry points.

The root `AGENTS.md` is generated from `.github/AGENTS.base.md` by
`make sync-instructions`. Edit that template; never edit `AGENTS.md` directly.

## Build & Test

```bash
go build -o uzomuzo ./cmd/uzomuzo   # build
go test ./...                       # test all
goimports -w . && golangci-lint run # format & lint
go run ./cmd/uzomuzo update-spdx    # regenerate SPDX license list
```

Team uses Go 1.26.1. The `go 1.25.0` line in `go.mod` is the module's minimum
supported Go version — do not downgrade it.

## Architecture (DDD)

`Interfaces → Application → Domain ← Infrastructure`

- **domain/** — Pure business logic. Core: `Analysis`, `ResolvedLicense`, `EOLStatus`, `AssessmentResult`
- **application/** — Use case orchestration. `AnalysisService`. Supports the `AnalysisEnricher` hook
- **infrastructure/** — API clients (depsdev, github, eolevaluator), integration, CSV export
- **interfaces/cli/** — CLI entry points. No concurrent logic
- **pkg/uzomuzo/** — Public library facade

Never violate the dependency direction. Full rules:
`.github/instructions/ddd-architecture.instructions.md`.

## Language Policy

**English only** — source code, comments, error and log messages, CLI output,
test code and fixtures, and all documentation (`README.md`, `docs/*.md`).
Japanese text must not appear in source comments or identifiers. Reply to a
chat message in the language the human used.

## This Repository Is Public

Everything committed here is published immediately, including to people outside
this organization. Two consequences bind every change you make:

- **Do not name a non-public sibling repository**, describe its internal
  conventions or architecture, or quote its documents — in code, comments, docs,
  commit messages, or pull request text. Refer to such repositories generically
  ("a consuming repository", "a sibling repository").
- Shared rule files under `.github/instructions/base/` are consumed by other
  repositories, so the same restriction applies with more force there. See
  `.github/instructions/base/README.md`.

Existing documents are not retro-edited to satisfy this. `docs/adr/` is
append-only decision history, and a handful of older files name sibling
repositories; leaving them is deliberate, not permission. The rule binds what
you write now.

## Rules — Read Before You Write Code

`.github/instructions/` is the single source of truth. Read the file that
covers what you are about to change; these are not summarized here, because a
summary would drift from the source.

<!-- INSTRUCTION-INDEX -->

## Codex Has No Guardrail Hooks — Hand Off Writes to Claude Code

The repository's automated checks are wired as Claude Code `PreToolUse` hooks in
`.claude/settings.json` (`adr-check.sh`, `check-new-deps.sh`,
`pre-push-review.sh`, `pr-body-review.sh`, `plan-mode-architect-check.sh`,
`readme-walkthrough.sh`). **They run only inside Claude Code.** A Codex session
executes `git` in its own process, so none of them fire: no ADR check, no new
dependency health check, no pre-push self-review, no PR body review.

Therefore, when running as Codex in this repository:

- **Do not run `git push` or `gh pr create`.** Leave the change in the working
  tree, or at most in a local commit, and hand off to a Claude Code session so
  the checks actually run before anything leaves the machine.
- Report what you changed and why, so the handoff carries the context the hooks
  would otherwise have surfaced.
- If a human explicitly instructs you to commit or push anyway, say plainly
  which checks are being skipped.

Two rules the hooks would otherwise enforce, which you must honor manually:

- **Never push directly to `main`.** Every change goes through a pull request.
- **An architectural decision needs an ADR** in `docs/adr/`. Reference an ADR
  from code by ID (`// See ADR-NNNN.`) — never restate its rationale in a
  comment.

## Devflow pilot

This machine-readable opt-in contract is read by the maintainers' dual-vendor
pilot helper. In this flow, one vendor's coding agent writes requirements,
design, plan, and implementation; an agent from another vendor reviews every
required phase. Contributors without the helper can ignore this section. All
other rules in this file still apply to everyone.

```json
{
  "schema_version": 1,
  "team": "uzomuzo-oss",
  "artifact_directory": "docs/plans",
  "policy_files": [
    "AGENTS.md",
    ".github/AGENTS.base.md",
    ".github/instructions/agent-orchestration.instructions.md",
    ".github/instructions/base/core/git-workflow.instructions.md",
    ".github/instructions/copilot-learned-coding.instructions.md",
    ".github/prompts/review-until-clean.prompt.md"
  ],
  "snapshot_exclude": [
    "docs/plans/"
  ],
  "checks": [
    {
      "id": "build",
      "argv": ["env", "-u", "GIT_DIR", "-u", "GIT_WORK_TREE", "-u", "GIT_INDEX_FILE", "-u", "GIT_COMMON_DIR", "GOFLAGS=-buildvcs=false", "go", "build", "./..."],
      "timeout_seconds": 900,
      "skip_parser": "none",
      "tools": [["go", "version"]]
    },
    {
      "id": "go-test-json",
      "argv": ["env", "-u", "GIT_DIR", "-u", "GIT_WORK_TREE", "-u", "GIT_INDEX_FILE", "-u", "GIT_COMMON_DIR", "go", "test", "-race", "-vet=off", "-count=1", "-json", "./..."],
      "timeout_seconds": 1800,
      "skip_parser": "go-test-json",
      "allowed_skips": [
        {"package": "github.com/future-architect/uzomuzo-oss/internal/common/links", "test": "TestBuildDepsDevURL_LiveProbe", "reason": "Opt-in live network probe (UZOMUZO_LIVE_PROBE=1); no local or CI check runs it"},
        {"package": "github.com/future-architect/uzomuzo-oss/internal/infrastructure/npmjs", "test": "TestGetPublishHistory_LiveProbe", "reason": "Opt-in live network probe (UZOMUZO_LIVE_PROBE=1); no local or CI check runs it"}
      ],
      "tools": [["go", "version"]]
    },
    {
      "id": "golangci-lint",
      "argv": ["env", "GOTOOLCHAIN=go1.26.1", "golangci-lint", "run"],
      "timeout_seconds": 1200,
      "skip_parser": "none",
      "tools": [["env", "GOTOOLCHAIN=go1.26.1", "golangci-lint", "--version"]]
    },
    {
      "id": "instruction-sync-fresh",
      "argv": ["bash", "-c", "set -euo pipefail; make sync-instructions >/dev/null; orphans=\"\"; for dest in .claude/rules/*.md; do base=$(basename \"$dest\" .md); if [ \"$base\" = instruction-sync ]; then continue; fi; if [ \"$base\" = agents ]; then base=agent-orchestration; fi; if [ -z \"$(find .github/instructions -type f -name \"$base.instructions.md\" -print -quit)\" ]; then orphans=\"$orphans $dest\"; fi; done; if [ -n \"$orphans\" ]; then echo \"generated rule files with no instruction source:$orphans\" >&2; exit 1; fi; drift=$(git status --porcelain --untracked-files=all -- AGENTS.md .claude/rules/); if [ -n \"$drift\" ]; then printf '%s\\n' \"$drift\"; echo 'generated files are stale: run make sync-instructions and commit the result' >&2; exit 1; fi; echo 'generated files are fresh'"],
      "timeout_seconds": 120,
      "skip_parser": "none",
      "paths": ["AGENTS.md", ".github/AGENTS.base.md", ".github/instructions/**", ".claude/rules/**", "Makefile"],
      "tools": [["bash", "--version"], ["make", "--version"], ["git", "--version"]]
    }
  ],
  "local_review_limit": 120,
  "bot": {
    "limit": 5,
    "policy_ref": ".github/prompts/review-until-clean.prompt.md#step-8-copilot-review-iteration-loop-max-5-rounds"
  },
  "publication_default": "local",
  "publication_steps": [],
  "knowledge": {
    "pending_directory": ".github/instructions",
    "required_fields": ["category", "summary", "pr", "file", "date"],
    "sync_argv": ["make", "sync-instructions"]
  }
}
```

Maintainers with the helper use this flow by default for implementation, fixes,
refactoring, tests, related configuration, and publication preparation.
Explanations, status checks, and read-only reviews do not start it. Use the
existing plan-mode and `/review-until-clean` flow only when a maintainer asks
for it explicitly or the helper is unavailable. A request to review existing
code still runs the full `/review-until-clean`.

Codex leads and Claude Code reviews by default. Choose the lead when the task
starts and keep that lead throughout; the reviewer must be from another vendor.
Even when Codex leads, Codex does not commit in this flow: a Claude Code
session started in this repository commits, pushes, and opens the pull request,
so the guardrail hooks described above run. Merge
and release each need separate explicit maintainer approval.

Keep the pilot's requirements, design, and plan files in git-ignored
`docs/plans/`; never commit them in this public repository. The helper keeps
task state outside the repository.

The checks cover what CI checks: `go build`, `go test -race`, `golangci-lint`,
and, when instruction sources change, the generated-file freshness and orphan
checks. They differ from CI in four ways. Tests emit `-json` so the helper can
reject skipped tests: only the two opt-in live network probes enabled by
`UZOMUZO_LIVE_PROBE=1` may skip, and a new skip needs an `allowed_skips` entry
with its reason in the same pull request. The linter runs with
`GOTOOLCHAIN=go1.26.1`, the team's Go version, because a golangci-lint release
can lag a newer local Go and fail to read its export data. Build and test unset
the helper's `GIT_*` variables so Go never inspects the helper's temporary
repository. The build sets `-buildvcs=false` because, inside the Codex sandbox,
Go's VCS stamping fails on the helper's snapshot tree. Update these checks when
CI changes.

After the helper's publication check passes and a pull request exists, run
`/review-until-clean` Phase B and Phase C for the Copilot bot. Do not repeat
Phase A: opposite-vendor reviews cover it. Keep `bot.limit` equal to the round
cap in that prompt's Step 8 heading; change both together. Record reusable
lessons as `pending_patterns` in
`.github/instructions/copilot-learned-coding.instructions.md`, then run
`make sync-instructions`.

## Instruction Sync

`.github/` is canonical. Two targets are **generated** from it:

```bash
make sync-instructions   # regenerate .claude/rules/*.md and AGENTS.md from .github/
```

Never edit those generated files directly — edit the `.github/` source and
regenerate. The `.claude/agents/` and `.claude/skills/` shims also delegate to
`.github/`, but they are **hand-maintained**, not produced by that command. See
`.claude/rules/instruction-sync.md` for the full mapping.
