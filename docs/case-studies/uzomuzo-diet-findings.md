# uzomuzo-diet Findings

Accumulated insights from diet-trial and diet-fuzz runs.

## 2026-04-12 — diet-fuzz run (21 projects × trivy/syft/cdxgen, focus: under-tested languages & legacy)

### Selection Strategy

Targeted gaps from previous runs: Java (severely under-tested, only 5 prior projects), Python with lockfiles, Go legacy (pre-2018), and JS/TS legacy (pre-2018). 21 projects total across 5 languages.

### NEW Root Cause: Java Annotation Processing (#287)

- **Bean Validation annotations** (`@NotNull`, `@Size`, `@Min`, `@Max`) from `hibernate-validator` are applied to entity fields as annotations — the annotation **is** the usage, but no function call exists. Found in linlinjava/litemall (2 import files, 0 call sites). This pattern is extremely common across the Java ecosystem: JPA (`@Entity`, `@Column`), Spring (`@Autowired`, `@Service`), Lombok (`@Data`, `@Getter`), Jackson (`@JsonProperty`).

### NEW Root Cause: Java NIO Framework Interface Dispatch (#288)

- **Netty types** (`ByteBuf`, `HttpRequest`, `SslHandler`) in OpenFeign/feign are imported for type declarations and interface implementations. The framework dispatches via the channel pipeline — handlers are invoked by the Netty event loop, not by direct user code method calls. 3 Netty modules (netty-buffer, netty-codec-http, netty-handler), each with 1 import file and 0 call sites. This pattern applies to all inversion-of-control frameworks (Vert.x, Akka, gRPC stubs).

### Vue Template Component Usage (added to #262)

