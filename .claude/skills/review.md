---
name: review
description: "Unified code review: Claude review + Copilot comment resolution + rule learning. Usage: /review [PR#] [--copilot-only] [--dry-run]"
---

# /review — Unified Code Review

> **Full specification**: See `.github/prompts/review.prompt.md` for the complete
> procedure, classification rules, and safety rules.

## Quick Reference

- **Full review**: `/review` — Claude review + Copilot resolution + rule learning
- **Single PR**: `/review 21` — target a specific PR
- **Copilot only**: `/review --copilot-only` — skip Claude review, just resolve Copilot
- **Dry run**: `/review --dry-run` — classify and propose without making changes
- Phase 1: Launch code-reviewer + architect agents in parallel
- Phase 2: Resolve Copilot threads (FIX / ALREADY_FIXED / WONT_FIX)
- Phase 3: Learn recurring patterns → propose rule additions to `.github/instructions/`
- Phase 4: Unified summary report
