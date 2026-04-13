# IBNC Pattern Taxonomy

[← Back to README.md](../README.md)

**IBNC** (Imports-But-No-Calls) describes dependencies that appear unused by static call-site analysis but are actually required at runtime. Removing them breaks the build or causes runtime failures.

This document is referenced by `/diet-remove` (Phase 1 step 4). It is maintained from empirical findings across 79+ OSS projects in 5 languages (Go, Java, TypeScript, JavaScript, Python) from diet-fuzz campaigns (April 2026).

## Quick Reference Checklist

Before classifying a dependency as "unused" or "trivial to remove," verify it is not one of these patterns:

- [ ] **Side-effect import** -- `import _ "pkg"` (Go), `import 'pkg'` (JS/TS), `require('pkg')` without assignment
- [ ] **Database / driver registration** -- blank import or conditional `require()` for DB drivers
- [ ] **Config-driven plugin** -- referenced in config files (eslint, tailwind, babel, postcss), not source imports
- [ ] **Framework DI / decorator** -- `@Entity`, `@Autowired`, `extends Framework`, Ember DI
- [ ] **Annotation-only usage** -- Java annotations (`@NotNull`, `@JsonProperty`) where the annotation *is* the usage
- [ ] **Type-only / constant-only package** -- imported for types or constants, zero function calls
- [ ] **Delegated composition** -- called indirectly through SDK wrappers or framework context objects

## Patterns in Detail

### 1. Side-Effect Import

**Languages**: Go, TypeScript, JavaScript

The import statement itself *is* the usage. The module's `init()` function (Go) or top-level code (JS/TS) registers drivers, patches globals, or augments prototypes.

| Language | Syntax | Example |
|----------|--------|---------|
| Go | `import _ "github.com/lib/pq"` | Registers PostgreSQL driver with `database/sql` |
| TypeScript | `import 'reflect-metadata'` | Patches global `Reflect` API for decorator support (794 import files in typeorm) |
| JavaScript | `require('isomorphic-fetch')` | Patches global `fetch` |
| CSS-only | `import 'normalize.css'` | Pure CSS package with zero JS API surface |

**Detection**: Look for `import _ "..."` (Go), `import 'pkg'` without destructuring (JS/TS), or packages with no exported API.

