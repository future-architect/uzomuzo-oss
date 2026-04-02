---
name: oss-select
description: "OSS Select — evaluate candidate OSS packages with uzomuzo to support adoption decisions"
argument-hint: |
  Provide PURLs for candidate packages:
  - Single evaluation: 'pkg:golang/modernc.org/sqlite'
  - Compare candidates: 'pkg:golang/modernc.org/sqlite pkg:golang/github.com/mattn/go-sqlite3'
  - Scan all go.mod deps: 'scan'
---

# /oss-select — OSS Package Evaluation

> **Full specification**: See `.github/prompts/oss-select.prompt.md` for the complete
> evaluation procedure, adoption criteria, and output formats.

## Quick Reference

- **Evaluate**: `/oss-select pkg:golang/modernc.org/sqlite` — single package assessment
- **Compare**: `/oss-select pkg:golang/modernc.org/sqlite pkg:golang/github.com/mattn/go-sqlite3`
- **Scan**: `/oss-select scan` — bulk check all go.mod dependencies
- Verdicts: Approved / Conditional / Not Approved / Needs Investigation
- Always include major version suffix in PURLs (e.g., `semver/v3` not `semver`)
