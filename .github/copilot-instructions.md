# GitHub Copilot Instructions for Go

This project uses split instruction files for maintainability.
See `.github/instructions/` for detailed rules.

## Instruction Files

| File | Scope |
|------|-------|
| [language-policy](instructions/language-policy.instructions.md) | Source code English / docs English / response language |
| [ddd-architecture](instructions/ddd-architecture.instructions.md) | DDD layer definitions, search-first, pre-implementation checklist |
| [coding-standards](instructions/coding-standards.instructions.md) | Clean Code, naming, API design, goimports/golangci-lint |
| [error-handling](instructions/error-handling.instructions.md) | Error wrapping (`%w`), errors.Is/As, slog integration |
| [testing-performance](instructions/testing-performance.instructions.md) | Table-driven tests, benchmarks, concurrency |
| [project-conventions](instructions/project-conventions.instructions.md) | Configuration policy, test data, JSON tooling constraints |
| [git-workflow](instructions/git-workflow.instructions.md) | Commit messages, PR workflow, TDD |
| [security](instructions/security.instructions.md) | Prompt injection defense, credentials, Go security |
| [agent-orchestration](instructions/agent-orchestration.instructions.md) | Available agents, usage rules, parallel execution |

## Quick Reference

- **Language**: Source code = English only. Docs = English (`README.md`, `docs/*.md`). Respond in the user's language.
- **DDD Layers**: `Interfaces → Application → Domain ← Infrastructure`. Never violate.
- **Search First**: Always search for existing implementations before writing new code.
- **Error Handling**: Always wrap errors with `fmt.Errorf("context: %w", err)`.
- **Formatting**: All Go code MUST be formatted with `goimports`.
- **Configuration**: Do NOT add new env vars / CLI flags casually. See project-conventions for checklist.
- **Security**: No hardcoded secrets. No `exec.Command("sh", "-c", userInput)`.
- **Testing**: Table-driven tests, `t.Run()` sub-tests, test with `-race` flag.

## Sync Policy

`.github/` is the **single source of truth** for all shared instructions, agents, and prompts.

- **Instructions**: Edit `.github/instructions/`. Run `make sync-instructions` to regenerate `.claude/rules/`.
- **Agents**: Edit `.github/agents/`. `.claude/agents/` are thin shims that delegate here.
- **Prompts**: Edit `.github/prompts/`. `.claude/skills/` are thin shims that delegate here.

See `.claude/rules/instruction-sync.md` for the full mapping and editing protocol.