**Related issues**: [#258](https://github.com/future-architect/uzomuzo-oss/issues/258) (Go cgo/driver blank imports), [#261](https://github.com/future-architect/uzomuzo-oss/issues/261) (CSS-only imports)

### 2. Database / Driver Registration

**Languages**: Go, TypeScript, Java

A specialization of Pattern 1: database drivers are loaded at runtime via registration mechanisms, not direct function calls from user code. Listed separately because the consequences of removal (broken DB connections) are more severe and harder to diagnose than general side-effect imports.

| Language | Pattern | Example |
|----------|---------|---------|
| Go | `import _ "github.com/mattn/go-sqlite3"` | Registers via `sql.Register()` in `init()` |
| TypeScript | `require('pg')` inside try-catch | typeorm loads drivers conditionally based on user config |
| Java | `Class.forName("com.mysql.cj.jdbc.Driver")` | JDBC driver registration via reflection |

**Evidence**: hashicorp/vault (go-mssqldb, go-hdb), typeorm (@google-cloud/spanner, mssql, mysql2, better-sqlite3).

**Related issues**: [#258](https://github.com/future-architect/uzomuzo-oss/issues/258)

### 3. Config-Driven Plugin

**Languages**: TypeScript, JavaScript

The dependency is referenced in a configuration file (JSON, JS, YAML), not in source code imports. Static import analysis will never find it.

| Config file | Plugin type | Example |
|-------------|------------|---------|
| `.eslintrc` / `eslint.config.js` | ESLint plugin | `eslint-plugin-unicorn`, `eslint-plugin-markdown` |
| `tailwind.config.js` | Tailwind plugin | `@tailwindcss/typography` |
| `babel.config.js` | Babel plugin | `@babel/plugin-transform-runtime` |
| `postcss.config.js` | PostCSS plugin | `autoprefixer` |
| `jest.config.js` | Jest preset/transform | `ts-jest`, `esbuild-register` |

**Detection**: Search config files (`*.config.js`, `.*rc`, `*.config.ts`) in addition to source imports.

**Note**: CLI-only tools (`wrangler`, `jest`, `ava`, `zx`) also fall in this category -- invoked as binaries, not imported.

### 4. Framework DI / Decorator

**Languages**: TypeScript, JavaScript, Java

Frameworks use inversion-of-control patterns (dependency injection, decorators, class inheritance) to wire components. The framework invokes user code, not the other way around.

| Framework | Pattern | Example |
|-----------|---------|---------|
| Ember | `extends GlimmerComponent`, `@classic` decorator | Ghost produced 80+ IBNC from Ember DI alone |
| NestJS | `@Injectable()`, `@Module()` | Adapter/transport packages loaded conditionally |
| Angular | `<app-component>` in templates | Template component usage invisible to import analysis |
| Vue | `<PrimeButton>` in `<template>` blocks | Component registered in `<script>`, used in `<template>` |
| Spring | `@Autowired`, `@Service`, classpath scanning | Auto-configuration loads beans via reflection |
| Netty | `extends ChannelInboundHandlerAdapter` | Handlers invoked by event loop, not user code |

**Evidence**: TryGhost/Ghost (Ember, 80+ IBNC), NestJS (16 IBNC via cdxgen), OpenFeign/feign (Netty, 3 modules), spring-petclinic (caffeine, mysql-connector-j via auto-config).

**Related issues**: [#262](https://github.com/future-architect/uzomuzo-oss/issues/262) (Angular/Vue template detection), [#288](https://github.com/future-architect/uzomuzo-oss/issues/288) (Framework interface dispatch), [#295](https://github.com/future-architect/uzomuzo-oss/issues/295) (Spring Boot starter false-sharing)

### 5. Annotation-Only Usage

**Languages**: Java

Java annotations are applied to classes, fields, and methods as metadata. The annotation processor or framework reads them at compile time or runtime. There is no function call to detect.

| Annotation source | Examples | Used by |
|-------------------|----------|---------|
| Bean Validation (Hibernate Validator) | `@NotNull`, `@Size`, `@Min`, `@Max` | Entity field validation |
| JPA (Jakarta Persistence) | `@Entity`, `@Column`, `@Table` | ORM mapping |
| Lombok | `@Data`, `@Getter`, `@Builder` | Compile-time code generation |
| Jackson | `@JsonProperty`, `@JsonIgnore` | JSON serialization |

**Evidence**: linlinjava/litemall (hibernate-validator: 2 import files, 0 call sites).

**Related issues**: [#287](https://github.com/future-architect/uzomuzo-oss/issues/287)

### 6. Type-Only / Constant-Only Package

**Languages**: Go, Java, TypeScript

The package is imported solely for type definitions or constants. No functions or methods are called.

| Language | Pattern | Example |
|----------|---------|---------|
| Go | `proton.Link`, `proton.Auth` (types), `proton.LinkStateActive` (constant) | rclone go-proton-api: 1 import file, 0 call sites |
| Java | `extends Foo<Bar>` where `Bar` is from external package | netty protobuf-java: `MessageLiteOrBuilder` used as type argument |
| TypeScript | `import type { Foo } from 'pkg'` | Type-only imports (often stripped at compile time) |

**Evidence**: rclone (go-proton-api), netty (protobuf-java via generic type arguments).

**Related issues**: [#278](https://github.com/future-architect/uzomuzo-oss/issues/278) (Type-only / constant-only packages), [#286](https://github.com/future-architect/uzomuzo-oss/issues/286) (Java generic type arguments)

### 7. Delegated Composition

**Languages**: JavaScript, Go

The dependency is called indirectly through a higher-level wrapper, framework context, or SDK layer. The call site exists in the wrapper's source, not in user code.

| Pattern | Example |
|---------|---------|
| Koa context delegation | `http-assert` called via `this.assert()` inside koa |
| SDK wrapper | `1password/onepassword-sdk-go` called through higher-level SDK |
| Plugin array registration | `metascraper([author(), description(), ...])` -- 7 plugins in Ghost |
| Class inheritance | `@socket.io/component-emitter` via `extends Emitter` |

**Evidence**: koajs/koa (http-assert via context delegation), TryGhost/Ghost (7 metascraper plugins via array registration), socket.io (component-emitter via class inheritance).

**Detection**: Check if the dependency is a transitive dependency of another direct dependency that wraps it.

## Cross-Cutting Concerns

### SBOM Tool Variance

The same project analyzed with different SBOM tools (trivy, syft, cdxgen) can produce **10-20x variance** in dependency counts. This directly affects which dependencies appear in the analysis and how many IBNC patterns surface.

| Tool | Strength | Weakness |
|------|----------|----------|
| trivy | Reliable Go/npm resolution | Misparses some Python projects, excludes devDependencies |
| syft | Includes devDependencies, finds GitHub Actions | No Python dependency graph, low Java multi-module counts |
| cdxgen | Richest JS SBOMs (4,585 vs 226 for Ghost) | Monorepo root detection issues (Gradle) |

### Cross-Language SBOM Contamination

Mixed-language repositories (Go + Ember UI, Python + React frontend) produce SBOMs that include dependencies from the other language. These cross-language deps appear as IBNC because the coupling analyzer for one language cannot trace imports in another.

**Evidence**: hashicorp/vault (Ember UI deps in Go SBOM), dagster (JS frontend deps in Python SBOM), spring-boot-admin (Vue frontend deps in Java SBOM).

## Maintenance

This document is updated when diet-fuzz campaigns discover new IBNC categories. Each pattern entry must include:
- At least one real-world evidence project
- A link to the tracking issue (if one exists)
- The affected languages
