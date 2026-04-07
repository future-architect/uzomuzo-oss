# CLAUDE.md

## Worktree Isolation (MUST READ FIRST)

**ALWAYS call `EnterWorktree` before doing any work that changes files.** This prevents branch conflicts when multiple sessions run in parallel. Give it a descriptive name (e.g., `feat-goreleaser`). Commit before exiting and inform the user of the branch name.

**Worktree deletion is dangerous — other sessions may depend on it.** NEVER remove a locked or unmerged worktree. See `.claude/rules/git-workflow.md` "Worktree Lifecycle: Lock and Cleanup" for the full safe cleanup protocol (lock check → merge check → dirty check).

## Build & Test

```bash
go build -o uzomuzo ./cmd/uzomuzo   # build
go test ./...                       # test all
goimports -w . && golangci-lint run # format & lint
go run ./cmd/uzomuzo update-spdx   # regenerate SPDX license list
```

## Configuration

Copy `config.template.env` to `.env` (auto-loaded via godotenv). Key vars: `GITHUB_TOKEN`, `LOG_LEVEL`, `LIFECYCLE_ASSESS_TYPE`. See `config.template.env` for defaults.

## Go Version Policy

Team uses **Go 1.26.1**. `go.mod` `go 1.25.0` is the dependency minimum — do not downgrade.

## Architecture (DDD)

`Interfaces → Application → Domain ← Infrastructure`. See `.claude/rules/ddd-architecture.md`.

- **domain/** — Pure business logic. Core: `Analysis`, `ResolvedLicense`, `EOLStatus`, `AssessmentResult`
- **application/** — Use case orchestration. `AnalysisService` / `FetchService`. Supports `AnalysisEnricher` hook
- **infrastructure/** — API clients (depsdev, github, eolevaluator), integration, CSV export
- **interfaces/cli/** — CLI entry points. No concurrent logic
- **pkg/uzomuzo/** — Public library facade

## Key Concepts

- **PURL Identity**: `OriginalPURL` (caller input) / `EffectivePURL` (resolved) / `CanonicalKey` (dedup key)
- **Generated**: `internal/domain/licenses/spdx_generated.go` — never edit, use `go run . update-spdx`

## Rules & Docs

- Coding: `.claude/rules/coding-standards.md`, `.claude/rules/copilot-learned-coding.md`, `.claude/rules/project-conventions.md`
- Language: English only (source + docs). See `.claude/rules/language-policy.md`
- Docs: `docs/data-flow.md`, `docs/development.md`, `docs/library-usage.md`, `docs/purl-identity-model.md`, `docs/license-resolution.md`
