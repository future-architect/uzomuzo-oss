---
name: diet-trial
description: "Run uzomuzo diet on an external OSS project. Usage: /diet-trial <org/repo> [--tool trivy|syft] [--compare] [--no-source] [--no-save]"
argument-hint: |
  Provide a GitHub repository:
  - Basic: 'grafana/grafana'
  - With syft: 'gin-gonic/gin --tool syft'
  - Compare tools: 'vuejs/core --compare'
  - Skip source: 'pallets/flask --no-source'
---

# /diet-trial — OSS Diet Analysis Trial

> **Full specification**: See `.github/prompts/diet-trial.prompt.md` for the complete
> execution pipeline, analysis procedure, and report format.

## Quick Reference

- **Basic**: `/diet-trial grafana/grafana` — Trivy SBOM + diet + save report
- **Syft**: `/diet-trial gin-gonic/gin --tool syft` — use syft instead of Trivy
- **Compare**: `/diet-trial vuejs/core --compare` — run both tools, compare results
- **Fast**: `/diet-trial fastapi/fastapi --no-source` — skip source coupling (Phase 2)
- **No save**: `/diet-trial hashicorp/terraform --no-save` — display only, don't save

## Pipeline

1. Clone (shallow) → 2. SBOM (Trivy/syft) → 3. `uzomuzo diet` → 4. Analysis + anomaly detection → 5. Auto-file issues for bugs → 6. Save report

## Key Points

- No GITHUB_TOKEN needed (diet accuracy is local-only: Graph + Coupling)
- Reports saved to `case-studies/uzomuzo-diet/<language>/` by default (go/python/typescript/java)
- Anomaly detection flags potential uzomuzo bugs and **auto-files GitHub issues** with reproduction data
- `--compare` mode produces SBOM tool comparison tables for conference materials
- Groups related anomalies into single issues; adds comments to existing issues for duplicates
