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
trivy fs /tmp/diet-trial-<repo> --format cyclonedx -o /tmp/diet-trial-<repo>-trivy-sbom.json 2>/tmp/diet-trial-<repo>-trivy-sbom.log
```

**Syft**:
```bash
syft scan /tmp/diet-trial-<repo> --source-name <repo> -o cyclonedx-json > /tmp/diet-trial-<repo>-syft-sbom.json 2>/tmp/diet-trial-<repo>-syft-sbom.log
```

After SBOM generation, extract and display summary:

```python
python3 -c "
import json
with open('/tmp/diet-trial-<repo>-<tool>-sbom.json') as f:
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
uzomuzo-diet --sbom /tmp/diet-trial-<repo>-<tool>-sbom.json --source /tmp/diet-trial-<repo> --format json 2>/tmp/diet-trial-<repo>-<tool>-diet.log > /tmp/diet-trial-<repo>-<tool>-diet-raw.json
```

The JSON output may have log lines mixed into stdout (emoji progress lines). Extract clean JSON:

```bash
# Find the line where JSON starts and extract
START=$(grep -n '^{' /tmp/diet-trial-<repo>-<tool>-diet-raw.json | head -1 | cut -d: -f1)
if [ -n "$START" ]; then
  tail -n +$START /tmp/diet-trial-<repo>-<tool>-diet-raw.json > /tmp/diet-trial-<repo>-<tool>-diet.json
else
  cp /tmp/diet-trial-<repo>-<tool>-diet-raw.json /tmp/diet-trial-<repo>-<tool>-diet.json
fi
```

If `--no-source` was specified, omit `--source` flag.

### Step 4: Analyze Results

Parse the diet JSON and produce the analysis. Use Python for reliable JSON parsing:

```python
python3 << 'PYEOF'
import json, sys

with open("/tmp/diet-trial-<repo>-<tool>-diet.json") as f:
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

### Step 4b: Auto-File Issues for Anomalies

When anomalies are found, **automatically collect reproduction data and file a GitHub issue** for each distinct anomaly type. This is critical — the whole point of `/diet-trial` is to find and track bugs.

#### Anomaly Types and Evidence Collection

For each anomaly, collect the evidence needed to reproduce it:

**IMPORTS-BUT-NO-CALLS** (call site detection miss):
```bash
# 1. Extract the SBOM entry for this dependency
python3 -c "
import json
with open('/tmp/diet-trial-<repo>-<tool>-sbom.json') as f:
    d = json.load(f)
for c in d['components']:
    if '<dep-name>' in c.get('purl', '') or '<dep-name>' in c.get('name', ''):
        print(json.dumps(c, indent=2))
        break
"

# 2. Find actual import statements in source
grep -rn 'import.*<dep-import-pattern>' /tmp/diet-trial-<repo>/ --include='*.py' --include='*.go' --include='*.ts' --include='*.js' --include='*.java' | head -20

# 3. Find actual call sites (what diet missed)
grep -rn '<dep-module>\.' /tmp/diet-trial-<repo>/ --include='*.py' --include='*.go' --include='*.ts' --include='*.js' --include='*.java' | head -20

# 4. Extract diet JSON entry
python3 -c "
import json
with open('/tmp/diet-trial-<repo>-<tool>-diet.json') as f:
    d = json.load(f)
for dep in d['dependencies']:
    if '<dep-name>' in dep.get('name', ''):
        print(json.dumps(dep, indent=2))
        break
"
```

**EOL-ZERO-SCORE** (scoring anomaly):
```bash
# Extract the full diet entry including all score components
python3 -c "
import json
with open('/tmp/diet-trial-<repo>-<tool>-diet.json') as f:
    d = json.load(f)
for dep in d['dependencies']:
    if '<dep-name>' in dep.get('name', ''):
        print(json.dumps(dep, indent=2))
        break
"
```

**HIGH-SCORE-BUT-HARD** (unusual ranking):
```bash
# Same as above — full diet entry with all axes
```

#### Issue Filing

For each anomaly (or group of related anomalies from the same root cause), file an issue:

```bash
gh issue create \
  --repo future-architect/uzomuzo-oss \
  --label "bug,diet-trial" \
  --title "<anomaly-type>: <dep-name> in <org/repo> (<language>)" \
  --body "$(cat <<'ISSUE_EOF'
## Anomaly

**Type**: <IMPORTS-BUT-NO-CALLS | EOL-ZERO-SCORE | HIGH-SCORE-BUT-HARD>
**Project**: <org/repo>
**Language**: <Go | Python | JavaScript | TypeScript | Java>
**SBOM tool**: <trivy | syft> v<version>
**Date**: <YYYY-MM-DD>

## Description

<One sentence describing what looks wrong>

## Expected vs Actual

- **Expected**: <what the correct result should be>
- **Actual**: <what diet reported>

## Reproduction

```bash
git clone --depth 1 https://github.com/<org>/<repo>.git /tmp/diet-trial-<repo>
<sbom-tool> fs /tmp/diet-trial-<repo> --format cyclonedx -o /tmp/sbom.json
uzomuzo-diet --sbom /tmp/sbom.json --source /tmp/diet-trial-<repo> --format json
# Then check the entry for <dep-name>
```

## Evidence

### SBOM entry
```json
<sbom-component-json>
```

### Diet result entry
```json
<diet-entry-json>
```

### Source code references
```
<grep output showing actual imports/calls>
```

## Analysis

<Your assessment: why this might be happening, which phase (1-4) likely has the bug>

---
*Filed automatically by `/diet-trial` — [case study](<link-to-saved-report-if-available>)*
ISSUE_EOF
)"
```

