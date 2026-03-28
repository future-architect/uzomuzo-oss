---
name: oss-select
description: "OSS Select — evaluate candidate packages with uzomuzo for adoption decisions"
argument-hint: |
  Specify candidate package PURLs:
  - Single evaluation: 'pkg:golang/modernc.org/sqlite'
  - Compare candidates: 'pkg:golang/modernc.org/sqlite pkg:golang/github.com/mattn/go-sqlite3'
  - Audit all go.mod deps: 'audit'
---

Follow the instructions in `.github/prompts/oss-select.prompt.md` to evaluate OSS packages using uzomuzo.
