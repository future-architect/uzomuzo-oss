# ADR-0015: Java Reflection Detection Strategy for diet IBNC Analysis

## Status

Accepted (amended by #303: CycloneDX scope integration)

## Context

`uzomuzo diet` uses tree-sitter AST analysis to detect import-but-not-called (IBNC) dependencies. For Java, this works well for compile-time couplings (method calls, constructors, type declarations, annotations, etc.), but fails for dependencies loaded via reflection at runtime.

Issue #300 proposes a `ScopeRuntime` whitelist for known reflection-loaded dependencies (JDBC drivers, logging backends, WebJars). Before accepting that approach, we needed evidence on whether tree-sitter-based reflection detection is a viable alternative or complement.

### Investigation: Reflection Patterns in 6 Java OSS Projects

We surveyed reflection usage in 6 real-world Java OSS projects: spring-petclinic, apolloconfig/apollo, mybatis/mybatis-3, google/gson, square/okhttp, and OpenFeign/feign.

#### Raw Reflection Counts (non-test code)

| Pattern | Count | AST Detectable? |
|---------|-------|-----------------|
| `Class.forName("literal")` | 8 | Yes |
| `Class.forName(variable/interpolation)` | 12 | No |
| `ServiceLoader.load` | 0 | — |
| `DriverManager.*` | 11 | No |
| `.newInstance()` | 93 | No |
| `getMethod/invoke` | 50 | No |
| **Total** | **174** | **8 (4.6%)** |

#### Critical Finding: Most Reflection Is Not Dependency Loading

Of 174 reflection call sites, only ~25 actually load external dependencies. The rest are:

- JAXP factory calls (`DocumentBuilderFactory.newInstance()`) — stdlib, not external deps
- Self-referential introspection (gson serializing user types, mybatis mapper proxies)
- JDK version probing (okhttp checking for `SSLSocket.getApplicationProtocol`)
- Array allocation (`Array.newInstance`)

#### Dep-Loading Reflection Detectability (~25 calls)

| Pattern | Example | Detectable? |
|---------|---------|-------------|
| OkHttp TLS provider probes | `Class.forName("org.bouncycastle.jsse...")` | **Yes** (3–4 calls) |
| OkHttp Android platform probes | `Class.forName("$packageName.OpenSSL*")` | Partial (dynamic prefix) |
| MyBatis JDBC driver loading | `Class.forName(driverName)` from XML config | No |
| MyBatis plugin/TypeHandler loading | `resolveClass(props.get("..."))` | No |
| Spring Boot autoconfiguration | `@SpringBootApplication` classpath scanning | No |

**Honest detectability: ~3–7 / ~25 dep-loading calls = 12–28%**

#### Per-Project Summary

| Project | Reflection Profile | AST Detection Value |
|---------|-------------------|---------------------|
| spring-petclinic | Zero direct reflection; all Spring DI | None |
| apollo | Near-zero; Spring `@Autowired` | None |
| mybatis-3 | XML-config-driven `Class.forName(var)` | None |
| gson | JDK internal probing, self-introspection | None |
| **okhttp** | **String-literal `Class.forName` for optional TLS providers** | **Sole win** |
| feign | JAXP factories, own API internals | None |

### Approaches Considered

| Approach | Coverage | Cost | Maintainability |
|----------|----------|------|-----------------|
| A. tree-sitter reflection AST detection | 12–28% of dep-loading reflection | High (new query patterns, Kotlin support needed for okhttp) | Medium |
| B. `ScopeRuntime` whitelist (#300) | Known categories (JDBC, logging, WebJars, etc.) | Low | Low (list additions only) |
| C. A + B combined | Marginal improvement over B alone | High | High |
| D. SPI `META-INF/services` scanning | 0% in our sample (no SPI files found) | Medium | Low |
| E. CycloneDX `scope` field (#303) | `optional` (provided) and `excluded` (test) deps only | Low | Low |

## Decision

**Adopt approach B (`ScopeRuntime` whitelist) as the primary strategy. Do not implement tree-sitter reflection detection at this time. Approach E (CycloneDX `scope` field) is a candidate for future supplementation (see Amendment below).**

### Rationale

1. **Low ROI for AST detection**: Only 3–7 out of ~25 dep-loading reflection calls across 6 projects are string-literal `Class.forName` — the sole pattern tree-sitter can reliably detect. The investment in new query patterns, cross-language support (Kotlin for okhttp), and edge-case handling is not justified.

2. **Spring Boot is the dominant case and is fundamentally undetectable**: The most common source of false-positive UNUSED flags on Java dependencies is Spring Boot autoconfiguration. No static analysis — not even full type resolution — can detect classpath-scanned dependencies. The whitelist approach directly addresses this by recognizing known runtime dependency categories.

3. **Whitelist is extensible and cheap**: Adding a new Maven coordinate to the `ScopeRuntime` list is a one-line change. The diet-fuzz testing pipeline continuously discovers new false positives, making whitelist expansion data-driven.

4. **The "optional dependency probe" pattern (okhttp) is niche**: While technically detectable, this pattern appears in library internals probing for optional TLS providers. End-user applications rarely use this pattern directly — they depend on okhttp, which depends on bouncycastle. Transitive dependency analysis handles this better than reflection detection.

### Amendment: CycloneDX Scope Investigation (#303)

Investigation into using the CycloneDX `scope` field for runtime detection revealed a critical limitation: the CycloneDX spec defines only three scope values (`required`, `optional`, `excluded`), and **both Maven compile and runtime scopes map to `required`**. CycloneDX scope therefore cannot distinguish runtime from compile dependencies.

However, the `scope` field could provide value for two other categories:

| CycloneDX scope | Maven scope | Potential action |
|-----------------|-------------|------------------|
| `required` | compile, runtime | No action (default). Runtime detection uses `mavenRuntimeDeps` whitelist. |
| `optional` | provided | Annotate as provided. These deps are supplied by the runtime environment (e.g., `javax.servlet-api`, `lombok`). Unlike runtime deps, they typically DO have source imports. |
| `excluded` | test | Filter out from diet plan. Test deps should not appear in production dependency analysis. |

**Tool support**: Only cdxgen and CycloneDX Maven Plugin populate the `scope` field. Trivy and syft do not.

**Current status**: Not yet implemented. The practical value is limited because Trivy (the most common tool for production analysis) already excludes test deps by default and does not populate the scope field. Implementation is deferred until concrete demand arises. See #303 for the full design.

## Consequences

### Positive

- #300's `ScopeRuntime` whitelist ships immediately with low risk
- No new tree-sitter query complexity for Java/Kotlin reflection patterns
- diet-fuzz pipeline drives whitelist expansion empirically
- CycloneDX scope investigation (#303) documented limitations and future options

### Negative

- Unknown runtime dependencies not in the whitelist will still be flagged as UNUSED
- CycloneDX scope is only available from cdxgen and CycloneDX Maven Plugin; Trivy/syft users get no scope benefit
- If a future ecosystem heavily uses string-literal `Class.forName` for dep loading, this decision should be revisited

### Future Reconsideration Triggers

- A diet-fuzz batch reveals >20% false positives from non-whitelisted reflection-loaded deps
- A new language ecosystem (e.g., Clojure, Scala) shows high `Class.forName("literal")` usage for dep loading
- SPI (`META-INF/services`) becomes a significant source of false positives in surveyed projects
- SBOM tools begin embedding the original Maven scope in CycloneDX `properties` (e.g., `cdx:maven:scope`), enabling runtime-vs-compile distinction without the whitelist

## References

- #300: Recognize Java reflection-loaded deps as runtime-scoped
- #303: Use CycloneDX scope field for optional/excluded detection
- #248: JDBC drivers flagged as unused (parent issue)
- #288: Framework dispatch detection (Spring Boot autoconfiguration)
- ADR-0014: diet command architecture (tree-sitter design constraints)
