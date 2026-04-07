# Diet Trial Report: google/guava

## Execution Conditions

| Item | Value |
|------|-------|
| Project | [google/guava](https://github.com/google/guava) |
| Date | 2026-04-07 |
| SBOM Tool | cdxgen v12.1.4 (CycloneDX Generator) |
| Diet Tool | uzomuzo-diet (built from source) |
| Flags | `--sbom --source --format json` |
| GITHUB_TOKEN | Not set (local-only analysis) |
| Clone | Shallow (`--depth 1`) |
| Note | Maven not available; cdxgen parsed pom.xml statically (direct deps only) |

## SBOM Summary

| Metric | Value |
|--------|-------|
| Total Components | 5 |
| maven | 5 |
| Primary Language | Java |

## Diet Summary

| Metric | Value |
|--------|-------|
| total_direct | 5 |
| total_transitive | 0 |
| unused_direct | 2 |
| easy_wins | 1 |
| actionable_direct | 2 |

## Difficulty Distribution

| Difficulty | Count |
|------------|-------|
| trivial | 2 |
| easy | 0 |
| moderate | 3 |
| hard | 0 |

## Lifecycle Distribution

| Lifecycle | Count |
|-----------|-------|
| Active | 5 |

## EOL/Archived Dependencies Used in Code

No EOL or Archived dependencies detected.

## Recommended Action Phases

### Phase 1: Trivial (2 deps)
- `org.codehaus.plexus/plexus-io` — unused, 1 vulnerability
- `org.ow2.asm/asm` — unused

### Phase 2-4: None applicable

## Top 5 by Priority Score

| Rank | Score | Difficulty | Name | Lifecycle |
|------|-------|------------|------|-----------|
| 1 | 0.31 | trivial | org.codehaus.plexus/plexus-io@3.6.0 | Active |
| 2 | 0.29 | trivial | org.ow2.asm/asm@9.9.1 | Active |
| 3 | 0.01 | moderate | com.google.j2objc/j2objc-annotations@3.1 | Active |
| 4 | 0.01 | moderate | org.jspecify/jspecify@1.0.0 | Active |
| 5 | 0.01 | moderate | com.google.errorprone/error_prone_annotations@2.47.0 | Active |

## Anomaly Check

### IMPORTS-BUT-NO-CALLS (3 anomalies)

All are annotation libraries:
- `org.jspecify/jspecify` — 2,374 files, 0 calls
- `com.google.errorprone/error_prone_annotations` — 728 files, 0 calls
- `com.google.j2objc/j2objc-annotations` — 147 files, 0 calls

See [#212](https://github.com/future-architect/uzomuzo-oss/issues/212) for root cause analysis.

## Raw Data

- SBOM: `/tmp/diet-trial-guava-sbom.json`
- Diet JSON: `/tmp/diet-trial-guava-diet.json`
- Diet log: `/tmp/diet-trial-guava-diet.log`
