---
description: "OSS Health Judge — extract pending records from oss-catalog.db, assess health via LLM, and write back"
argument-hint: |
  Specify processing mode:
  - Process 100 pending records (default): 'judge'
  - Limit count: 'judge --limit 50'
  - Filter by ecosystem: 'judge --ecosystem npm'
  - Re-judge stale records: 'judge --filter stale --days 30'
  - New additions only: 'judge --filter new --days 7'
  - Specific PURLs: 'judge pkg:npm/express pkg:npm/lodash'
agent: "agent"
model: ["claude-opus-4.6"]
---

# OSS Health Judge — LLM-Based OSS Health Assessment

Extract target records from oss-catalog.db, assess each OSS package's health, and write results back to the DB.
Executes the full extract → judge → import flow.

---

## S1 Preparation

### S1.1 Verify DB File

1. Check if `oss-catalog.db` exists in the workspace
2. If not found, suggest the user run `gh release download --pattern "oss-catalog.db" --output oss-catalog.db --clobber`
3. Keep the DB path for this session (default: `./oss-catalog.db`)

### S1.2 Parse Arguments

Parse the following from the user message:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `--filter` | `pending` | `pending` / `stale` / `new` / `status` |
| `--limit` | `100` | Number of records to process at once |
| `--ecosystem` | (all) | Ecosystem filter |
| `--days` | `30` | Days threshold for stale/new filter |
| `--health-status` | - | Target status (used with `--filter status`) |
| PURL list | - | Directly specify PURLs |

---

## S2 Record Extraction

### S2.1 Run Extract Command

> **Note**: `catalog-health-extract` and `catalog-health-import` are subcommands available
> only in the **uzomuzo-catalog** repository. When using this prompt in uzomuzo-oss,
> adapt the commands to the available CLI (see `go run . --help`).

```bash
go run . catalog-health-extract --db <DB_PATH> --filter <FILTER> --limit <LIMIT> [--ecosystem <ECO>] [--days <DAYS>] [--health-status <STATUS>] > /tmp/oss-health-batch.json
```

### S2.2 Verify Extraction Results

- Display the number of extracted records
- If 0 records, report "No target records found" and stop

---

## S3 Health Assessment

### S3.1 Assessment Rules

Evaluate each record using the following indicators to determine `health_status` and `health_description_ja`.

#### health_status Definitions

| Status | Criteria |
|--------|----------|
| `healthy` | Actively maintained. High Scorecard, active releases, healthy community |
| `warning` | Some concerns. Stalling releases, low Scorecard sub-scores, high bot commit ratio, etc. |
| `critical` | Serious concerns. Archived, no releases for extended period, unpatched vulnerabilities, EOL |
| `unknown` | Insufficient data for assessment |

#### Indicators Used for Assessment

1. **lifecycle_label** — Active adds points, Stalled/EOL deducts
2. **overall_score** — OpenSSF Scorecard overall score (0-10)
3. **scorecard_checks** — Individual checks (especially Maintained, Vulnerabilities, Code-Review)
4. **latest_stable** — Last stable release date (older = lower score)
5. **advisory_count** — Known vulnerability count
6. **stars / dependent_count** — Community scale (reference indicator)
7. **bot_commit_ratio** — Concern if bot ratio is too high
8. **is_archived / is_disabled** — Archived = critical candidate
9. **days_since_last_commit** — Long inactivity is a concern

#### health_description_ja Writing Guidelines

- Write concisely in 2-4 sentences in Japanese
- Include both strengths and concerns
- Cite specific numbers (e.g., "last release was 2024-03-15")
- Mention alternative packages if applicable
- Write for FutureVuls users (security administrators) as the audience

### S3.2 Batch Processing

Read each record from the extracted JSON and generate assessment results based on the above rules.

Output format:
```json
[
  {
    "purl": "pkg:npm/express",
    "health_status": "healthy",
    "health_description_ja": "Actively maintained..."
  }
]
```

### S3.3 Review Assessment Results

Present results to the user for review:
- Display status distribution (healthy: N, warning: N, critical: N)
- List all critical records
- Apply corrections if the user requests changes

### S3.4 Save Judged Results

After review and any corrections, write the final judged results to the batch file:

```bash
# Overwrite the batch file with judged results (same path used by import)
python3 -c "import json; json.dump(results, open('/tmp/oss-health-batch.json', 'w'), indent=2)"
```

---

## S4 Write Back to DB

### S4.1 Run Import Command

After user approves the results:

```bash
go run . catalog-health-import --db <DB_PATH> < /tmp/oss-health-batch.json
```

### S4.2 Report Results

- Display the number of updated records
- Display overall DB coverage (assessed / pending)

---

## S5 Error Handling

- DB file not found: suggest `gh release download --pattern "oss-catalog.db" --output oss-catalog.db --clobber`
- Extract returns 0 records: suggest changing filter conditions
- Import failure: display error details and suggest retry
