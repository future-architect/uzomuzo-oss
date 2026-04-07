# Diet Trial Report: spring-projects/spring-boot

## Execution Conditions

| Item | Value |
|------|-------|
| Project | [spring-projects/spring-boot](https://github.com/spring-projects/spring-boot) |
| Date | 2026-04-07 |
| SBOM Tool | cdxgen v12.1.4 (CycloneDX Generator) |
| Diet Tool | uzomuzo-diet (built from source) |
| Flags | `--sbom --source --format json` |
| GITHUB_TOKEN | Not set (local-only analysis) |
| Clone | Shallow (`--depth 1`) |
| Note | Maven/Gradle not available; cdxgen parsed `spring-boot-dependencies` BOM statically |

## SBOM Summary

| Metric | Value |
|--------|-------|
| Total Components | 649 |
| maven | 649 |
| Primary Language | Java |

## Diet Summary

| Metric | Value |
|--------|-------|
| total_direct | 649 |
| total_transitive | 0 |
| unused_direct | 132 |
| easy_wins | 94 |
| actionable_direct | 132 |

## Difficulty Distribution

| Difficulty | Count |
|------------|-------|
| trivial | 132 |
| easy | 39 |
| moderate | 71 |
| hard | 407 |

## Lifecycle Distribution

| Lifecycle | Count |
|-----------|-------|
| Active | 561 |
| Stalled | 43 |
| Review Needed | 25 |
| Legacy-Safe | 18 |
| EOL-Effective | 1 |
| EOL-Confirmed | 1 |

## EOL/Archived Dependencies Used in Code

| Difficulty | Name | Files | Calls | APIs | Lifecycle |
|------------|------|-------|-------|------|-----------|
| hard | org.slf4j/slf4j-log4j12 | 30 | 69 | 18 | EOL-Confirmed |

## Recommended Action Phases

### Phase 1: Trivial (132 deps)
Unused or zero-coupling dependencies. Safe to remove or ignore.

### Phase 2: Easy Wins (39 deps)
Low coupling effort. Can be replaced with minimal code changes.

### Phase 3: Moderate (71 deps)
Moderate coupling. Requires planned migration effort.

### Phase 4: Hard (407 deps)
High coupling and/or deeply integrated. Requires significant refactoring.

## Top 20 by Priority Score

| Rank | Score | Difficulty | Name | Lifecycle |
|------|-------|------------|------|-----------|
| 1 | 0.43 | trivial | org.codehaus.janino/janino | Legacy-Safe |
| 2 | 0.43 | trivial | org.codehaus.janino/commons-compiler | Legacy-Safe |
| 3 | 0.43 | trivial | jaxen/jaxen | Stalled |
| 4 | 0.43 | trivial | javax.money/money-api | Legacy-Safe |
| 5 | 0.38 | trivial | com.oracle.database.jdbc/ojdbc11 | Review Needed |
| 6 | 0.38 | trivial | com.oracle.database.jdbc/ucp11 | Review Needed |
| 7 | 0.38 | trivial | com.mysql/mysql-connector-j | Review Needed |
| 8 | 0.43 | trivial | org.glassfish.jersey.ext/jersey-spring6 | Stalled |
| 9 | 0.43 | trivial | jakarta.json/jakarta.json-api | Stalled |
| 10 | 0.43 | trivial | org.eclipse.parsson/parsson | Stalled |

## Anomaly Check

### IMPORTS-BUT-NO-CALLS (38 anomalies)

Annotation libraries with high import counts but zero call sites:
- `org.jspecify/jspecify` — 2,787 files, 0 calls
- `org.reactivestreams/reactive-streams` — 10 files, 0 calls
- `jakarta.annotation/jakarta.annotation-api` — 7 files, 0 calls

See [#212](https://github.com/future-architect/uzomuzo-oss/issues/212) for root cause analysis.

### EOL-ZERO-SCORE (1 anomaly)

- `org.slf4j/slf4j-log4j12` — hard, EOL-Confirmed, score=0.00

See [#214](https://github.com/future-architect/uzomuzo-oss/issues/214) for root cause analysis.

## Raw Data

- SBOM: `/tmp/diet-trial-spring-boot-sbom.json`
- Diet JSON: `/tmp/diet-trial-spring-boot-diet.json`
- Diet log: `/tmp/diet-trial-spring-boot-diet.log`
