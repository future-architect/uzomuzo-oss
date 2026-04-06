# ADR-0014: uzomuzo diet — Dependency Removability Analysis Architecture

## Status

Proposed

## Context

`uzomuzo scan` tells you **which dependencies are unhealthy** (EOL, Stalled, etc.), but not:
- How deeply each dependency is coupled to your code
- How many transitive dependencies it pulls in
- Whether it's actually used at all
- Which ones to tackle first for maximum impact

The [vuls-diet project](https://github.com/future-architect/vuls/pull/2476) demonstrated that manual dependency analysis + removal can achieve dramatic results (binary −68%, dependencies −59%), but the process was entirely manual. `uzomuzo diet` (#158) automates this by combining upstream health signals with local static analysis to produce a prioritized "diet plan."

### Design Constraints

1. **Multi-language from day one** — uzomuzo-oss targets multiple ecosystems, not just Go. A Go-only tool would create a misleading first impression
2. **Pure Go for uzomuzo core** — `uzomuzo-catalog` and `future-architect/backend` depend on `uzomuzo-oss/pkg/uzomuzo`. CGo must not propagate to these consumers
3. **Build speed** — Adding CGo to the main `uzomuzo` binary would slow builds from ~5s to ~60-90s. This is unacceptable for the core CLI
4. **Deterministic output** — diet should produce reproducible, machine-readable results suitable for CI. Non-deterministic tasks (e.g., replacement code suggestions) belong outside the tool

### Approaches Considered for Static Analysis

| Approach | Accuracy | Performance | Multi-language | Pure Go | Verdict |
|----------|----------|-------------|---------------|---------|---------|
| `go/packages` + `go/ast` | Excellent (type info) | Moderate | No (Go only) | Yes | Go-only, doesn't meet constraint 1 |
| `go/analysis` framework | Excellent | Moderate | No | Yes | Same as above, more ceremony |
| tree-sitter via CGo | Excellent (syntax-level) | Fast | Yes | No | Meets constraint 1, violates 3 for main binary |
| tree-sitter via WASM (wazero) | Good | Slow (2-5x) | Yes | Yes | No mature pure-Go binding exists; requires custom WASM glue, memory management |
| Per-language regex parsers | Poor (edge cases) | Fastest | Yes | Yes | False positives from comments, multi-line imports, conditional imports |
| `gopls` / LSP | Excellent | Very slow (batch) | No | Yes | Not suitable for batch analysis |

### tree-sitter Precision for Diet Use Case

The diet command needs: (1) which files import each dependency, (2) how many call sites reference it, (3) API surface breadth. tree-sitter lacks type resolution (unlike `go/packages`), but the precision gap is negligible for removability scoring:

| Case | Frequency | Impact on ranking |
|------|-----------|-------------------|
| Variable shadowing (false positive) | Extremely rare | Safe side — overestimates coupling |
| Dot imports (false negative) | Extremely rare, discouraged in Go | Minimal |
| Implicit interface satisfaction | Medium | Not visible in imports — actually means "easy to decouple" |
| Type alias indirection | Rare | Alias definition itself is detected |

Conclusion: tree-sitter's syntax-level analysis is sufficient for diet's priority ranking. The rare edge cases either don't change the ranking or err on the safe side (overestimating coupling).

### Approaches Considered for Dependency Graph

| Approach | Invocations (391 deps) | Accuracy | Multi-language |
|----------|----------------------|----------|---------------|
| SBOM (CycloneDX) input | 0 (file parse) | Excellent | Yes — ecosystem-agnostic |
| `go mod graph` | 1 (< 1s) | Excellent (MVS) | No (Go only) |
| `go mod why -m` per dep | 391 (3-13 min) | Excellent | No |
| deps.dev API | N+1 calls | Poor (Replace/MVS unaware) | Yes |

## Decision

### Separate Binary with CLI Delegation

diet ships as a **separate binary** (`uzomuzo-diet`) with CGo + tree-sitter. The main `uzomuzo` binary delegates to it transparently via the git-style external subcommand pattern:

```
$ uzomuzo diet --sbom bom.json    # user runs this
  → uzomuzo finds uzomuzo-diet on PATH
  → exec uzomuzo-diet --sbom bom.json
```

```
uzomuzo-oss/
  cmd/
    uzomuzo/          ← existing CLI (Pure Go, fast build)
    uzomuzo-diet/     ← separate binary (CGo + tree-sitter)
  internal/
    infrastructure/
      treesitter/     ← CGo code lives here (internal/, invisible to library consumers)
```

**Why this works:**
- `uzomuzo` stays Pure Go — no build time impact, no CGo propagation to catalog/backend
- `uzomuzo-diet` is the only binary that links tree-sitter
- `internal/` prevents external consumers from importing tree-sitter packages
- Users see a unified `uzomuzo diet` UX
- Both binaries share domain logic via `internal/`
- goreleaser publishes both binaries; Homebrew formula installs both

### SBOM Input for Dependency Graph

diet takes a CycloneDX (or SPDX) SBOM as input rather than parsing ecosystem-specific lock files.

```
[User responsibility]  syft / trivy / cdxgen → generate SBOM
[diet responsibility]  SBOM → graph analysis + tree-sitter coupling → Diet Plan
```

**Why:**
- Ecosystem-agnostic — one input format for all languages
- CycloneDX `dependencies` field provides the full dependency tree with direct/transitive distinction
- No need to build and maintain lock file parsers for each ecosystem (package-lock.json, poetry.lock, Cargo.lock, pom.xml, etc.)
- uzomuzo already has a CycloneDX parser in `internal/infrastructure/depparser/cyclonedx/`
- Monorepo support becomes the user's problem (generate one SBOM per sub-project)

### tree-sitter for Multi-Language Static Analysis

All languages use tree-sitter for coupling analysis via CGo (`smacker/go-tree-sitter` or `tree-sitter/go-tree-sitter`). No per-language special casing.

**v0.1 languages:** Go, Python, JavaScript/TypeScript, Java

**v0.2 languages:** Rust, C#, others

Each language requires one tree-sitter grammar + one query file defining import patterns and selector expressions. Adding a new language is a grammar + query, not a new parser.

### Four-Phase Analysis Pipeline

```
Phase 1: Dependency Graph (SBOM parse, in-memory)
  SBOM → DAG → exclusive transitive dependency count per direct dep

Phase 2: Static Analysis (tree-sitter, local source code)
  Source files → import extraction → call site counting
  → file count, call sites, API breadth per dependency

Phase 3: Health Signals (API, reuse existing uzomuzo infrastructure)
  AnalysisService.ProcessBatchPURLs() → EOL / Scorecard / Advisory

Phase 4: Scoring & Prioritization
  Impact = exclusive transitive deps × health risk × (1 / coupling effort)
  → Sort by priority → Diet Plan output (JSON / table / detailed)
```

### Replacement Suggestions Are Out of Scope

diet produces **deterministic, machine-readable output** (what to remove, why, difficulty). Replacement code suggestions are delegated to LLMs consuming diet's JSON output:

```
uzomuzo diet (deterministic)              LLM (non-deterministic)
┌─────────────────────────┐          ┌──────────────────────────┐
│ SBOM → dependency graph  │          │ diet JSON output          │
│ tree-sitter → coupling   │    →     │ + source code             │
│ health × coupling → rank │          │ → replacement suggestions │
│ output: JSON             │          │ → PR creation             │
└─────────────────────────┘          └──────────────────────────┘
```

This can be implemented as a Claude Code custom command that takes diet's output as input — an evolution of the existing `analyze-dep.md` command (#157).

### Monorepo: Out of Scope

With SBOM input, monorepo handling is the user's responsibility. Users generate one SBOM per sub-project and run `uzomuzo diet` per SBOM. A future `--sbom-dir` flag for batch processing can be added based on demand.

## Consequences

### Benefits

- **Multi-language from day one** — Go, Python, JS/TS, Java in v0.1. No "Go-only tool" first impression
- **Zero impact on existing consumers** — uzomuzo core, catalog, and backend remain Pure Go with fast builds
- **Unified UX** — `uzomuzo diet` works seamlessly via delegation; users don't need to know about the separate binary
- **Clean separation of concerns** — deterministic analysis (diet) vs. non-deterministic suggestions (LLM)
- **Existing infrastructure reuse** — AnalysisService, deps.dev client, output formatting all shared via `internal/`
- **No existing tool provides this** — removability scoring combining graph impact, coupling depth, and health signals is unique

### Trade-offs

- **Two binaries to distribute** — goreleaser and Homebrew formula must handle both. Users who install only `uzomuzo` get a helpful error message when running `uzomuzo diet`
- **CGo build complexity for uzomuzo-diet** — Cross-compilation requires `zig cc` or similar. CI cold builds take ~60-90s for diet binary
- **tree-sitter syntax-level analysis** — Rare edge cases (variable shadowing, dot imports) may produce slightly inaccurate coupling counts. Acceptable for priority ranking
- **SBOM generation is a prerequisite** — Users must generate an SBOM before running diet. This is an extra step but leverages mature tooling (syft, trivy, cdxgen)
