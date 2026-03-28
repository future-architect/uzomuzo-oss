---
description: "OSS Select — evaluate candidate packages with uzomuzo and support adoption decisions"
argument-hint: |
  Specify candidate package PURLs:
  - Single package evaluation: 'pkg:golang/modernc.org/sqlite'
  - Compare multiple candidates: 'pkg:golang/modernc.org/sqlite pkg:golang/github.com/mattn/go-sqlite3'
  - Audit all go.mod dependencies: 'audit'
  - Specific ecosystem: 'audit --ecosystem npm'
agent: "agent"
model: ["claude-opus-4.6"]
---

# OSS Select — OSS Health Evaluation and Selection Support with uzomuzo

Support adoption decisions for new OSS packages, or audit the health of existing dependencies.

---

## S1 Mode Detection

Determine the mode from the user message:

| Mode | Trigger | Behavior |
|------|---------|----------|
| **compare** | Multiple PURLs specified | Compare candidates and present recommendation |
| **evaluate** | Single PURL specified | Detailed evaluation of one package |
| **audit** | `audit` keyword | Bulk check all go.mod dependencies |

---

## S2 Package Evaluation

### S2.1 Run uzomuzo

```bash
# When PURLs are directly specified
GOWORK=off go run . <purl1> [purl2 ...]

# Audit mode: generate PURL list from go.mod
GOWORK=off go list -m -json all | python3 -c "
import json, sys
for line in sys.stdin.read().split('\n}\n'):
    line = line.strip()
    if not line: continue
    if not line.endswith('}'): line += '}'
    try:
        m = json.loads(line)
        path = m.get('Path', '')
        if path and not path.startswith('github.com/vuls-saas/') and not path.startswith('github.com/future-architect/'):
            print(f'pkg:golang/{path}')
    except: pass
" > /tmp/oss-select-purls.txt
GOWORK=off go run . $(cat /tmp/oss-select-purls.txt | tr '\n' ' ')
```

### S2.2 Read CSV Data

uzomuzo outputs detailed data to `uzomuzo-catalog.csv`. Read this for structured data.

---

## S3 Evaluation Report Output

### S3.1 Single Package Evaluation (evaluate mode)

Output in the following format:

```markdown
## pkg:golang/modernc.org/sqlite

| Indicator | Value | Verdict |
|-----------|-------|---------|
| Lifecycle | Active | ✅ |
| Scorecard | 5.3/10 | ⚠️ |
| Maintained | 10/10 | ✅ |
| Code-Review | 2/10 | ⚠️ |
| Vulnerabilities | 0 | ✅ |
| Stars | 5,000+ | ✅ |
| Last Release | 18 days ago | ✅ |
| License | BSD | ✅ |
| Archived | No | ✅ |

### Overall Verdict: Approved
- Strengths: Actively maintained, no vulnerabilities, pure-Go (no CGO required)
- Concerns: Single maintainer (bus factor=1), low Code-Review score
```

### S3.2 Compare Mode (compare)

Present candidates side by side:

```markdown
## Comparison: SQLite Drivers

| Indicator | modernc.org/sqlite | mattn/go-sqlite3 |
|-----------|--------------------|------------------|
| Lifecycle | Active | Active |
| Scorecard | 5.3 | 6.1 |
| CGO Required | No | **Yes** |
| Stars | 5,000 | 8,000 |
| ... | ... | ... |

### Recommendation: modernc.org/sqlite
Reason: No CGO requirement simplifies CI configuration. Scorecard is slightly lower but Maintained=10 indicates active development.
```

### S3.3 Audit Mode

Summary of all dependencies + highlight problematic packages:

```markdown
## Dependency Health Report

| Status | Count |
|--------|-------|
| ✅ Active | 8 |
| ⚠️ Stalled | 3 |
| ❌ EOL | 1 |

### Action Required
| Package | Status | Action |
|---------|--------|--------|
| Masterminds/semver v1 | EOL | Migrate to semver/v3 |

### Watch
| Package | Status | Reason |
|---------|--------|--------|
| google/uuid | Stalled | Maintained=0, but managed by Google with no vulnerabilities |
```

---

## S4 Adoption Criteria

Apply the following criteria for automatic verdicts:

| Verdict | Condition |
|---------|-----------|
| **✅ Approved** | Active AND Vulnerabilities=0 AND OSI-approved license |
| **⚠️ Conditional** | Stalled but no vulnerabilities, feature-complete (mature library) |
| **❌ Not Approved** | EOL / Archived / has vulnerabilities / incompatible license |
| **🔍 Needs Investigation** | Review Needed / insufficient data |

### Additional Checks (beyond uzomuzo output)

- **Transitive dependency count**: Check with `go mod graph | wc -l`. Flag if excessive
- **CGO dependency**: Check with `go list -deps` for cgo usage
- **Alternative packages**: For EOL/Stalled packages, suggest Active alternatives with equivalent functionality

---

## S5 Notes

### PURL Version Paths

For Go modules with major version suffixes (`/v2`, `/v3`, etc.),
**always evaluate with the suffix-included PURL**.

```
❌ pkg:golang/github.com/Masterminds/semver     → evaluates v1 (EOL)
✅ pkg:golang/github.com/Masterminds/semver/v3  → evaluates v3 (Active)
```

The safest approach is to convert go.mod `require` paths directly to PURLs.

### Audit Mode Execution

- Exclude internal packages (`vuls-saas/*`, `future-architect/*`)
- Check all dependencies including `// indirect` (indirect dependencies are also attack targets)