- **PrimeVue** and **@fortawesome/vue-fontawesome** in spring-boot-admin: Components registered in `<script>` and used in `<template>` blocks — same root cause as Angular template detection (#262). Confirmed across all 3 SBOM tools (trivy, syft, cdxgen).

### Icon Data Imports — Constant Usage (added to #278)

- **@fortawesome/free-*-svg-icons** (3 packages) in spring-boot-admin: Imported icon objects (`faGithub`, `faTwitter`) are passed as data to `library.add()` — no function call on the imported symbol itself. Same root cause as constant-only usage (#278).

### CSS-Only Imports (added to #261)

- **normalize.css** in litemall and **office-ui-fabric-core** in fluentui: Pure CSS packages with zero JavaScript API surface. `import 'normalize.css'` and SCSS `@import` are side-effect patterns (#261).

### Java SBOM Tool Observations

- **Trivy Maven resolution timeout**: 3/8 Java projects (apollo, flyway, eladmin) failed due to Maven repository network resolution. Environment-specific, not a diet bug.
- **Syft low dep counts for multi-module Java**: syft detected PURLs but diet analyzed very few (apollo=4, cas=1, eladmin=1). Syft's dependency graph for Java multi-module projects doesn't resolve well without a build.
- **Mixed-language projects dominate Java IBNC**: spring-boot-admin and conductor include Vue/React frontends. Most IBNC (45/49) was JavaScript deps from the frontend, not Java deps.

### Go Legacy: Zero Anomalies (confirmed)

- 4 legacy Go projects (nats-server 2012, grpc-gateway 2015, validator 2015, hey 2016) × 3 tools = 12 runs. **Zero IBNC, zero EOL-ZERO, zero HIGH-HARD**. Go remains the most stable language for diet analysis, even for legacy repos.

### Python SBOM Tooling Remains the Bottleneck

- **Syft**: Failed on all 4 Python projects (no dependency graph).
- **Trivy**: Only works with lockfiles/requirements.txt — succeeded only for django-silk.
- **cdxgen**: Detected components but classified **all as transitive** (0 direct deps). This makes diet unable to identify removable direct dependencies.

### Cross-Language: 0 Direct Deps Across All npm SBOM Tools

- All 5 JS/TS legacy projects report 0 direct dependencies from all 3 SBOM tools. The `is_direct` field is not populated for npm/yarn lockfile-based SBOMs by any tool. This is a known SBOM ecosystem limitation, not a diet bug.

## 2026-04-08 — diet-fuzz run (20 projects × syft, 4 languages)

### SBOM Tool Limitations

- **syft cannot resolve Python projects using only `pyproject.toml`**: Projects without `requirements.txt`, `Pipfile.lock`, or `poetry.lock` produce SBOMs with 0 dependency graph edges. Diet fails with `SBOM has no dependency graph`. Affected: psf/black, encode/httpx, pypa/pip (3/5 Python projects). Workaround: use trivy or cdxgen, or ensure lockfiles exist.

### Call Site Detection — Resolved Patterns

The following patterns were fixed by #238 and #239, confirmed by before/after comparison (TypeScript IBNC: 58 → 25, -57%):

- **Default import + constructor** (`import Foo from 'foo'; new Foo()`) — fixed by #239. Evidence: dockerode in drizzle-orm (31 files, was 0 calls → now detected).
- **JSX component usage** (`<Component />`) — fixed by #238. Evidence: @heroicons/react (5 files), react-icons (4 files) in trpc now detected.
- **Named/destructured imports** — fixed by #239. Evidence: @opentelemetry/context-async-hooks (15 files) in prisma now detected.

### Call Site Detection — Remaining Known Limitations

These IMPORTS-BUT-NO-CALLS patterns are **expected behavior**, not bugs. They should be excluded from anomaly counts or documented as known limitations:

- **Config-driven tools** (14 cases): eslint plugins (`eslint-plugin-markdown`, `eslint-plugin-unicorn`, etc.), tailwindcss plugins, babel plugins. These are referenced in config files (`.eslintrc`, `tailwind.config.js`), not imported in source code.
- **Side-effect imports** (6 cases): `@testing-library/jest-dom`, `jest-extended`, `isomorphic-fetch`, `jest-serializer-ansi-escapes`. The `import 'foo'` pattern augments globals; there are no explicit API calls to detect.
- **CLI tools used as binaries** (8 cases): `wrangler` (×4 versions), `jest`, `ava`, `zx`, `esbuild-register`. These are invoked as CLI commands, not imported in application code.
- **Python conditional/try-except imports** (1 case): `cryptography` in flask — `try: import cryptography` used for feature detection. The import itself is the usage. Filed as #243.

### Cross-Language Observations

- **Go is the most stable language for diet analysis**: 0 anomalies across 5 projects (cli/cli, fzf, cobra, bubbletea, lazygit). go.mod-based resolution is mature and reliable.
- **TypeScript monorepos produce the most IBNC**: Average 5 IBNC per project vs 0 for Go. Monorepo patterns (re-exports, workspace dependencies, multi-package imports) create more edge cases for static analysis.
- **JavaScript projects have fewer direct deps in syft SBOMs**: express (1 direct), axios (1 direct). syft's CycloneDX output treats the root package as the only "direct" dependency when the lockfile doesn't distinguish dev vs prod well.
- **EOL/Archived deps are most common in TypeScript/JavaScript**: 9 EOL deps across 5 TS projects, 3 across 5 JS projects, vs 1 for Go and 0 for Python. The npm ecosystem's rapid package churn and frequent deprecations contribute.

### Tool Comparison Notes

- **syft vs trivy component counts differ**: syft finds more GitHub Actions components. For the same project, syft may report more total components but similar dependency graph depth.
- **syft includes devDependencies by default**: Unlike trivy which excludes them. This means syft SBOMs have more components and potentially more IBNC (dev-only deps like test utilities are included).

## 2026-04-08 — diet-fuzz run (20 projects × trivy/syft/cdxgen, 4 languages)

### SBOM Tool Findings (3-tool comparison)

- **pnpm monorepos produce 0 PURLs across ALL tools**: supabase, assistant-ui, medplum (all pnpm workspaces without root-level lock files) produced 0 PURL components from trivy, syft, and cdxgen. Monorepo SBOM generation requires per-workspace scanning or a resolved root lock file.
- **cdxgen produces dramatically richer JS SBOMs**: For TryGhost/Ghost, cdxgen found 4,585 deps vs trivy's 226 and syft's 225. cdxgen resolves full workspace transitive dependencies; trivy and syft surface only root-level or direct deps.
- **syft underperforms on some JavaScript projects**: mirotalksfu (3 deps vs trivy's 43), wekan (2 deps vs 40), openmct (no dependency graph). syft's JS scanner misses deps when the lockfile structure is non-standard.
- **Go SBOM tools don't mark `is_direct`**: All 5 Go projects × all 3 tools had `direct_deps=0`. The `is_direct` field is not populated for Go modules by any tool.
- **Direct dep count disagreement is massive**: dagster: trivy=326, syft=77, cdxgen=94. Ghost: trivy=226, syft=225, cdxgen=4,585. Tool choice significantly impacts diet analysis.

### Call Site Detection — New Patterns

- **Ember ecosystem (JavaScript)**: Ember's dependency injection (`extends GlimmerComponent`), decorators (`@classic`), and Ember Data container patterns are not detected as call sites. Ghost alone produced 80+ IBNC cases from this. The `extends` keyword and decorator syntax are not covered by the current tree-sitter call site queries.
- **Go cgo/driver imports**: `github.com/mattn/go-sqlite3` is imported for side effects (`import _ "..."`) and used indirectly through `database/sql`. Similar to the side-effect import pattern in JS but with Go's blank import syntax. Filed as #258.
- **Go SDK wrapper patterns**: `github.com/1password/onepassword-sdk-go` and `github.com/aws/smithy-go` are called through higher-level SDK wrappers, not directly. The call site is in the wrapper package, not the user's code.
- **Cross-language dep contamination in Python monorepos**: dagster and gptme are Python projects with JS frontends. SBOM tools (especially trivy) include JS workspace deps (`remark-gfm`, `rehype-highlight`, etc.) which the Python coupling analyzer cannot trace — producing false IBNC.
- **Python plugin/framework patterns**: `pytest-databases`, `pytest-mock` (pytest plugins), `click-default-group` (click plugin), `typing-extensions` (TYPE_CHECKING conditional) — all invoked through framework mechanisms, not standard function calls.
- **`metascraper-*` plugin array pattern**: 7 metascraper plugins in Ghost are registered via config array (`metascraper([author(), description(), ...])`) — the plugin function calls are inside the array, not standalone.

### Updated Cross-Language Observations

- **Go now shows IBNC**: Unlike the previous run (0 anomalies), this run found 9 IBNC in 2/5 Go projects. Expanding to different projects (ollama, vals) revealed cgo/driver and SDK wrapper patterns not seen in CLI-focused projects.
- **JavaScript IBNC is dominated by Ember/framework patterns**: 159 real IBNC, overwhelmingly from Ghost's Ember patterns. Framework-specific DI/decorator detection would address ~50% of JS IBNC.
- **Tool choice creates 10-20× dep count variance**: The same project analyzed with different tools produces wildly different component counts, affecting IBNC counts proportionally.

## 2026-04-09 — diet-fuzz round 2 (20 projects × trivy/syft/cdxgen, targeted selection)

### Selection Strategy

Round 2 targeted gaps from round 1: TypeScript non-monorepo projects (3/5 skipped in round 1), non-Ember JavaScript frameworks, pure Python projects (no JS frontends), and Go cgo/plugin-heavy projects.

### TypeScript: Major Data Recovery (5/5 success vs 2/5 in round 1)

- All 5 TypeScript projects (zod, nest, date-fns, typeorm, formik) produced valid results across all 3 SBOM tools (15/15). Selecting projects with root-level lockfiles was the key.
- **typeorm database driver pattern**: `@google-cloud/spanner` (score 0.691), `mssql`, `mysql2`, `better-sqlite3`, `pg-native` — all loaded via conditional `require()` at runtime based on user configuration. This is the TypeScript equivalent of Go's `import _ "driver"` pattern. 16 IBNC via syft.
- **`reflect-metadata` side-effect import**: 794 import files in typeorm, 8 in nest. `import 'reflect-metadata'` patches the global `Reflect` API for decorator support. Structurally identical to Go's blank import — zero callable API surface.
- **NestJS adapter/transport pattern**: `@fastify/static`, `@fastify/cors`, `amqplib`, `mqtt`, `ioredis` — loaded conditionally based on chosen HTTP adapter or microservice transport. 16 IBNC via cdxgen.

### JavaScript: Ember Hypothesis Confirmed

- Round 1 JS IBNC: 159 (dominated by Ghost/Ember). Round 2 JS IBNC: 11 (no Ember). **93% reduction** confirms Ember DI/decorator patterns were the primary driver.
- **Koa delegated composition**: `http-assert` (score 0.624), `koa-compose`, `mime-types` are called through koa's context object (`this.assert()` → `http-assert`). The call site is inside koa's source, not user code.
- **`extends` pattern**: `@socket.io/component-emitter` used via `extends Emitter` (7 files) — class inheritance not detected as call site.
- **2/5 JS projects skipped** (fastify, hapi) — no lockfiles. Lockfile absence is a cross-language SBOM tool limitation.

### Go: Vault Confirms Database Driver Pattern

- hashicorp/vault (plugin-heavy): 14 IBNC across tools. `go-mssqldb` and `go-hdb` are database drivers registered via blank import — same pattern as `go-sqlite3` in round 1.
- **Cross-language contamination in vault**: Vault's Ember UI produces JS deps in Go SBOMs. `@glimmer/tracking` (243 files), `@glimmer/component` (311 files), `ember-data` — false IBNC from Go SBOM including JS workspace.
- kubectl, gorm, pq, fiber: all clean (0 IBNC, 0 EOL). Interface-heavy and framework projects without plugin/driver patterns are well-handled.

### Python: Optional Dependency Pattern Confirmed

- 3 IBNC total: `cryptography` (flask), `email-validator` and `python-multipart` (fastapi). All use `try: import X / except ImportError` pattern for optional dependencies.
- **2/5 skipped** (httpie/cli, django-rest-framework): no lockfiles. Pure `pyproject.toml` projects remain unresolvable by all 3 SBOM tools.

### Cross-Run Pattern Taxonomy

Accumulated across both rounds, IBNC patterns fall into clear categories:

| Pattern | Languages | Actionable? | Suggested fix |
|---------|-----------|-------------|---------------|
| Side-effect/blank import (`import _`, `import 'x'`) | Go, TS, JS | Yes | Detect `import _ "pkg"` and `import 'pkg'` as usage |
| Database driver conditional loading | Go, TS | Maybe | Detect `require()` inside try-catch |
| Config-driven plugins (remark, eslint, postcss) | TS, JS | No | Known limitation — config files, not imports |
| Framework DI/decorator (`@Entity`, `extends`) | TS, JS (Ember, Nest) | Yes | Detect decorator usage and `extends` as call sites |
| Delegated composition (koa context, SDK wrappers) | JS, Go | No | Call site is in library, not user code |
| Python optional imports (`try/except ImportError`) | Python | Yes | Detect import inside try block as conditional usage |
| Cross-language SBOM contamination | Go, Python | No | SBOM tool limitation — mixed-language repos |
| Type-only imports (`csstype`, `typing-extensions`) | TS, Python | Maybe | Detect TYPE_CHECKING blocks |

## 2026-04-12 — diet-fuzz run (18 projects × trivy/syft, 4 languages)

### Selection Strategy

Round 4 targeted under-tested patterns: Java generics/functional API (validate #283 fix), Go plugin/backend systems (validate #281 fix), Python auto-discovery/C-extension projects, JS/TS constructor/chained-command patterns. All 18 projects were previously untested and excluded from a parallel session running 21 other projects.

### NEW Root Cause: Java Generic Type Arguments Not Captured (#286)

- **Pattern**: `extends Foo<Bar>` — the #283 fix captures `Foo` (outer type) but NOT `Bar` (type argument). When `Foo` is from the local package and `Bar` is from an external package, the external dependency usage is missed.
- **Evidence**: netty/netty — `protobuf-java` has 4 import files, 0 call sites. `ProtobufEncoder extends MessageToMessageEncoder<MessageLiteOrBuilder>` — `MessageLiteOrBuilder` from protobuf-java is not captured.
- **Additional missing patterns in Java**: `instanceof`, type cast, class literal, method reference, type bounds — none are captured by current call site queries.
- **Impact**: Any Java project using an external library primarily for type definitions (common with protobuf, JPA entities, serialization frameworks) will show false IBNC.
- Filed as **#286**.

### Go Type-Only Package Usage — Evidence for #278

- **rclone `go-proton-api`** (1 import file, 0 call sites): Package used extensively for types (`proton.Link`, `proton.Auth`, `proton.User`) and constants (`proton.LinkStateActive`) but zero function/method calls. Same file's `proton-api-bridge` import correctly shows 9 call sites, proving the analyzer works for that file.
- This confirms #278 is not limited to constant-only packages — **type-only packages** with zero callable API surface are equally affected.

### SBOM Tool Findings

- **syft v1.42.4 produces no dependency graph for ANY Python project** (4/4 failed). syft found components (54 for django, 57 for matplotlib) but zero dependency graph edges. Only trivy produced usable Python SBOMs.
- **trivy misparses django dependencies**: Detected 6 deps, all test eggs (`brokenapp`, `commandegg`, etc.) from `tests/setup.cfg` — not the real runtime deps (`asgiref`, `sqlparse`). Root `setup.cfg` was ignored.
- **matplotlib SBOM uses `pkg:conda/*` PURLs**: Dependencies from `environment.yml` are conda packages, not PyPI. Coupling analysis cannot match imports to conda PURLs.
- **Go SBOM discrepancy remains large**: trivy finds 4-12× more deps than syft across all Go projects (moby: 170 vs 25, traefik: 295 vs 59, etcd: 231 vs 24).

### Cross-Run Pattern Updates

| Pattern | Languages | New evidence |
|---------|-----------|-------------|
| Generic type argument (`<Bar>` in `extends Foo<Bar>`) | Java | **NEW** — netty protobuf-java (#286) |
| Type-only / constant-only packages | Go | rclone go-proton-api (#278) |
| Go blank imports | Go | moby go-md2man, rclone x/mobile, etcd dev tools (#258) |
| CommonJS chained/inline require | JS | pm2 debug (17 files), mkdirp, source-map-support (#241) |
| CommonJS destructured require | JS | pm2 eventemitter2 (#274) |
| Angular template components | Java (cross-lang), TS | flink @angular/forms (17 files), cypress @angular/common (#262) |
