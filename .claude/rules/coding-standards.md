<!-- Generated from .github/instructions/coding-standards.instructions.md — DO NOT EDIT DIRECTLY -->

# Coding Standards

## Clean Code Principles

- **YAGNI (You Aren't Gonna Need It)**: Do not implement functionality until it is actually needed.
- **DRY (Don't Repeat Yourself)**: Avoid code duplication through proper abstraction and modularization.
- **Single Responsibility Principle**: Each function, struct, and package should have one clear purpose.
- **Delete Unused Code**: Always remove unused variables, functions, structs, and other dead code when making changes. This includes cleaning up imports that are no longer needed.

## Function Organization and Ordering

- **Public Functions First**: Place all exported (public) functions at the top of the file, followed by internal (unexported) functions below.
- **Logical Grouping**: Within public and internal sections, group related functions together.
- **Constructor Pattern**: If present, place `New...` constructor functions immediately after type definitions.

## Abstraction and Interface Guidelines

- **Value-Driven Abstraction**: Only create abstractions when they provide clear value. Avoid over-engineering with unnecessary abstractions.
- **Interface Creation Rules**: Create interfaces only when:
  - Multiple implementations exist or are planned
  - Dependency inversion is genuinely required for testability or architectural reasons
  - Polymorphic behavior is actually needed
- **Pragmatic Design**: Prefer concrete types over interfaces unless abstraction serves a specific, valuable purpose.

## Leverage the Zero Value

A key tenet of idiomatic Go is to make the zero value of a type useful.

- **Design for a Useful Zero Value**: Strive to design structs where the zero value is a valid and ready-to-use default. This can often eliminate the need for `New...` constructors.
- **Negative Naming for `bool` Flags**: If a feature should be enabled by default, name the flag with a negative sentiment (e.g., `DisableXxx` instead of `EnableXxx`), so its zero value (`false`) corresponds to the desired default behavior.

## Struct and Field Management

**Critical Rule: Define only what you use, delete what you don't use.**

- Regularly audit and remove unused structs, fields, functions, and other dead code.
- When refactoring or modifying code, always clean up any variables, functions, or imports that become unused as a result of the changes.

## Formatting and Linting

- **Formatter**: All Go code MUST be formatted with `goimports`. This is not negotiable.
- **Linter**: Code should adhere to the rules defined in our project's `golangci-lint` configuration.

## Naming Conventions

- **Package Names**: Short, concise, all lowercase. No `_` or `mixedCaps`.
- **Interface Names**: Single-method interfaces are often named by the method name plus an `-er` suffix (e.g., `Reader`).
- **Acronyms**: Keep acronyms in the same case (e.g., `ServeHTTP`, `userID`, `APIClient`).

## Documentation Comments

- **Exported Identifiers**: All exported functions, types, constants, and variables MUST have a `godoc` comment.
- **Godoc Format**: A comment for `MyFunction` should start with `// MyFunction ...`.

## API Design and Backward Compatibility

Any exported function, type, or constant is part of our public API. API stability is crucial.

- **Additive Changes are Preferred**: When modifying an exported struct, prefer adding new, optional fields over changing or removing existing ones.
- **Use the Options Pattern for Arguments**: Avoid adding new arguments to an existing exported function. Instead, use the "functional options pattern" for optional arguments to ensure backward compatibility.
- **Interfaces are (Almost) Forever**: Be very deliberate when designing exported interfaces, as adding methods to them is a breaking change.

## Learned from Copilot Reviews

- **Diff Content Filtering**: When writing tools that analyze `git diff` output, always strip diff metadata lines (`+++`, `---`, `diff --git`, `@@`) before pattern-matching on `^+` lines. Diff headers can trigger false positives.
- **Comment-Code Consistency**: When changing implementation behavior (e.g., switching from three-dot to two-dot diff), update all comments and documentation that reference the old behavior in the same commit. Also verify that function/struct comments accurately describe the actual heuristic or mechanism — do not mention capabilities (e.g., detecting `"true:"`) that the code does not implement. Comments that overstate behavior create false confidence in coverage.
- **Documentation Command Accuracy**: When adding or updating shell commands in documentation (README, CONTRIBUTING, etc.), verify they work by checking the actual project structure. Use `go build -o <binary> .` (package target) instead of `go build -o <binary> main.go` (single file) for multi-file packages. Ensure version references match `go.mod` and CI configuration.
- **Markdown Link Validity**: When adding or editing Markdown files under `.github/` (templates, workflows, docs), use absolute paths from the repo root (e.g., `/docs/development.md`) for links to repo files, since relative paths resolve from the file's directory. Always verify that linked files actually exist before committing.
- **Nullable Field Documentation**: When documenting a pointer or optional field, enumerate **all** conditions under which it can be nil/empty — not just the primary case. For example, a `ForkSource` field should note it is empty when `IsFork` is false **and** when the parent is private/inaccessible. Similarly, ensure the comment names the correct upstream API field (e.g., `parent` vs `source`) that the implementation actually uses.
- **Defensive Coding — Validate Early, Fail Clearly**: When a constructor or factory function receives a required dependency (e.g., a service, client, or parser), validate it is non-nil and return a descriptive error rather than allowing a nil-pointer panic later. Similarly, when CLI flags are mutually exclusive, reject the invalid combination at the validation layer with a clear message instead of silently preferring one. When a data field is a collection (slice/array), emit all items in serialized output rather than silently taking only the first. When sniffing file formats, validate field **values** (not just key presence) — e.g., check `bomFormat == "CycloneDX"`, not just that `bomFormat` exists.
- **File Type Detection — Use Exact Basename, Not Suffix**: When detecting file types by name (e.g., `go.mod`), use `filepath.Base(path) == "go.mod"` instead of `strings.HasSuffix(path, ".mod")`. Suffix matching can misclassify unrelated files (e.g., `deps.mod`) and route them to the wrong parser. Similarly, when matching path segments (e.g., `.github/workflows/`), require a leading path separator (`/.github/workflows/`) to avoid false positives from paths where the segment is embedded (e.g., `/tmp/not.github/workflows/`).
- **Reject Flags That Silently Have No Effect**: When a CLI flag only applies to a specific input mode (e.g., `--sample` for PURL list files), explicitly reject it with a clear error when the input is a different mode (e.g., go.mod or SBOM). Do not silently ignore the flag — users assume their flags take effect.
- **Deduplicate Inputs Before Batch API Calls**: When accepting user-provided input lists (PURLs, URLs) that feed into batch API calls, deduplicate them while preserving first-seen order before processing. Duplicates cause redundant external calls, skew logging/counts, and waste resources.
- **Normalize User-Provided Enum Values**: When accepting string values for format selectors, mode switches, or other enums from CLI flags, normalize with `strings.TrimSpace(strings.ToLower(...))` before validation. Case-sensitive matching rejects common inputs like `--format JSON` or `--format "json "`.
- **GitHub Actions `||` Treats Empty as Falsy**: When a workflow input documents "empty = X behavior", do not use `${{ inputs.foo || 'default' }}` — the `||` operator treats empty string as falsy and applies the default, preventing users from intentionally selecting the empty option. Instead, pass the raw input via an env var and apply defaults conditionally (e.g., only for scheduled triggers).
- **Guard Downstream Jobs Against Missing Outputs**: When a CI job produces outputs that downstream jobs depend on (exit codes, flags), gate downstream jobs on `needs.<job>.outputs.<key> != ''` to prevent execution when the upstream job fails before setting outputs. Otherwise, empty values may be misinterpreted (e.g., empty exit code `""` compared with `!= "0"` evaluates to true, creating misleading reports).
- **CI Job Gating — Key on Outputs, Not Job Result**: When a CI job intentionally exits non-zero for a primary use case (e.g., policy violations), downstream jobs must not gate on `needs.<job>.result == 'success'`. Use explicit output variables (exit codes, flags) to control downstream behavior, so jobs run in the scenarios they are designed for.
- **Remove Dead Configuration Inputs**: When a configuration surface (CLI flag, workflow input, env var) is no longer honored by the implementation (e.g., hardcoded internally), remove it entirely rather than leaving a misleading interface. A visible input that silently does nothing is worse than no input at all.
- **CI Permissions Documentation — Verify Inheritance**: When documenting GitHub Actions job permissions, verify each job's actual `permissions:` block in the workflow file. Jobs without an explicit `permissions:` key inherit the workflow-level permissions — do not describe them as having "no permissions" or "no extra permissions". State what each job actually has, including inherited defaults.
- **Lazy I/O During Format Detection**: When probing a file's format, prefer path-based checks first, then read only a small prefix for content-based heuristics. Read the full file only after confirming the format to avoid wasted I/O on non-matching files (e.g., reading an entire docker-compose.yml just to check if it's a GitHub Actions workflow).
- **Deterministic Output from Non-Deterministic Sources**: When building ordered output from non-deterministic sources (Go map iteration, goroutine-collected results, API directory listings), sort the data before further processing. This applies to rendered text, BFS seed queues, and any "first-seen wins" algorithm where input order determines provenance.
- **Post-Filter Fuzzy Search Results**: When using search APIs that perform fuzzy or word-level matching (e.g., GitHub issue search), add a post-filter to verify exact matches before acting on results. Fuzzy matches can cause false-positive deduplication or incorrect state transitions.
- **Consolidate Detection Heuristics — Single Source of Truth**: When a detection heuristic (file type sniffing, format detection, path matching) is used in multiple locations, centralize it in the responsible package and have callers delegate. Duplicating the heuristic across layers (e.g., `cmd/` and `infrastructure/`) risks drift when one copy is updated but the other is not.
- **Use Correct GitHub API Media Types**: When calling GitHub REST APIs, use the documented `Accept` header for the desired response format. For raw file content use `application/vnd.github.raw` (not `application/vnd.github.raw+json`). Incorrect media types may cause silent content-negotiation failures or unexpected response formats. Refer to GitHub's REST API media type documentation before adding a new endpoint call.
- **Narrow Typed Error Matching to Specific Conditions**: When checking typed errors (e.g., `IsResourceNotFoundError`), verify the error's message or context matches the expected source — not just the error type. A single error type can be returned by multiple code paths with different semantics (e.g., "repo not found" vs "no package managers"), and a broad type check can trigger incorrect fallback behavior for unrelated error origins.
- **ADR and Documentation Must Describe Actual Behavior**: When writing ADRs or design documents alongside implementation, verify that documented output formats, UI behavior, and feature descriptions match what the code actually produces. Do not document aspirational behavior (e.g., "source is embedded in per-entry headers") when the implementation has known limitations (e.g., only the summary table shows source). Document the current state accurately and note planned improvements separately.
- **Consistent Conditional Columns Across Output Formats**: When a column or field is conditionally shown in one output format (e.g., table omits RELATION when all entries are Unknown), apply the same conditional logic to all other formats (CSV, JSON). Unconditionally including a column in one format while conditionally hiding it in another creates inconsistent API surfaces and confuses downstream consumers.
- **Nil vs Empty Map Semantics for Sentinel-Checked Maps**: When a function returns a map that callers check for `nil` as a sentinel (e.g., "no data available" vs "data resolved but empty"), return `nil` when the resolved set is empty rather than an empty non-nil map. An empty non-nil map can cause callers to misinterpret "no results found" as "all items excluded", leading to incorrect classification or silent data loss.
- **Normalize Repo-Scoped Paths with `path.Clean`**: When accepting user- or YAML-supplied paths that are scoped within a repository (e.g., local action `./` references), normalize with `path.Clean` (not `filepath.Clean`) and reject results that equal `"."` or start with `".."`. Also reject backslashes. This prevents traversal beyond the repository root via the Contents API without blocking valid intra-repo `..` segments (e.g., `./foo/../bar` → `bar`).
- **Accurate Error Map Keys**: When recording errors in a `map[string]error` keyed by file path, use the actual resolved path — not a hardcoded filename. If a fetch tries `action.yml` then falls back to `action.yaml`, the error key must reflect which file was attempted, or use the parent path without a filename assumption.
- **Explicit Fallback for Unknown Enum Values**: When mapping external values (API responses, YAML fields) to internal enums or display strings, map unrecognized values to an explicit fallback (e.g., `"unknown(X)"`) rather than silently defaulting to a valid enum member. Silent defaults hide data quality issues and make debugging harder.
- **Machine-Readable Columns Must Contain Single Values**: When adding columns to machine-readable output (CSV, JSON), each column must contain exactly one data type — do not combine a label and a number in a single field (e.g., `"HIGH (7.5)"`). Split compound values into separate columns (e.g., `max_advisory_severity` + `max_cvss3_score`). Mixed-format cells break downstream parsing and sorting.
- **Use Domain Constants for Domain-Defined String Values**: When display or mapping logic switches on string values that are defined as domain constants (e.g., `LicenseSource*`), reference the constants — not duplicated raw strings. Duplicating values causes silent drift when constants are renamed or new values are added.
- **Branch Output Display on Each Field's Own Availability**: When rendering output fields (CLI text, CSV, JSON), branch display logic on each field's own availability — do not couple display of one field to the presence of an unrelated field. Ensure all output formats use the same data-source fallback chain as domain logic. Use host-agnostic labels (e.g., `Repository:` not `GitHub:`) unless the host is confirmed, and render all populated data fields rather than silently dropping them.
- **Use `utf8.RuneCountInString` for Terminal Display Widths**: When computing string widths for terminal display (box drawing, alignment), use `utf8.RuneCountInString` — not `len` — to avoid incorrect sizing with multi-byte characters (box-drawing glyphs, emoji). Clamp computed padding to zero when content already exceeds the budget rather than forcing a minimum that widens output beyond the declared width.
- **Filter and Normalize IDs Before Batch API Calls**: When building batch API requests from collected IDs, filter empty/whitespace values and deduplicate before processing to prevent invalid HTTP requests and cache pollution. Use `select` on `ctx.Done()` alongside channel operations in batch goroutines to avoid blocking after context cancellation.
- **Guard Nil Structs Consistently Across Output Formats**: When a struct field may be nil (e.g., `ReleaseInfo`), apply the nil guard in every output renderer that accesses it (text, CSV, JSON). If one renderer has the guard and another does not, the unguarded path will panic on nil input.
- **Use Case-Insensitive Comparison for URL Components**: When comparing URL components (scheme, host), use case-insensitive comparison per RFC 3986 — schemes (`HTTP://`) and hosts (`GitHub.COM`) are case-insensitive. Normalize with `strings.ToLower` or `strings.EqualFold` before prefix checks or host matching to avoid double-prefixing or missed matches.
- **Structured Logging Conventions**: When adding `slog` calls: use DEBUG level for routine per-item telemetry (reserve INFO for exceptional events); use `snake_case` for event names (not spaces) for consistency and filterability; choose field key names that accurately describe the data across all call sites (e.g., `"ref"` not `"purl"` when the function handles both PURLs and URLs).
- **Match Validation Format Strings to Production Format Strings**: When a validation or check function mirrors a production function's output (e.g., marker validation vs. marker replacement), use the exact same format strings and delimiters. Mismatched formats allow invalid input to pass validation silently.
- **CI Steps Must Stage All Script Outputs**: When a CI step checks for changes and stages files after running a script, include all files the script can produce — not just the commonly changed subset. If the script's output file list is defined in a config (e.g., `commands.json`), derive the staging paths from that config or use a broad `git diff --quiet` check. Silently dropping outputs leads to dirty workspaces or missed commits.
- **CI Workflow Steps Must Use Dynamic Refs**: When a CI workflow step references a branch name (e.g., `--base main` in `gh pr create`), use the workflow's actual ref context (e.g., `${{ github.ref_name }}`) instead of hardcoding a branch name. Hardcoded refs produce unexpected behavior when the workflow is triggered from a non-default branch.
- **Output Column Header Must Match Rendered Data**: When rendering tabular or structured output, verify that each column header/label corresponds to the actual data field being printed — not a related but different field (e.g., printing `Name` under a "PURL" header). Review header-to-value correspondence in the same pass as adding columns.
- **Unique Map Keys for Multi-Value Sentinels**: When using sentinel keys in a map to track special-case entries (e.g., blank imports, dot imports), ensure each entry gets a unique key (e.g., sentinel prefix + distinguishing suffix like the import path). Shared sentinel keys cause later entries to silently overwrite earlier ones, losing data.
- **Use Framework-Provided Parsed Arguments for Subprocess Delegation**: When delegating to a subprocess from a CLI framework handler, use the framework's parsed argument accessors (e.g., `cmd.Args().Slice()`) instead of the process-global `os.Args`. Global args may not match the framework's routing and break when the CLI is invoked programmatically.
- **Classify from Raw Values Before Rounding**: When deriving a category or label from a computed numeric value (e.g., score → difficulty bucket), apply the classification logic to the raw value before any rounding. Rounding first can push boundary values into the wrong bucket.
- **Validate Generated Strings Against Target-Language Syntax**: When programmatically generating identifiers, import paths, or package names for a target language, validate each candidate against that language's syntax rules before emitting it. Validation must cover the full identifier grammar — not just invalid characters but also positional rules (e.g., Java identifiers cannot start with a digit) and compound structures (e.g., dot-separated package names must validate each segment independently). For example, Maven artifactIds often contain hyphens (`commons-lang3`) and groupIds can too (`commons-io`), which are invalid in Java package names — emitting them verbatim produces candidates that can never match real imports. Similarly, error hints and suggestions must use terminology appropriate to the detected language/ecosystem, not hardcode references to a single ecosystem (e.g., `go.mod`) when the tool supports multiple languages.
- **Collect All Matches in Collector Functions — No Early Return**: When a function iterates over children/items to collect all matching results (e.g., AST bindings, search hits), append each match to a slice and return the slice after the loop. Do not `return` on the first match — early return drops remaining items. This applies whenever the caller needs *all* matches, not just the first.
- **Normalize Map Keys Consistently Across Insert and Lookup**: When building a `map[string]T` with normalized keys (e.g., `strings.ToLower` at insertion), apply the same normalization at every lookup site. A mismatch causes silent lookup failures for inputs with non-canonical casing (e.g., mixed-case Python module names like `OpenSSL`). Audit all functions that query the map, not just the one you're currently editing.
- **Use Ecosystem-Neutral Language in Multi-Language Error Messages**: When a CLI tool supports multiple ecosystems, error hints and suggestions must not reference language-specific files (e.g., `go.mod`) unless the current context is confirmed to be that language. Generic messages like "dependency manifest not found" are safer than ecosystem-specific ones.
