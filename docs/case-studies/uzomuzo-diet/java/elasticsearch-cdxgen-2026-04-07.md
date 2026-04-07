# Diet Trial Report: elastic/elasticsearch

## Execution Conditions

| Item | Value |
|------|-------|
| Project | [elastic/elasticsearch](https://github.com/elastic/elasticsearch) |
| Date | 2026-04-07 |
| SBOM Tool | cdxgen v12.1.4 (CycloneDX Generator) |
| Diet Tool | uzomuzo-diet (built from source) |
| Flags | `--sbom --source --format json` |
| GITHUB_TOKEN | Not set (local-only analysis) |
| Clone | Shallow (`--depth 1`) |
| Note | Java/Gradle not available; cdxgen parsed `gradle/verification-metadata.xml` (flat graph, no transitive depth) |

## SBOM Summary

| Metric | Value |
|--------|-------|
| Total Components | 1,183 |
| maven | 1,183 |
| Primary Language | Java |

## Diet Summary

| Metric | Value |
|--------|-------|
| total_direct | 1,183 |
| total_transitive | 0 |
| unused_direct | 612 |
| easy_wins | 165 |
| actionable_direct | 612 |

## Difficulty Distribution

| Difficulty | Count |
|------------|-------|
| trivial | 612 |
| easy | 111 |
| moderate | 63 |
| hard | 397 |

Bimodal distribution: 52% trivial (unused) vs 34% hard (deeply embedded).

## Lifecycle Distribution

| Lifecycle | Count |
|-----------|-------|
| Active | 971 |
| Legacy-Safe | 81 |
| Stalled | 58 |
| EOL-Effective | 29 |
| Review Needed | 28 |
| Archived | 16 |

## EOL/Archived Dependencies Used in Code

| Difficulty | Name | Files | Calls | APIs | Lifecycle |
|------------|------|-------|-------|------|-----------|
| hard | org.opensaml/opensaml-core | ~80 | ~523 | ~115 | EOL-Effective |
| hard | org.opensaml/opensaml-saml-api | (grouped) | (grouped) | (grouped) | EOL-Effective |
| hard | org.opensaml/opensaml-saml-impl | (grouped) | (grouped) | (grouped) | EOL-Effective |
| hard | org.opensaml/opensaml-security-api | (grouped) | (grouped) | (grouped) | EOL-Effective |
| hard | org.opensaml/opensaml-xmlsec-api | (grouped) | (grouped) | (grouped) | EOL-Effective |
| hard | (+ 5 more opensaml modules) | | | | EOL-Effective |
| hard | com.squareup/javapoet | 17 | 183 | 7 | Archived |
| moderate | com.wdtinc/mapbox-vector-tile | 8 | 10 | 7 | EOL-Effective |
| moderate | org.reactivestreams/reactive-streams-tck | 5 | 9 | 6 | EOL-Effective |
| moderate | javax.annotation/javax.annotation-api | 13 | 2 | 1 | Archived |

**OpenSAML (10 modules)** is the highest-risk finding: deeply embedded SAML authentication library, all EOL-Effective.

## Recommended Action Phases

### Phase 1: Trivial (612 deps)
Unused or zero-coupling dependencies. Many are likely transitive (flat graph artifact).

### Phase 2: Easy Wins (111 deps)
Low coupling effort. Can be replaced with minimal code changes.

### Phase 3: Moderate (63 deps)
Moderate coupling. Includes mapbox-vector-tile, reactive-streams-tck.

### Phase 4: Hard (397 deps)
High coupling. Includes OpenSAML (10 modules), javapoet, slf4j.

## Top 20 by Priority Score

| Rank | Score | Difficulty | Name | Lifecycle |
|------|-------|------------|------|-----------|
| 1 | 0.52 | trivial | io.sgr/s2-geometry-library-java | Archived |
| 2 | 0.51 | trivial | javax.servlet/javax.servlet-api | Archived |
| 3 | 0.51 | trivial | org.sonatype.plexus/plexus-cipher | Archived |
| 4 | 0.51 | trivial | org.codehaus.plexus/plexus-component-annotations | Archived |
| 5 | 0.51 | trivial | org.codehaus.plexus/plexus-container-default | Archived |
| 6 | 0.49 | trivial | io.opencensus/opencensus-api | Archived |
| 7 | 0.49 | trivial | io.opencensus/opencensus-contrib-http-util | Archived |
| 8 | 0.49 | trivial | javax.xml.bind/jaxb-api | Archived |
| 9 | 0.49 | trivial | org.apache.htrace/htrace-core4 | Archived |
| 10 | 0.49 | trivial | org.glassfish/javax.json | Archived |

## Anomaly Check

### IMPORTS-BUT-NO-CALLS (50 anomalies)

Notable:
- `javax.inject/javax.inject` — 90 files, 0 calls (`@Inject` annotation)
- `org.jetbrains/annotations` — 8 files, 0 calls
- `com.github.luben/zstd-jni` — 4 files, 0 calls (JNI native)
- `org.xerial.snappy/snappy-java` — 2 files, 0 calls (JNI native)

See [#212](https://github.com/future-architect/uzomuzo-oss/issues/212) for annotation root cause.

### EOL-ZERO-SCORE (20 anomalies)

All OpenSAML modules: EOL-Effective + hard, score=0.00.

See [#214](https://github.com/future-architect/uzomuzo-oss/issues/214) for scoring root cause.

## Raw Data

- SBOM: `/tmp/diet-trial-elasticsearch-sbom.json`
- Diet JSON: `/tmp/diet-trial-elasticsearch-diet.json`
- Diet log: `/tmp/diet-trial-elasticsearch-diet.log`
