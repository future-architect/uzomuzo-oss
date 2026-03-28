---
name: oss-health
description: "OSS Health Judge — assess pending oss-catalog.db records via LLM"
argument-hint: |
  Specify processing mode:
  - Process 100 pending (default): 'judge'
  - Limit count: 'judge --limit 50'
  - Filter by ecosystem: 'judge --ecosystem npm'
  - Re-judge stale: 'judge --filter stale --days 30'
---

Follow the instructions in `.github/prompts/oss-health.prompt.md` to extract, judge, and import OSS health assessments.
