# Diet Trial Report: apache/flink

## Execution Conditions

| Item | Value |
|------|-------|
| Project | [apache/flink](https://github.com/apache/flink) |
| Date | 2026-04-07 |
| SBOM Tool | cdxgen v12.1.4 (CycloneDX Generator) |
| Diet Tool | uzomuzo-diet (built from source) |
| Flags | `--sbom --source --format json` |
| GITHUB_TOKEN | Not set (local-only analysis) |
| Clone | Shallow (`--depth 1`) |
| Note | Maven not available; cdxgen parsed 177 pom.xml files statically (direct deps only, 39 duplicate entries) |

## SBOM Summary

| Metric | Value |
|--------|-------|
| Total Components | 199 |
| maven | 199 |
| Primary Language | Java |

## Diet Summary

| Metric | Value |
|--------|-------|
| total_direct | 199 |
| total_transitive | 0 |
| unused_direct | 75 |
| easy_wins | 35 |
| actionable_direct | 75 |

## Difficulty Distribution

| Difficulty | Count |
|------------|-------|
| trivial | 75 |
| easy | 21 |
| moderate | 16 |
| hard | 87 |

## Lifecycle Distribution

| Lifecycle | Count |
|-----------|-------|
| Active | 167 |
| Stalled | 14 |
| Legacy-Safe | 10 |
| Archived | 3 |
| EOL-Effective | 3 |
| Review Needed | 2 |

## EOL/Archived Dependencies Used in Code

All 6 EOL/Archived dependencies are **trivial** (no detected source coupling):

| Difficulty | Name | Version | Lifecycle |
|------------|------|---------|-----------|
| trivial | javax.activation/javax.activation-api | 1.2.0 | Archived |
| trivial | javax.xml.bind/jaxb-api | 2.3.1 | Archived |
| trivial | org.codehaus.jackson/jackson-mapper-asl | 1.9.14 | EOL-Effective |
| trivial | commons-configuration/commons-configuration | 1.7 | EOL-Effective |

## Stalled Dependencies (non-trivial)

| Difficulty | Name | Version | Files | Calls |
|------------|------|---------|-------|-------|
| easy | org.joda/joda-convert | 1.7 | 2 | 1 |
| easy | com.knuddels/jtokkit | 1.1.0 | 1 | 13 |
| moderate | net.razorvine/pyrolite | 4.13 | — | — |
| hard | org.slf4j/slf4j-api | 1.7.36 | — | — |
| hard | junit/junit | 4.13.2 | — | — |

## Recommended Action Phases

### Phase 1: Trivial (75 deps)
Unused dependencies including javax.* APIs and Jackson 1.x. Safe to remove.

### Phase 2: Easy Wins (21 deps)
Low coupling. Includes joda-convert, jtokkit.

### Phase 3: Moderate (16 deps)
Moderate coupling. Includes pyrolite (Python interop).

### Phase 4: Hard (87 deps)
High coupling. Core logging, testing, and framework dependencies.

## Top 20 by Priority Score

| Rank | Score | Difficulty | Name | Lifecycle |
|------|-------|------------|------|-----------|
| 1 | 0.51 | trivial | javax.activation/javax.activation-api | Archived |
| 2 | 0.51 | trivial | javax.xml.bind/jaxb-api | Archived |
| 3 | 0.43 | trivial | net.sf.py4j/py4j | Stalled |
| 4 | 0.43 | trivial | org.javassist/javassist | Stalled |
| 5 | 0.43 | trivial | com.lmax/disruptor | Stalled |
| 6 | 0.41 | trivial | com.ververica/frocksdbjni | Legacy-Safe |
| 7 | 0.41 | trivial | org.jcuda/jcublas | Stalled |
| 8 | 0.38 | trivial | com.facebook.presto.hadoop/hadoop-apache2 | Review Needed |
| 9 | 0.35 | trivial | com.google.code.findbugs/jsr305 | Legacy-Safe |
| 10 | 0.35 | trivial | org.codehaus.jackson/jackson-mapper-asl | EOL-Effective |

## Anomaly Check

### IMPORTS-BUT-NO-CALLS (11 anomalies)

Notable annotation/JNI libraries:
- `org.immutables/value` — 74 files, 0 calls (annotation processor)
- `org.checkerframework/checker-qual` — 56 files, 0 calls (annotation)
- `com.aliyun.oss/aliyun-sdk-oss` — 7 files, 0 calls
- `org.xerial.snappy/snappy-java` — 1 file, 0 calls (JNI)

See [#212](https://github.com/future-architect/uzomuzo-oss/issues/212) for annotation root cause.

## Raw Data

- SBOM: `/tmp/diet-trial-flink-sbom.json`
- Diet JSON: `/tmp/diet-trial-flink-diet.json`
- Diet log: `/tmp/diet-trial-flink-diet.log`
