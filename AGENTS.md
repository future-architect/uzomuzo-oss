<!-- Generated from .github/AGENTS.base.md — DO NOT EDIT DIRECTLY -->

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

## Rules — Read Before You Write Code

`.github/instructions/` is the single source of truth. Read the file that
covers what you are about to change; these are not summarized here, because a
summary would drift from the source.

| File | Topic |
|------|-------|
| `.github/instructions/agent-orchestration.instructions.md` | Agent Orchestration |
| `.github/instructions/base/arch-ddd/ddd-architecture.instructions.md` | DDD Layered Architecture — Strict Enforcement |
| `.github/instructions/base/core/coding-standards.instructions.md` | Coding Standards |
| `.github/instructions/base/core/error-handling.instructions.md` | Error Handling |
| `.github/instructions/base/core/git-workflow.instructions.md` | Git Workflow |
| `.github/instructions/base/core/language-policy.instructions.md` | Language Policy |
| `.github/instructions/base/core/security.instructions.md` | Security Guidelines |
| `.github/instructions/copilot-learned-coding.instructions.md` | Coding Standards — Learned from Copilot Reviews |
| `.github/instructions/project-conventions.instructions.md` | Project Conventions |
| `.github/instructions/test-design.instructions.md` | Test Design — pre-PR lens |
| `.github/instructions/testing-performance.instructions.md` | Testing & Performance |

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

## Instruction Sync

`.github/` is canonical. Two targets are **generated** from it:

```bash
make sync-instructions   # regenerate .claude/rules/*.md and AGENTS.md from .github/
```

Never edit those generated files directly — edit the `.github/` source and
regenerate. The `.claude/agents/` and `.claude/skills/` shims also delegate to
`.github/`, but they are **hand-maintained**, not produced by that command. See
`.claude/rules/instruction-sync.md` for the full mapping.
