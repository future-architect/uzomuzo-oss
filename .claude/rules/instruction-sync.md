# Instruction Sync — Single Source of Truth

`.github/` is the **single source of truth** for all shared instructions. `.claude/` files are either generated copies or thin delegation shims.

## Architecture

| File type | `.github/` (canonical) | `.claude/` (derived) | Sync method |
|-----------|------------------------|----------------------|-------------|
| **Rules** | `.github/instructions/*.instructions.md` | `.claude/rules/*.md` | `make sync-instructions` (generated copy) |
| **Agents** | `.github/agents/*.agent.md` | `.claude/agents/*.md` | Thin shim with delegation (hand-maintained) |
| **Skills/Prompts** | `.github/prompts/*.prompt.md` | `.claude/skills/*.md` | Thin shim with delegation (hand-maintained) |

## Rules: Generated via Script

`.claude/rules/` files (except this file) are **auto-generated** from `.github/instructions/`. Do NOT edit them directly.

```bash
make sync-instructions   # regenerate .claude/rules/ from .github/instructions/
```

Rename mapping:

| `.github/instructions/` | `.claude/rules/` |
|--------------------------|------------------|
| `agent-orchestration.instructions.md` | `agents.md` |
| `copilot-learned-coding.instructions.md` | `copilot-learned-coding.md` |
| All others | Same base name (strip `.instructions` suffix) |

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
2. **Rules**: Run `make sync-instructions` after editing `.github/instructions/`
3. **Agents**: Edit `.github/agents/*.agent.md`. `.claude/agents/` shims rarely need changes
4. **Skills**: Edit `.github/prompts/`. `.claude/skills/` shims rarely need changes
5. **New file**: Create in `.github/`, add to this mapping, create `.claude/` counterpart (generated or shim)
6. **Deletion**: Remove from both locations and this mapping
