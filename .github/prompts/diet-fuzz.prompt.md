---
description: "Batch diet-trial across multiple OSS projects to fuzz-test uzomuzo-diet's detection accuracy"
---

# /diet-fuzz — Batch Diet Fuzz Testing

Run uzomuzo diet-trial on many OSS projects across multiple languages to fuzz-test parser accuracy, find detection bugs, and accumulate evidence for known issues.

## Arguments

Parse the user's arguments:

- **`<languages|all>`** (required): Comma-separated language list (`go,python,typescript,js`) or `all` (= `go,python,typescript,js`)
- **`--count N`** (optional, default: `5`): Number of projects per language
- **`--tool <trivy,syft,cdxgen>`** (optional, default: all three): Comma-separated SBOM tools to use. Each project is analyzed with every specified tool.
- **`--max-parallel N`** (optional, default: `4`): Maximum concurrent agents
- **`--projects-file <path>`** (optional): Path to a file listing `org/repo` entries (one per line, with optional `# language` suffix). When provided, skip auto-selection.

### Examples

```bash
/diet-fuzz all                              # 5 projects × 4 languages × 3 tools
/diet-fuzz go,typescript --count 10         # 10 projects × 2 languages × 3 tools
/diet-fuzz python --tool syft --count 3     # 3 Python projects × syft only
/diet-fuzz all --projects-file projects.txt # Use curated list
```

## Pipeline Overview

```
1. Pull & Build    → git pull origin/main, rebuild uzomuzo-diet
2. Select Projects → stratified sampling or projects-file
3. Pre-filter      → clone + SBOM, skip if dependency graph empty
4. Diet Analysis   → run uzomuzo-diet per project × tool
5. Anomaly Detect  → find IMPORTS-BUT-NO-CALLS, EOL-ZERO-SCORE, etc.
6. Compare Past    → diff against previous run JSONs
7. File Issues     → auto-create or comment on existing issues
8. Record Findings → append new insights to findings.md
9. Report          → display cross-language summary table
```

## Phase 1: Pull & Build

**Always rebuild from latest origin/main** to avoid stale binary issues.

```bash
cd <project-root>
git fetch origin main
git checkout main
git pull origin main
mkdir -p ~/bin
CGO_ENABLED=1 go build -o ~/bin/uzomuzo-diet ./cmd/uzomuzo-diet
export PATH="$HOME/bin:$PATH"
```

Verify SBOM tools are installed. Install any missing ones:

- **trivy**: `curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b ~/bin`
- **syft**: `curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b ~/bin`
- **cdxgen**: `npm install -g @cyclonedx/cdxgen` (requires Node.js)

Record tool versions for the run.

## Phase 2: Project Selection

### When `--projects-file` is provided

Read the file. Each line is `org/repo` with optional `# language` comment:

```
cli/cli # go
pallets/flask # python
prisma/prisma # typescript
expressjs/express # js
```

If no language comment, detect from the repo's primary language via GitHub API.

### When auto-selecting (no projects-file)

Use **stratified sampling** for diversity across time periods, popularity, and coding styles.

#### Strata (per language)

| Stratum | Filter | Share |
|---------|--------|-------|
| **Popular** | stars > 10000 | 20% |
| **Mid-tier** | stars 1000–10000 | 30% |
| **Niche** | stars 100–1000 | 30% |
| **Legacy** | created before 2018 | 20% |

#### Selection Algorithm

```bash
# For each language and stratum, search GitHub:
gh api "search/repositories?q=language:<lang>+stars:<range>+created:<range>&sort=updated&per_page=50"
```

For each candidate:
1. Check that a dependency manifest exists for the language:
   - Go: `go.mod`
   - Python: `requirements.txt` OR `Pipfile.lock` OR `poetry.lock` (not `pyproject.toml` alone — syft cannot resolve it)
   - TypeScript/JS: `package-lock.json` OR `yarn.lock` OR `pnpm-lock.yaml`
2. Exclude archived repositories
3. Exclude forks
4. Randomize within each stratum, then pick to fill the quota

If a stratum cannot fill its quota (e.g., not enough legacy Go repos), redistribute to other strata.

#### Deduplication

