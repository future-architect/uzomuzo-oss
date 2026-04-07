---
name: batch-issues
description: "Batch-process open GitHub issues with parallel agents. Analyzes conflicts, dispatches workers, creates PRs. Usage: /batch-issues [issue numbers] [--dry-run] [--max-parallel N]"
argument-hint: |
  Options:
  - No args: auto-select and process open issues
  - Issue numbers: '/batch-issues 168 171 187' to process specific issues
  - --dry-run: show dispatch plan without launching agents
  - --max-parallel N: limit parallel agents (default 4)
---

# /batch-issues -- Parallel Issue Dispatcher

> **Full specification**: See `.github/prompts/batch-issues.prompt.md` for the complete
> procedure, conflict analysis, agent workflow, and safety rules.

## Quick Reference

- **Auto-select**: `/batch-issues` -- pick open issues, analyze conflicts, dispatch
- **Specific issues**: `/batch-issues 168 171 187` -- process these issues
- **Dry run**: `/batch-issues --dry-run` -- show plan only
- **Limit parallelism**: `/batch-issues --max-parallel 2`
- Phase 1: Issue selection & filtering
- Phase 2: Conflict analysis (shared file detection)
- Phase 3: Size/risk assessment (S/M/L classification)
- Phase 4: Dispatch plan (present to user)
- Phase 5: Agent dispatch (worktree-isolated, parallel across groups, sequential within)
- Phase 6: Summary report with PR links
- Phase 7: Evidence verification — check/fix Before/After test output in every PR body
- Each agent: reproduce (MANDATORY) -> architect -> implement -> review-until-clean -> verify after (MANDATORY) -> before/after PR (template enforced)
