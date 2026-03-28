---
name: oss-health
description: "OSS Health Judge — classify pending oss-catalog.db records using the LLM"
argument-hint: |
  Select processing mode:
  - Process up to 100 unjudged records (default): 'judge'
  - Limit count: 'judge --limit 50'
  - Filter by ecosystem: 'judge --ecosystem npm'
  - Re-judge stale: 'judge --filter stale --days 30'
---

# /oss-health — OSS Health Judge

> **Full specification**: See `.github/prompts/oss-health.prompt.md` for the complete
> extract → judge → import procedure.

## Quick Reference

- **Default**: `/oss-health judge` — process up to 100 pending records
- **Limited**: `/oss-health judge --limit 50` — process 50 records
- **Stale**: `/oss-health judge --filter stale --days 30` — re-judge stale records
- **Specific**: `/oss-health judge pkg:npm/express pkg:npm/lodash`
- Requires `oss-catalog.db` in workspace (download via `gh release download`)
- Note: `catalog-health-extract`/`catalog-health-import` commands are catalog-repo only