Track all previously analyzed projects in `docs/case-studies/uzomuzo-diet/` directory names. When auto-selecting, **prefer projects not yet analyzed** to maximize coverage. If `--count` exceeds available new projects, allow repeats (useful for regression testing).

## Phase 3: Pre-filter (Clone + SBOM Validation)

For each selected project × each SBOM tool:

### 3a. Clone

```bash
git clone --depth 1 https://github.com/<org>/<repo>.git /tmp/diet-fuzz-<repo>
```

Skip if already exists.

### 3b. Generate SBOM

**Trivy:**
```bash
trivy fs /tmp/diet-fuzz-<repo> --format cyclonedx -o /tmp/diet-fuzz-<repo>-trivy-sbom.json 2>/tmp/diet-fuzz-<repo>-trivy-sbom.log
```

**Syft:**
```bash
syft scan /tmp/diet-fuzz-<repo> --source-name <repo> -o cyclonedx-json > /tmp/diet-fuzz-<repo>-syft-sbom.json 2>/tmp/diet-fuzz-<repo>-syft-sbom.log
```

**cdxgen:**
```bash
cdxgen -o /tmp/diet-fuzz-<repo>-cdxgen-sbom.json /tmp/diet-fuzz-<repo> 2>/tmp/diet-fuzz-<repo>-cdxgen-sbom.log
```

### 3c. Validate

Check each SBOM for non-empty dependency graph:

```python
python3 -c "
import json, sys
with open('/tmp/diet-fuzz-<repo>-<tool>-sbom.json') as f:
    d = json.load(f)
comps = d.get('components', [])
has_deps = bool(d.get('dependencies', []))
purl_comps = [c for c in comps if c.get('purl', '').startswith('pkg:')]
print(f'Components: {len(comps)}, PURLs: {len(purl_comps)}, Has dependency graph: {has_deps}')
if len(purl_comps) == 0 or not has_deps:
    print('SKIP: empty dependency graph')
    sys.exit(1)
"
```

If validation fails for ALL tools, **skip this project and select a replacement** (next candidate from the same stratum). If validation fails for some tools but not others, proceed with the successful tools only.

## Phase 4: Diet Analysis

For each project × each valid SBOM tool:

```bash
export PATH="$HOME/bin:$PATH"
unset GITHUB_TOKEN
uzomuzo-diet --sbom /tmp/diet-fuzz-<repo>-<tool>-sbom.json --source /tmp/diet-fuzz-<repo> --format json 2>/tmp/diet-fuzz-<repo>-<tool>-diet.log > /tmp/diet-fuzz-<repo>-<tool>-diet-raw.json
```

Clean JSON (log lines may be mixed into stdout):

```bash
START=$(grep -n '^{' /tmp/diet-fuzz-<repo>-<tool>-diet-raw.json | head -1 | cut -d: -f1)
if [ -n "$START" ]; then
  tail -n +$START /tmp/diet-fuzz-<repo>-<tool>-diet-raw.json > /tmp/diet-fuzz-<repo>-<tool>-diet.json
else
  cp /tmp/diet-fuzz-<repo>-<tool>-diet-raw.json /tmp/diet-fuzz-<repo>-<tool>-diet.json
fi
```

### Save results

Create the run directory and save:

```bash
RUN_DIR="docs/case-studies/uzomuzo-diet/$(date +%Y-%m-%dT%H%M)"
DEST="${RUN_DIR}/<language>"
mkdir -p "$DEST"
cp /tmp/diet-fuzz-<repo>-<tool>-diet.json "${DEST}/<repo>-<tool>.json"
cp /tmp/diet-fuzz-<repo>-<tool>-sbom.json "${DEST}/<repo>-<tool>.sbom.json"
```

Determine `<language>` from SBOM ecosystem breakdown (most components):

| Primary ecosystem | Directory |
|-------------------|-----------|
| golang | `go/` |
| npm | `typescript/` or `javascript/` (based on presence of `.ts` files in repo) |
| pypi | `python/` |
| maven | `java/` |
| mixed | `multi/` |

## Phase 5: Anomaly Detection

For each diet result, detect anomalies:

