# Blog images

Figures used in blog posts about uzomuzo (for example on dev.to). Each PNG is rendered from the SVG next to it; edit the SVG and re-render instead of editing the PNG.

```bash
google-chrome --headless=new --no-sandbox --hide-scrollbars --window-size=1600,660 \
  --default-background-color=ffffffff --screenshot=out.png file://$PWD/<name>.svg
```

| File | Used in | Shows |
|---|---|---|
| `2026-10-malicious-and-silence-timeline.*` | "A maintained package can still ship malware" (dev.to, 2026-10) | On 2025-09-08, `chalk@5.6.1` and `ansi-styles@6.2.2` were published maliciously within a minute. Applying today's rules to the published timestamps: chalk had released 22 days earlier, so nothing in uzomuzo flagged it until its MAL record appeared about 4 hours later; ansi-styles had been silent for 1,061 days, so the release-after-silence marker (ADR-0028) would have covered it from the start. Timestamps: npm registry `time` and OSV `published` (MAL-2025-46969, MAL-2025-46967). |
| `2026-10-leaf-vs-layer.*` | "We cut our scanner binary by 68%" (dev.to, 2026-10) | Left: after vuls PR #2461 removed its direct import of `go-homedir`, nine other modules still required it (`go mod graph` at `fcbd72e`), so `go.sum` stayed at 1,220 lines. Right: vuls PR #2476 stopped going through Trivy's fanal and called the lockfile parsers directly. Packages in the scanner build counted with `go list -tags scanner -deps ./cmd/scanner` at `652294a^` (2,345) and `652294a` (737). Binary sizes from the PR. |
| `2026-10-actions-layers.*` | "Your CI runs other people's code" (dev.to, 2026-10) | Which layer of a workflow each tool reads, and who can fix each layer. Example from Grafana's `release-build.yml` and `tibdex/github-app-token` (archived July 2025). A scan of grafana/grafana on 2026-10-26 with `--include-actions --show-transitive` found no Actions inside composite Actions (`transitive_actions=0`). |
