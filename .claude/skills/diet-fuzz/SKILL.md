---
name: diet-fuzz
description: "Batch diet-trial fuzz testing across OSS projects. Usage: /diet-fuzz <languages|all> [--count N] [--tool trivy,syft,cdxgen] [--max-parallel N] [--projects-file path]"
argument-hint: |
  Specify languages to test:
  - All languages: 'all'
  - Specific: 'go,typescript --count 10'
  - Single tool: 'python --tool syft --count 3'
  - Curated list: 'all --projects-file projects.txt'
---

# /diet-fuzz — Batch Diet Fuzz Testing

> **Full specification**: See `.github/prompts/diet-fuzz.prompt.md` for the complete
> execution pipeline, project selection algorithm, and issue filing format.

## Quick Reference

- **Full run**: `/diet-fuzz all` — 5 projects × 4 languages × 3 SBOM tools
- **Targeted**: `/diet-fuzz go,typescript --count 10` — 10 projects × 2 languages
- **Single tool**: `/diet-fuzz python --tool syft --count 3`
- **Curated**: `/diet-fuzz all --projects-file projects.txt`
- **High parallel**: `/diet-fuzz all --max-parallel 8`

## Pipeline

1. Pull `origin/main` & rebuild `uzomuzo-diet`
2. Select projects (stratified sampling or projects-file)
3. Pre-filter (clone + SBOM, skip empty dependency graphs)
4. Run `uzomuzo-diet` per project × tool
5. Detect anomalies (IBNC, EOL-ZERO-SCORE, HIGH-SCORE-BUT-HARD)
6. Compare with previous runs (auto-diff)
7. Auto-file/update GitHub issues (grouped by root cause)
8. Append new findings to `uzomuzo-diet-findings.md`
9. Display cross-language summary table

## Key Points

- **Stratified sampling**: Projects chosen across popularity tiers (stars) and time periods for parser pattern diversity
- **All SBOM tools by default**: trivy, syft, cdxgen — cross-tool comparison reveals tool-specific issues
- **Auto issue management**: Creates new issues or adds evidence to existing ones, labels `bug` + `diet-trial` + `lang:*`
- **Regression detection**: Compares with past runs to catch accuracy regressions
- **Pre-filter**: Skips projects where SBOM has no dependency graph (avoids wasted diet runs)
- **Uses `gh api` (REST)** for GitHub operations to avoid GraphQL rate limits