```python
python3 << 'PYEOF'
import json

with open("/tmp/diet-fuzz-<repo>-<tool>-diet.json") as f:
    d = json.load(f)

deps = d["dependencies"]
eol_labels = {"EOL-Confirmed", "EOL-Effective", "EOL-Scheduled", "Archived"}

anomalies = []
for dep in deps:
    # Imported but no call sites detected
    if dep.get("import_file_count", 0) > 0 and dep.get("call_site_count", 0) == 0 and dep["difficulty"] != "trivial":
        anomalies.append({
            "type": "IMPORTS-BUT-NO-CALLS",
            "dep": dep["name"],
            "files": dep.get("import_file_count", 0),
            "detail": dep
        })
    # High priority but hard to replace
    if dep["priority_score"] > 0.3 and dep["difficulty"] == "hard":
        anomalies.append({
            "type": "HIGH-SCORE-BUT-HARD",
            "dep": dep["name"],
            "score": dep["priority_score"],
            "detail": dep
        })
    # EOL but near-zero score (scoring bug?)
    if dep.get("lifecycle", "") in eol_labels and dep["priority_score"] < 0.01 and dep["difficulty"] != "trivial":
        anomalies.append({
            "type": "EOL-ZERO-SCORE",
            "dep": dep["name"],
            "score": dep["priority_score"],
            "lifecycle": dep.get("lifecycle"),
            "detail": dep
        })
PYEOF
```

### Categorize anomalies

Group anomalies by **type × language** (same root cause = same issue):

- `IMPORTS-BUT-NO-CALLS` for TypeScript → likely same parser bug
- `IMPORTS-BUT-NO-CALLS` for Go → different parser, different bug
- `EOL-ZERO-SCORE` for any language → scoring logic bug

### Filter known expected behavior

Do NOT count these as anomalies (they are known limitations):

- **Config-driven tools**: eslint plugins (`eslint-plugin-*`), tailwindcss plugins, babel plugins — referenced in config files, not imports
- **Side-effect imports**: `@testing-library/jest-dom`, `isomorphic-fetch`, `jest-extended` — `import 'foo'` pattern
- **CLI tools used as binaries**: `wrangler`, `jest`, `ava`, `tsc` — invoked via CLI, not imported

When filtering, log filtered items separately as "known limitations" for the report.

## Phase 6: Compare with Previous Runs

Search for previous diet JSON files for the same project + tool combination:

```bash
# Find previous results for this project+tool across all run directories
find docs/case-studies/uzomuzo-diet/ -name "<repo>-<tool>.json" -not -path "*/<current-run-dir>/*" | sort -r | head -1
```

If a previous result exists, compute the diff:

```python
python3 << 'PYEOF'
import json

with open("<previous-path>") as f:
    prev = json.load(f)
with open("<current-path>") as f:
    curr = json.load(f)

prev_ibnc = sum(1 for d in prev["dependencies"] if d.get("import_file_count", 0) > 0 and d.get("call_site_count", 0) == 0 and d["difficulty"] != "trivial")
curr_ibnc = sum(1 for d in curr["dependencies"] if d.get("import_file_count", 0) > 0 and d.get("call_site_count", 0) == 0 and d["difficulty"] != "trivial")

prev_eol = sum(1 for d in prev["dependencies"] if d.get("lifecycle", "") in {"EOL-Confirmed", "EOL-Effective", "EOL-Scheduled", "Archived"} and d["difficulty"] != "trivial")
curr_eol = sum(1 for d in curr["dependencies"] if d.get("lifecycle", "") in {"EOL-Confirmed", "EOL-Effective", "EOL-Scheduled", "Archived"} and d["difficulty"] != "trivial")

print(f"IBNC: {prev_ibnc} → {curr_ibnc} ({curr_ibnc - prev_ibnc:+d})")
print(f"EOL (non-trivial): {prev_eol} → {curr_eol} ({curr_eol - prev_eol:+d})")
PYEOF
```

Flag regressions (IBNC increased) prominently in the report.

## Phase 7: Auto-File Issues

### Duplicate Check (MANDATORY)

Before filing any issue, check for existing open issues with the same anomaly type + language:

```bash
gh api "search/issues?q=repo:future-architect/uzomuzo-oss+is:issue+is:open+label:diet-trial+<anomaly-type>+<language>" --jq '.items[] | {number, title}'
```

**Post-filter**: Verify the title matches the anomaly type and language (fuzzy search can return false positives).

