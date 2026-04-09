# uzomuzo-diet Findings

Accumulated insights from diet-trial and diet-fuzz runs.

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

## 2026-04-09 — diet-fuzz round 3 (5 TypeScript projects, targeted deep-dive)

### Selection Strategy

TypeScript-only round targeting decorator DI (Angular), ORM conditional drivers (Prisma, Drizzle), RPC frameworks (tRPC), and AI SDK (Vercel AI). All 5 projects succeeded (15/15 tool combos).

### Angular: Karma Plugins and HTML Template Components

- **Karma test runner plugins** (7 packages, 45+ files): `karma-chrome-launcher`, `karma-jasmine`, `karma-coverage`, etc. — loaded via `karma.conf.js`, not source imports. Same config-driven pattern as eslint/remark plugins but in the test runner ecosystem.
- **HTML template component pattern** (NEW): `ngx-progressbar`, `angular-split` are Angular components used via HTML template selectors (`<ngx-progressbar>`) and NgModule declarations. The "call site" is in `.html` templates, which tree-sitter does not scan for TypeScript import usage. This is a fundamentally different pattern from all others — the usage is in a different file type.
- **`zone.js` side-effect import**: 7 files. Angular's async tracking library, imported as `import 'zone.js'` — patches global async APIs. Additional evidence for #261.
- **`@angular/aria`**: 111 import files, 0 call sites. Used through Angular's dependency injection — the module is imported and registered, but API calls go through DI-resolved instances.

### Prisma: Generator and Script Dependencies

- `@prisma/generator` (24 files): Internal package used through Prisma's code generation pipeline, not direct function calls.
- `next`, `@octokit/rest`, `esbuild-register`: Example app and CI script dependencies — not part of the Prisma library itself. Monorepo SBOM includes workspace dev dependencies.

### Drizzle-ORM: Minimal IBNC

- Only 3 IBNC (syft): `zx` (dev script runner), `@typescript-eslint/experimental-utils` (ESLint tooling), `ws` (conditional WebSocket driver). Clean result — Drizzle's direct import patterns are well-handled.

### tRPC and Vercel AI

- tRPC: 13 IBNC (trivy). Mix of remark plugins (docs site), `event-source-polyfill` (side-effect), `server-only` (Next.js build guard), `@fastify/websocket` (conditional transport).
- Vercel AI: 6 IBNC (trivy). `@vercel/kv` (SDK), `json-schema` (indirect usage), `react-server-dom-webpack` (framework internal), cross-language deps (`pydantic`, `@angular/common` from examples).

### Updated Pattern Taxonomy (3 rounds combined)

| Pattern | Languages | Evidence | Actionable? | Issue |
|---------|-----------|----------|-------------|-------|
| Side-effect import (`import 'x'`, `import _ "x"`) | Go, TS, JS | 805+ files (reflect-metadata alone) | **HIGH** | #261 |
| DB driver conditional loading | Go, TS | typeorm, vault, drizzle | **HIGH** | #260 |
| Config-driven plugins (karma, eslint, remark, postcss) | TS, JS | 20+ packages | No — config files | #245 |
| Framework DI/decorator | TS, JS | Angular, Nest, Ember | **MEDIUM** | #245 |
| HTML template components | TS (Angular) | ngx-progressbar, angular-split | **NEW** — cross-file-type | #245 |
| Delegated composition | JS, Go | koa, SDK wrappers | No — library-internal | #241 |
| Python optional imports | Python | flask, fastapi | **MEDIUM** | #243 |
| Cross-language SBOM contamination | Go, Python, TS | vault, dagster, vercel/ai | No — SBOM tool issue | — |
| Type-only imports | TS, Python | csstype, typing-extensions, @cloudflare/workers-types | **LOW** | — |
| Example/script deps in monorepo | TS | prisma, trpc | No — SBOM scope issue | — |