#### Duplicate Check (MANDATORY before filing)

Before creating any issue, you MUST check for existing issues that cover the same bug:

```bash
# Step 1: Search by anomaly type + language (broad match)
gh issue list --repo future-architect/uzomuzo-oss --label "diet-trial" \
  --search "<anomaly-type> <language>" --state open --json number,title,body --limit 20

# Step 2: If Step 1 returns results, check if any cover the same root cause.
# Same root cause = same anomaly type + same language + same detection phase.
# Examples of "same root cause":
#   - "IMPORTS-BUT-NO-CALLS for Python deps" in flask AND django → same bug (Python call site detection)
#   - "IMPORTS-BUT-NO-CALLS for Go deps" in grafana AND terraform → same bug (Go call site detection)
#   - "IMPORTS-BUT-NO-CALLS for Go dep X" AND "IMPORTS-BUT-NO-CALLS for Python dep Y" → DIFFERENT bugs
```

**If a matching issue exists** → add a comment with new evidence:
```bash
gh issue comment <issue-number> --repo future-architect/uzomuzo-oss --body "$(cat <<'COMMENT_EOF'
## Additional evidence from <org/repo>

**Affected deps**: <list>
**SBOM tool**: <trivy|syft> v<version>

### Diet entry
```json
<diet-entry-json>
```

### Source references
```
<grep output>
```

---
*Added by `/diet-trial`*
COMMENT_EOF
)"
```

**If no matching issue exists** → create a new one (see Issue Filing above).

#### Guidelines

- **Group related anomalies**: If 5 Python deps all show IMPORTS-BUT-NO-CALLS, that's likely one bug (e.g., Python call site detection). File one issue, list all affected deps.
- **Severity hints**: Include in the issue body:
  - How many deps are affected in this project
  - Whether the same anomaly appeared in other trial runs
  - Whether it affects the priority ranking (high-rank deps with bugs = higher severity)
- **Do NOT file issues for expected behavior**: The anomaly check includes known false positives (e.g., config-driven deps like Spring Boot starters showing 0 calls is expected, not a bug). Use judgment.

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
2. If `/workspace/vuls-saas/vuls-diet/case-studies/uzomuzo-diet/` exists, save there under a language subdirectory
3. Otherwise save to the current project's `docs/case-studies/` (create if needed)

#### Language subdirectory

Determine the **primary language** from the SBOM ecosystem breakdown (the ecosystem with the most components) and save under that language's subdirectory:

| Primary ecosystem | Subdirectory |
|-------------------|-------------|
| golang | `go/` |
| npm | `typescript/` |
| pypi | `python/` |
| maven | `java/` |
| mixed (no clear majority) | `multi/` |

Create the subdirectory if it doesn't exist.

Filename format: `<repo>-<tool>-<YYYY-MM-DD>.<ext>`

All output types share the same base name, differentiated only by extension:

| Type | Extension | Description |
|------|-----------|-------------|
| Report | `.md` | Markdown analysis report |
| SBOM | `.sbom.json` | SBOM data (CycloneDX JSON) |
| Diet result | `.diet.json` | uzomuzo diet analysis result |

Example paths (for `flask` with `trivy` on `2026-04-07`):
- `case-studies/uzomuzo-diet/python/flask-trivy-2026-04-07.md`
- `case-studies/uzomuzo-diet/python/flask-trivy-2026-04-07.sbom.json`
- `case-studies/uzomuzo-diet/python/flask-trivy-2026-04-07.diet.json`

Save all three files together. Copy the SBOM and diet JSON from their `/tmp/` locations:

```bash
DEST="<case-studies-dir>/<language-subdir>"
BASE="<repo>-<tool>-<YYYY-MM-DD>"
cp /tmp/diet-trial-<repo>-<tool>-sbom.json "${DEST}/${BASE}.sbom.json"
cp /tmp/diet-trial-<repo>-<tool>-diet.json "${DEST}/${BASE}.diet.json"
# Report (.md) is written directly by the agent
```

The saved report must be in the same format as existing case studies in `case-studies/`. Use Japanese for section headers and commentary (matching existing case study style). Include:

- Execution conditions (tool versions, flags used)
- All tables from Step 5
- Raw data file paths
- Anomaly findings (important for bug tracking)

After saving, display all saved file paths to the user.

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