### If matching issue exists → Add comment

```bash
gh api repos/future-architect/uzomuzo-oss/issues/<number>/comments \
  -f body="$(cat <<'COMMENT_EOF'
## Additional evidence from diet-fuzz run <YYYY-MM-DDTHHmm>

**Affected projects**: <list>
**SBOM tools**: <tools with versions>

| Project | Dependency | Files | Pattern |
|---------|-----------|-------|---------|
| ... | ... | ... | ... |

### Comparison with previous run

| Metric | Previous | Current | Delta |
|--------|----------|---------|-------|
| Total IBNC | X | Y | +/-Z |

---
*Added by `/diet-fuzz` run <YYYY-MM-DDTHHmm>*
COMMENT_EOF
)"
```

### If no matching issue → Create new

```bash
gh api repos/future-architect/uzomuzo-oss/issues \
  -f title="<anomaly-type>: <description> (<language>)" \
  -f body="$(cat <<'ISSUE_EOF'
## Anomaly

**Type**: <IMPORTS-BUT-NO-CALLS | EOL-ZERO-SCORE | HIGH-SCORE-BUT-HARD>
**Language**: <Go | Python | JavaScript | TypeScript>
**SBOM tools**: <trivy vX.Y, syft vX.Y, cdxgen vX.Y>
**Run**: <YYYY-MM-DDTHHmm> (diet-fuzz)

## Summary

| Metric | Value |
|--------|-------|
| Affected projects | N/M |
| Total anomalies | X |
| Most impacted | <project> (<dep>, N files) |

## Pattern

<Description of the parser pattern being missed>

## Evidence (by project)

### <org/repo>
| Dependency | Files | Pattern |
|-----------|-------|---------|
| ... | ... | ... |

<details>
<summary>Diet entry (<dep>)</summary>

```json
{ ... }
```
</details>

### <org/repo>
...

## Reproduction

```bash
git clone --depth 1 https://github.com/<org>/<repo>.git /tmp/test
<sbom-tool> scan /tmp/test -o cyclonedx-json > /tmp/sbom.json
uzomuzo-diet --sbom /tmp/sbom.json --source /tmp/test --format json
# Check: <dep> → import_file_count=N, call_site_count=0
```

## Comparison with previous run

| Run | Total IBNC | Affected projects |
|-----|-----------|-------------------|
| <prev-run> | X | N/M |
| **<current-run>** | **Y** | **N/M** |
| Delta | **+/-Z** | **+/-W** |

---
*Filed by `/diet-fuzz` run <YYYY-MM-DDTHHmm>*
ISSUE_EOF
)" \
  -f "labels[]=bug" \
  -f "labels[]=diet-trial" \
  -f "labels[]=lang:<language>"
```

### Issue Filing Rules

- **Group by root cause**: 1 anomaly type × 1 language = 1 Issue. Multiple projects and deps go into the same issue.
- **Use `gh api`** (REST) for all GitHub operations to avoid GraphQL rate limits.
- **Create language labels** if they don't exist:
  ```bash
  gh api repos/future-architect/uzomuzo-oss/labels -f name="lang:<language>" -f color="<color>" 2>/dev/null || true
  ```
  Colors: `lang:go` = `00ADD8`, `lang:python` = `3776AB`, `lang:typescript` = `3178C6`, `lang:javascript` = `F7DF1E`, `lang:java` = `B07219`

## Phase 8: Record Findings

After analysis, check for **new insights** not already in `docs/case-studies/uzomuzo-diet-findings.md`.

New insight criteria:
- A new anomaly pattern not previously documented
- A tool-specific limitation discovered (e.g., "syft cannot resolve pyproject.toml without lockfile")
- A regression or significant improvement in detection accuracy
- A cross-language comparison insight

If new insights exist, **append** them to the findings file:

```markdown
## <YYYY-MM-DD> — diet-fuzz run

- **<finding>**: <description with evidence>
```

If the file does not exist, create it with a header:

```markdown
# uzomuzo-diet Findings

Accumulated insights from diet-trial and diet-fuzz runs.

## <YYYY-MM-DD> — diet-fuzz run

- ...
```

Do NOT duplicate findings already present in the file. Read the file first and compare.

