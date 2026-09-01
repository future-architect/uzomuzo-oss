# Instruction Sync — Single Source of Truth

`.github/` is the **single source of truth** for all shared instructions. `.claude/` files — and the root `AGENTS.md` — are either generated copies or thin delegation shims.

## Architecture

| File type | `.github/` (canonical) | `.claude/` (derived) | Sync method |
|-----------|------------------------|----------------------|-------------|
| **Rules** | `.github/instructions/*.instructions.md` | `.claude/rules/*.md` | `make sync-instructions` (generated copy) |
| **Agents** | `.github/agents/*.agent.md` | `.claude/agents/*.md` | Thin shim with delegation (hand-maintained) |
| **Skills/Prompts** | `.github/prompts/*.prompt.md` | `.claude/skills/*.md` | Thin shim with delegation (hand-maintained) |
| **Codex entry point** | `.github/AGENTS.base.md` | `AGENTS.md` (repo root) | `make sync-instructions` (generated copy) |

## Rules: Generated via Script

`.claude/rules/` files (except this file) are **auto-generated** from `.github/instructions/`. Do NOT edit them directly.

```bash
make sync-instructions   # regenerate .claude/rules/ and AGENTS.md from .github/
```

Rename mapping:

| `.github/instructions/` | `.claude/rules/` |
|--------------------------|------------------|
| `agent-orchestration.instructions.md` | `agents.md` |
| `copilot-learned-coding.instructions.md` | `copilot-learned-coding.md` |
| All others | Same base name (strip `.instructions` suffix) |

## AGENTS.md: Generated Entry Point for Codex

The root `AGENTS.md` is what OpenAI Codex CLI (and compatible tools) read; it is
the Codex counterpart of `CLAUDE.md` and `.github/copilot-instructions.md`. It is
**generated** by `make sync-instructions` from `.github/AGENTS.base.md`, so the
rule text is never duplicated:

- Static prose (build commands, DDD summary, language policy, Codex-specific
  constraints) lives in `.github/AGENTS.base.md`.
- The line `<!-- INSTRUCTION-INDEX -->` in that file is replaced at generation
  time by a table derived from `.github/instructions/*.instructions.md` — one
  row per file, titled by the file's own `# ` heading. Adding a new instruction
  file therefore adds its row automatically; no hand-editing.

`AGENTS.md` must NOT be edited directly. Edit `.github/AGENTS.base.md` and
regenerate.

`.github/AGENTS.base.md` deliberately sits outside `.github/instructions/`
because it is a different kind of artifact — a template carrying an index
marker, not a standalone rule file to be copied 1:1 — and must not be
enumerated by the instruction-index loop that reads that directory.

### Why an index, not a concatenation

`.github/instructions/` totals well over 100 KB (`copilot-learned-coding` alone
is ~68 KB). Inlining it would load the whole corpus into every Codex session.
`AGENTS.md` stays a few KB and points at the canonical files, so an agent reads
the rules that apply to what it is changing.

### The preamble intentionally repeats a little of CLAUDE.md

`AGENTS.base.md` restates the build commands, the Go version policy, the DDD
layer list and the language policy that also appear in `CLAUDE.md`. That
overlap is deliberate and unavoidable: Codex reads only `AGENTS.md`, so a
pointer to `CLAUDE.md` would reach nothing. **Both files must be updated
together** when any of those facts change. Keep the overlap to orientation
only — the canonical rule text stays in `.github/instructions/` and is
referenced by the index, never inlined.

### Known limitation: orphaned rule files

`make sync-instructions` creates and overwrites; it never deletes. If an
instruction file is removed from `.github/instructions/`, its generated
`.claude/rules/<name>.md` stays behind, and the CI freshness gate will not
notice — regeneration simply leaves that file untouched. Delete the generated
file by hand in the same commit. Automatic pruning is deliberately not
implemented: everything under `.claude/rules/` except `instruction-sync.md` is
generated, so a buggy prune would delete real content, and no instruction file
has ever been removed in this repository.

### Codex does not run the Claude Code hooks

`AGENTS.md` states this explicitly. The repository's guardrails
(`adr-check.sh`, `check-new-deps.sh`, `pre-push-review.sh`, `pr-body-review.sh`,
`plan-mode-architect-check.sh`, `readme-walkthrough.sh`) are wired as
`PreToolUse` hooks in `.claude/settings.json` and fire only inside Claude Code.
Codex is told to leave `git push` and `gh pr create` to a Claude Code session
so those checks actually run before anything leaves the machine; a local commit
is allowed.

## Agents: Delegation Pattern

`.claude/agents/*.md` are thin shims containing:
1. Claude-specific YAML frontmatter (`name`, `tools`, `model`)
2. A pointer: "See `.github/agents/<name>.agent.md` for the full specification"
3. A brief Quick Reference section

Only update `.claude/agents/` when Claude-specific metadata changes (tools, model).

## Skills: Delegation Pattern

`.claude/skills/*.md` contain YAML frontmatter + a pointer to `.github/prompts/`.

## CLAUDE.md Overlap

`CLAUDE.md` is always loaded and contains condensed references to rules files. It must NOT duplicate full rule content.

| CLAUDE.md Section | Source of Detail |
|---|---|
| Language Policy | `.claude/rules/language-policy.md` |
| Coding Standards | `.claude/rules/coding-standards.md` + `project-conventions.md` |
| Architecture | CLAUDE.md is canonical (project-specific context) |
| EOL Catalog / PURL Identity | CLAUDE.md is canonical (project-specific context) |

## Editing Protocol

1. **Always edit `.github/` side** — it is the single source of truth
2. **Rules**: Run `make sync-instructions` after editing `.github/instructions/` or `.github/AGENTS.base.md`
3. **Agents**: Edit `.github/agents/*.agent.md`. `.claude/agents/` shims rarely need changes
4. **Skills**: Edit `.github/prompts/`. `.claude/skills/` shims rarely need changes
5. **New file**: Create in `.github/`, add to this mapping, create `.claude/` counterpart (generated or shim)
6. **Deletion**: Remove from both locations and this mapping
