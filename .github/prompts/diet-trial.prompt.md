---
description: "Run uzomuzo diet on an external OSS project to test accuracy, find bugs, and gather case study data"
---

# /diet-trial — OSS Diet Analysis Trial

You are running a diet analysis trial on an external OSS project to test uzomuzo diet's accuracy, find bugs, and gather case study data for conference materials.

## Arguments

Parse the user's arguments:

- **`<org/repo>`** (required): GitHub repository in `org/repo` format (e.g., `grafana/grafana`)
- **`--tool <trivy|syft>`** (optional, default: `trivy`): SBOM generation tool
- **`--compare`** (optional): Run with both trivy and syft, compare results
- **`--no-source`** (optional): Skip source coupling analysis (Phase 2). Faster but less accurate.
- **`--no-save`** (optional): Do not save report to `case-studies/`
- **`--save-to <path>`** (optional): Override default save location

If the user provides a full GitHub URL (`https://github.com/org/repo`), extract `org/repo` from it.

## Prerequisites Check

Before starting, verify the required tools are available:

```bash
# Check uzomuzo-diet binary
which uzomuzo-diet 2>/dev/null || echo "MISSING: uzomuzo-diet"

# Check SBOM tool
which trivy 2>/dev/null || echo "MISSING: trivy"
which syft 2>/dev/null || echo "MISSING: syft"
```

If `uzomuzo-diet` is missing, build it:

```bash
CGO_ENABLED=1 go build -o ~/bin/uzomuzo-diet ./cmd/uzomuzo-diet
export PATH="$HOME/bin:$PATH"
```

If the selected SBOM tool is missing, install it:

- **trivy**: `curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b ~/bin`
- **syft**: `curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b ~/bin`

## Execution Pipeline

### Step 1: Clone

```bash
git clone --depth 1 https://github.com/<org>/<repo>.git /tmp/diet-trial-<repo>
```

If the clone already exists, skip. If clone fails (private repo, not found), report the error and stop.

### Step 2: Generate SBOM

**Trivy** (default):
```bash
trivy fs /tmp/diet-trial-<repo> --format cyclonedx -o /tmp/diet-trial-<repo>-sbom.json 2>/tmp/diet-trial-<repo>-sbom.log
```

**Syft**:
```bash
syft scan /tmp/diet-trial-<repo> --source-name <repo> -o cyclonedx-json > /tmp/diet-trial-<repo>-sbom.json 2>/tmp/diet-trial-<repo>-sbom.log
```

After SBOM generation, extract and display summary:

```python
python3 -c "
import json
with open('/tmp/diet-trial-<repo>-sbom.json') as f:
    d = json.load(f)
comps = d.get('components', [])
ecosystems = {}
for c in comps:
    p = c.get('purl', '')
    if p.startswith('pkg:'):
        eco = p.split('/')[0].split(':')[1]
        ecosystems[eco] = ecosystems.get(eco, 0) + 1
print(f'Components: {len(comps)}')
print(f'Ecosystems: {ecosystems}')
"
```

If components = 0, the SBOM generation failed. Check the log and report the error.

### Step 3: Run uzomuzo diet

**IMPORTANT**: Do NOT set GITHUB_TOKEN. Token is unnecessary for diet accuracy (Graph and Coupling are the dominant scoring axes, both are local-only).

```bash
unset GITHUB_TOKEN
uzomuzo-diet --sbom /tmp/diet-trial-<repo>-sbom.json --source /tmp/diet-trial-<repo> --format json 2>/tmp/diet-trial-<repo>-diet.log > /tmp/diet-trial-<repo>-diet-raw.json
```

The JSON output may have log lines mixed into stdout (emoji progress lines). Extract clean JSON:

```bash
# Find the line where JSON starts and extract
START=$(grep -n '^{' /tmp/diet-trial-<repo>-diet-raw.json | head -1 | cut -d: -f1)
if [ -n "$START" ]; then
  tail -n +$START /tmp/diet-trial-<repo>-diet-raw.json > /tmp/diet-trial-<repo>-diet.json
else
  cp /tmp/diet-trial-<repo>-diet-raw.json /tmp/diet-trial-<repo>-diet.json
fi
```

If `--no-source` was specified, omit `--source` flag.

### Step 4: Analyze Results

Parse the diet JSON and produce the analysis. Use Python for reliable JSON parsing:

