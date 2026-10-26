# Blog images

Figures used in blog posts about uzomuzo (for example on dev.to). Each PNG is rendered from the SVG next to it; edit the SVG and re-render instead of editing the PNG.

```bash
google-chrome --headless=new --no-sandbox --hide-scrollbars --window-size=1600,660 \
  --default-background-color=ffffffff --screenshot=out.png file://$PWD/<name>.svg
```

| File | Used in | Shows |
|---|---|---|
| `2026-10-malicious-and-silence-timeline.*` | "A maintained package can still ship malware" (dev.to, 2026-10) | On 2025-09-08, `chalk@5.6.1` and `ansi-styles@6.2.2` were published maliciously within a minute. Applying today's rules to the published timestamps: chalk had released 22 days earlier, so nothing in uzomuzo flagged it until its MAL record appeared about 4 hours later; ansi-styles had been silent for 1,061 days, so the release-after-silence marker (ADR-0028) would have covered it from the start. Timestamps: npm registry `time` and OSV `published` (MAL-2025-46969, MAL-2025-46967). |