## Phase 9: Terminal Report

After all analysis is complete, display a structured summary to the terminal.

### Report Format

```
══════════════════════════════════════════════════
  diet-fuzz results — <YYYY-MM-DDTHHmm>
  Tools: trivy vX.Y, syft vX.Y, cdxgen vX.Y
  Projects: N (M languages)
══════════════════════════════════════════════════

=== CROSS-LANGUAGE SUMMARY ===

| Language | Projects | Success | Direct deps (avg) | IBNC | EOL | Anomalies |
|----------|----------|---------|-------------------|------|-----|-----------|
| Go       | 5        | 5       | 6.4               | 0    | 1   | 0         |
| Python   | 5        | 2       | 21                | 1    | 0   | 1         |
| TS       | 5        | 5       | 88                | 25   | 9   | 25        |
| JS       | 5        | 5       | 18.6              | 12   | 3   | 12        |

=== TOOL COMPARISON (per project) ===

| Project | trivy deps | syft deps | cdxgen deps | trivy IBNC | syft IBNC | cdxgen IBNC |
|---------|-----------|-----------|-------------|-----------|-----------|-------------|
| ...     | ...       | ...       | ...         | ...       | ...       | ...         |

=== ANOMALIES BY TYPE ===

| Type | Go | Python | TS | JS | Total |
|------|-----|--------|-----|-----|-------|
| IMPORTS-BUT-NO-CALLS | 0 | 1 | 25 | 12 | 38 |
| EOL-ZERO-SCORE | 0 | 0 | 0 | 0 | 0 |
| HIGH-SCORE-BUT-HARD | 0 | 0 | 0 | 0 | 0 |

=== COMPARISON WITH PREVIOUS RUN ===

| Language | Prev IBNC | Curr IBNC | Delta |
|----------|-----------|-----------|-------|
| ...      | ...       | ...       | ...   |

=== ISSUES FILED/UPDATED ===

- #242 — updated with new evidence (TypeScript IBNC)
- #244 — NEW: Python conditional import detection (Python IBNC)

=== NEW FINDINGS ===

- <finding summary>

══════════════════════════════════════════════════
  Saved to: docs/case-studies/uzomuzo-diet/<run-dir>/
══════════════════════════════════════════════════
```

## Execution Strategy

### Agent Orchestration

Launch up to `--max-parallel` agents concurrently. Each agent handles projects for one language with one SBOM tool:

```
max_parallel = 4, languages = [go, python, ts, js], tools = [trivy, syft, cdxgen]
→ 12 agent tasks total, 4 running at a time
```

Each agent:
1. Iterates through its assigned projects sequentially
2. Runs clone → SBOM → validate → diet → analyze for each
3. Returns structured results (JSON) to the orchestrator

The orchestrator (main conversation):
1. Launches agents up to max_parallel
2. As agents complete, launches next pending agent
3. After all agents complete, runs Phase 5-9 (anomaly detection, comparison, issue filing, findings, report)

### Agent Prompt Template

Each agent receives:
- Language and SBOM tool assignment
- List of projects to analyze
- Path to save results (`<run-dir>/<lang>/`)
- Instructions to return structured JSON summary (not file issues — the orchestrator does that)

## Error Handling

- **Clone failure**: Skip project, select replacement from same stratum
- **SBOM generation failure**: Log error, try next tool. If all tools fail, skip project.
- **Diet execution failure**: Log error with stderr content. Common: tree-sitter parse errors.
- **Zero dependencies**: SBOM likely empty/malformed. Skip this tool for this project.
- **JSON parse failure**: Apply cleanup step (strip log lines). If still fails, log and skip.
- **GitHub API rate limit**: Use `gh api` (REST) instead of `gh` CLI (GraphQL). If REST also limited, log and defer issue filing.
- **Agent failure**: Log the error, continue with remaining agents. Do not retry automatically.

## Notes

- **No GITHUB_TOKEN needed for diet**: Unset it before running. Diet accuracy depends on Graph + Coupling (local-only).
- **Shallow clone is sufficient**: Source coupling only needs current source.
- **Large repos** (>10k components): May take several minutes. Expected.
- **Run directory is immutable**: Once created, a run directory's contents should not be modified by later runs. Each run gets its own timestamped directory.