```python
python3 << 'PYEOF'
import json, sys

with open("/tmp/diet-trial-<repo>-diet.json") as f:
    d = json.load(f)

summary = d["summary"]
deps = d["dependencies"]

# 1. Summary
print("=== SUMMARY ===")
for k, v in summary.items():
    print(f"  {k}: {v}")

# 2. Difficulty distribution
difficulties = {}
for dep in deps:
    diff = dep["difficulty"]
    difficulties[diff] = difficulties.get(diff, 0) + 1
print("\n=== DIFFICULTY DISTRIBUTION ===")
for diff in ["trivial", "easy", "moderate", "hard"]:
    print(f"  {diff}: {difficulties.get(diff, 0)}")

# 3. Lifecycle distribution (from diet data)
lifecycles = {}
for dep in deps:
    lc = dep.get("lifecycle", "Unknown")
    lifecycles[lc] = lifecycles.get(lc, 0) + 1
print("\n=== LIFECYCLE DISTRIBUTION ===")
for lc, cnt in sorted(lifecycles.items(), key=lambda x: -x[1]):
    print(f"  {lc}: {cnt}")

# 4. EOL/Archived used in code (non-trivial)
eol_labels = {"EOL-Confirmed", "EOL-Effective", "EOL-Scheduled", "Archived"}
eol_used = [dep for dep in deps if dep.get("lifecycle", "") in eol_labels and dep["difficulty"] != "trivial"]
eol_used.sort(key=lambda x: -x.get("call_site_count", 0))
print(f"\n=== EOL/ARCHIVED + USED IN CODE ({len(eol_used)} deps) ===")
for dep in eol_used:
    print(f"  [{dep['difficulty']}] {dep['name']} — {dep.get('import_file_count',0)} files, {dep.get('call_site_count',0)} calls, {dep.get('api_breadth',0)} APIs — {dep.get('lifecycle','?')}")

# 5. Top 20 by priority score
print("\n=== TOP 20 (priority score) ===")
for dep in deps[:20]:
    print(f"  #{dep['rank']} {dep['priority_score']:.2f} [{dep['difficulty']}] {dep['name']} — {dep.get('lifecycle','?')}")

# 6. Anomaly detection (potential bugs / accuracy issues)
print("\n=== ANOMALY CHECK ===")
anomalies = []
for dep in deps:
    # High coupling but 0 calls — possible detection miss
    if dep.get("import_file_count", 0) > 0 and dep.get("call_site_count", 0) == 0 and dep["difficulty"] not in ("trivial",):
        anomalies.append(f"  IMPORTS-BUT-NO-CALLS: {dep['name']} ({dep.get('import_file_count',0)} files, 0 calls) — possible call site detection miss")
    # Very high score but hard — unusual
    if dep["priority_score"] > 0.3 and dep["difficulty"] == "hard":
        anomalies.append(f"  HIGH-SCORE-BUT-HARD: {dep['name']} (score={dep['priority_score']:.2f}) — worth investigating")
    # EOL but score 0 — scoring issue?
    if dep.get("lifecycle", "") in eol_labels and dep["priority_score"] < 0.01 and dep["difficulty"] == "trivial":
        pass  # Expected: trivial EOL deps often have low score due to unused bonus cap
    elif dep.get("lifecycle", "") in eol_labels and dep["priority_score"] < 0.01:
        anomalies.append(f"  EOL-ZERO-SCORE: {dep['name']} (score={dep['priority_score']:.2f}, {dep['difficulty']}) — EOL dep with near-zero score")

if anomalies:
    for a in anomalies:
        print(a)
else:
    print("  No anomalies detected.")

PYEOF
```

### Step 5: Display Report

After analysis, display a structured report to the user. The report MUST include:

1. **Header**: Project name, date, SBOM tool, component count, ecosystem breakdown
2. **Summary table**: total_direct, total_transitive, unused_direct, easy_wins
3. **Difficulty distribution**: trivial / easy / moderate / hard
4. **EOL/Archived dependencies actually used in code** (the key finding):
   - Grouped by difficulty (hard → moderate → easy)
   - Include: name, files, calls, APIs, lifecycle status
   - Include migration target suggestions where obvious (e.g., `golang/mock` → `uber-go/mock`)
5. **Recommended action phases**: Phase 1 (trivial), Phase 2 (easy EOL), Phase 3 (moderate EOL), Phase 4 (hard EOL)
6. **Anomaly check results**: Any potential bugs or accuracy issues found
7. **Top 20 by priority score**

### Step 6: Save Report (unless --no-save)

Save the report as a Markdown file. Default location logic:

1. If `--save-to <path>` is specified, use that path
2. If `/workspace/vuls-saas/vuls-diet/case-studies/uzomuzo-diet/` exists, save there
3. Otherwise save to the current project's `docs/case-studies/` (create if needed)

Filename format: `<repo>-diet-trial-<tool>-<YYYY-MM-DD>.md`

The saved report must be in the same format as existing case studies in `case-studies/`. Use Japanese for section headers and commentary (matching existing case study style). Include:

- Execution conditions (tool versions, flags used)
- All tables from Step 5
- Raw data file paths
- Anomaly findings (important for bug tracking)

After saving, display the file path to the user.

## --compare Mode

When `--compare` is specified:

1. Run the full pipeline with **trivy** (default mode, excludes dev deps)
2. Run the full pipeline with **syft** (includes all deps)
3. Display a comparison table:

```
=== SBOM TOOL COMPARISON: <repo> ===
| Metric              | Trivy  | Syft   | Delta |
|---------------------|--------|--------|-------|
| Components          | X      | Y      | +Z    |
| Direct deps (diet)  | X      | Y      | +Z    |
| Unused              | X      | Y      | +Z    |
| Easy wins           | X      | Y      | +Z    |
| EOL (non-trivial)   | X      | Y      | +Z    |
```

This comparison is valuable for the SBOM Tool Comparison section in docs/diet.md and conference materials.

## Error Handling

- **Clone failure**: Report the error (private repo? not found? network?) and stop
- **SBOM generation failure**: Check the log file, report warnings/errors, suggest alternative tool
- **Diet execution failure**: Check the log file. Common issues:
  - tree-sitter parse errors (report the language and file)
  - SBOM format issues (check `bomFormat` field)
- **JSON parse failure**: Log lines mixed into stdout. Apply the cleanup step and retry.
- **Zero dependencies in diet output**: SBOM may be empty or malformed. Check component count.

## Notes

- **No GITHUB_TOKEN needed**: Diet accuracy depends on Graph (SBOM) and Coupling (source), both local-only. Token only affects Stalled detection in Phase 3 Health Signals, which is a minor factor.
- **Shallow clone is sufficient**: Source coupling analysis only needs current source, not git history.
- **Large repos** (>10k components): Diet may take several minutes for Phase 3 API calls. This is expected.
- **Trivy vs syft**: Trivy excludes devDependencies by default. For comprehensive analysis, use `--compare`.
