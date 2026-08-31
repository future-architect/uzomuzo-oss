# AGENTS.md

Entry point for coding agents that read `AGENTS.md` (OpenAI Codex CLI and
compatible tools). Claude Code reads `CLAUDE.md` and `.claude/rules/`; GitHub
Copilot reads `.github/copilot-instructions.md`. All three are derived from the
same source of truth: **`.github/`**.

This file is generated. Do not edit it by hand — edit `.github/AGENTS.base.md`
and run `make sync-instructions`.

## Build & Test

```bash
go build -o uzomuzo ./cmd/uzomuzo   # build
go test ./...                       # test all
goimports -w . && golangci-lint run # format & lint
go run ./cmd/uzomuzo update-spdx    # regenerate SPDX license list
```

Team uses Go 1.26.1. The `go 1.25.0` line in `go.mod` is the dependency
minimum — do not downgrade it.

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

## Instruction Sync

`.github/` is canonical. `.claude/rules/*.md`, `AGENTS.md`, and the
`.claude/agents/` and `.claude/skills/` shims are derived from it.

```bash
make sync-instructions   # regenerate .claude/rules/ and AGENTS.md from .github/
```

Edit the `.github/` side first, then regenerate. Never edit a generated file
directly. See `.claude/rules/instruction-sync.md` for the full mapping.
