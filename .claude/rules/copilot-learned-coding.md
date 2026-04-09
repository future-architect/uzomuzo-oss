<!-- Generated from .github/instructions/copilot-learned-coding.instructions.md — DO NOT EDIT DIRECTLY -->

# Coding Standards — Learned from Copilot Reviews

Rules extracted from recurring Copilot review patterns on coding-standards topics (naming, defensive coding, API consistency, CI workflows, output formatting, etc.).

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
- **Extract Shared Helpers for Near-Duplicate Code Paths**: When two functions follow the same sequence (e.g., parse input → call external API → interpret result → populate output) differing only in how one parameter is obtained, extract the shared sequence into a single helper parameterized on that value. Near-duplicate paths drift silently when logging, error handling, or evidence formatting is updated in one copy but not the other.
- **Narrow Candidate Heuristics and Map Assertions to Specific Items**: When generating candidate values (import paths, match keys) from heuristics, validate each candidate against its target domain to avoid false-positive attribution from overly broad matching. Similarly, when asserting on `map[K]V` results, check the specific key under test (`paths := m[key]; len(paths) == 0`) — not the whole map (`len(m) == 0`), which only confirms any key has data without verifying the key you care about.
- **Narrow Heuristic Candidate Sets to Avoid False Attribution**: When building candidate lists for matching (e.g., import-path heuristics, file-type detection), prefer precise patterns over broad substring matching. An overly broad heuristic (e.g., taking only the last segment after a delimiter) can collide with unrelated entries and cause false attribution (e.g., marking an unrelated dependency as "used"). Add validation or specificity constraints to each candidate before insertion.
- **Verify Tree-Sitter Query Patterns Do Not Overlap**: When adding new tree-sitter (or similar AST) query patterns to a multi-pattern query, verify that the new pattern does not match nodes already captured by an existing pattern via parent-child nesting. For example, a standalone `member_expression` pattern already matches the inner `pkg.Foo` node inside `new pkg.Foo()`, so adding a `new_expression` wrapping `member_expression` pattern would double-count the same call site. Test with representative code that exercises both the new and existing patterns.

## Pending Copilot Patterns

<!--
Cross-PR pattern accumulator for /review Phase 3.
New entries are inserted at random positions to avoid merge conflicts.
When a category reaches 2+ entries across different PRs, it is promoted
to a rule in the section above, and the promoted entries are removed.

Schema (YAML-in-Markdown):
  - category: "<pattern-category>"
    summary: "<one-line description>"
    pr: <PR number>
    file: "<path>"
    date: "YYYY-MM-DD"
-->

```yaml
pending_patterns:
  - category: "testing"
    summary: "Propagate build tags (e.g., //go:build cgo) to test files that import build-tag-constrained packages — otherwise CGO_ENABLED=0 builds fail"
    pr: 237
    file: "internal/application/diet/coupling_integration_test.go"
    date: "2026-04-08"
  - category: "testing"
    summary: "Capture and assert arguments passed to test fakes/mocks — unconditional return values let tests pass even when PURL parsing/decoding is wrong"
    pr: 236
    file: "internal/infrastructure/eolevaluator/evaluator_npm_test.go"
    date: "2026-04-08"
  - category: "comment-doc-drift"
    summary: "Doc comments must not make absolute claims about external system behavior — scope claims to what the current function actually handles"
    pr: 253
    file: "internal/infrastructure/treesitter/analyzer.go"
    date: "2026-04-09"
  - category: "testing"
    summary: "Accept interface types in test-setter methods (e.g., SetXxxClient) so fakes can be injected via public API instead of unexported fields"
    pr: 236
    file: "internal/infrastructure/eolevaluator/evaluator.go"
    date: "2026-04-08"
  - category: "testing"
    summary: "When a function can return nil to signal 'unavailable' vs empty to signal 'no matches', ensure tests include at least one matching item so the result is non-nil and the test exercises actual behavior — not a vacuous early return"
    pr: 237
    file: "internal/application/diet/coupling_integration_test.go"
    date: "2026-04-08"
  - category: "whitespace-agnostic-matching"
    summary: "Use bytes.Fields tokenization instead of fixed-separator prefix checks when matching directives — tabs and multiple spaces are valid separators"
    pr: 140
    file: "internal/infrastructure/depparser/detect.go"
    date: "2026-04-05"
  - category: "testing"
    summary: "Test fixture PURLs must match the import package prefix they map to — mismatched PURL/import pairs make tests confusing and hide incorrect mappings"
    pr: 235
    file: "internal/infrastructure/treesitter/analyzer_test.go"
    date: "2026-04-08"
  - category: "api-consistency"
    summary: "Remove omitempty from boolean and always-present slice JSON tags — omitempty makes absent-vs-false/empty ambiguous for downstream schema consumers"
    pr: 223
    file: "internal/interfaces/cli/diet_render.go"
    date: "2026-04-07"
```

<!-- Promotion history (kept for audit trail):
  # error-handling: promoted to error-handling.instructions.md (PRs #87, #159 — surface initialization errors instead of silent degradation)
  # defensive-coding: already covered by promoted rules (PR #159, analyzer.go:382 — filter tree-sitter captures by name to skip non-import captures)
  # deterministic-output: already covered by promoted rule (PR #159, analyzer.go:315 — sort ImportFiles for deterministic JSON output)
  # comment-doc-drift: already covered by promoted rule (PR #159, diet.go:18 — comment documented CLI default but code has different semantics for programmatic callers)
  # defensive-coding: promoted to coding-standards.instructions.md (PRs #127, #130 — match validation format strings, CI staging completeness, dynamic refs)
  # duplicate-parsing: promoted to copilot-learned-coding.instructions.md (PRs #111, #236 — extract shared helpers for near-duplicate code paths)
  # naming-consistency: already covered by "Use Domain Constants for Domain-Defined String Values" (PR #119, boxdraw.go — domain constants vs raw strings)
  # comment-doc-drift: already covered by promoted rule (PR #121, aggregates.go — enum precedence comments used old labels)
  # comment-doc-drift: already covered by promoted rule (PR #121, types.go — struct field comment referenced old label names)
  # testing (PR #121): promoted — see testing-performance.instructions.md "Use `filepath.Join` for Temp File Paths"
  # comment-doc-drift: already covered by promoted rule in coding-standards (PR #127, main.go:203 — replaceBlock comment claimed duplicate marker detection but only checked begin markers)
  # defensive-coding: already covered by promoted rules (PR #127, main.go:214 — dynamic Markdown fence delimiter to avoid backtick collisions)
  # defensive-coding: refined existing rule "Use Case-Insensitive Comparison for URL Components" (PR #119, boxdraw.go:680)
  # defensive-coding: already covered by "Branch Output Display on Each Field's Own Availability" (PR #119, boxdraw.go:314, boxdraw.go:403, boxdraw.go:606, boxdraw.go:741)
  # defensive-coding: guard loop/slice budgets against non-positive values to prevent panic/infinite loop (PR #119, boxdraw.go:163)
  # api-consistency: promoted to coding-standards.instructions.md (PRs #101, #107, #115, #116)
  # defensive-coding: promoted to coding-standards.instructions.md (PRs #101, #103, #106, #107, #111, #115, #116)
  # testing: promoted to testing-performance.instructions.md (PRs #101, #103, #106, #107, #115, #116, #119)
  # comment-doc-drift: promoted to coding-standards.instructions.md (PRs #101, #106, #111, #116)
  # deterministic-output: promoted to coding-standards.instructions.md (PRs #106, #111)
  # naming-consistency: promoted to coding-standards.instructions.md (PRs #87, #116, #119 — context-sensitive labels for mixed input types)
  # logging-consistency: promoted to coding-standards.instructions.md (PRs #116, #119)
  # defensive-coding: already covered by "Consolidate Detection Heuristics — Single Source of Truth" (PR #119, batch.go:724 — use common.IsValidGitHubURL instead of partial scheme-prefix check)
  # comment-doc-drift: already covered by promoted rule in coding-standards (PR #119, boxdraw.go:88 — writeLine comment overstated URL-exclusion heuristic)
  # defensive-coding: already covered by budget guard rule (PR #119, boxdraw.go:171 — preserve unbroken tokens instead of force-splitting mid-token)
  # api-consistency: already covered by "Branch Output Display on Each Field's Own Availability" (PR #123, boxdraw.go:634 — deps.dev link coupled to direct advisory presence instead of any-advisory presence)
  # api-consistency: already covered by "Consistent Conditional Columns Across Output Formats" (PR #123, scan_render.go:433 — blank vs "0" for zero-count numeric CSV columns)
  # comment-doc-drift: already covered by promoted rule (PR #123, helpers.go:165 — comment claimed UI URL in summary but implementation no longer includes it)
  # naming-consistency: already covered by promoted rule (PR #123, lifecycle_assessor.go:432,437 — "vulns" abbreviation inconsistent with "vulnerabilities" used elsewhere)
  # deterministic-output: already covered by promoted rule (PR #123, enrich_transitive_advisory.go:76 — nondeterministic map iteration for transitive advisory entries)
  # api-consistency: already covered by "Branch Output Display" spirit (PR #123, boxdraw.go:804 — header dep names derived from full list instead of displayed/truncated subset)
  # comment-doc-drift: already covered by promoted rule (PR #130, ci.yml:40 — checkout comment said "default ref" but workflow_dispatch uses user-selected ref)
  # comment-doc-drift: already covered by promoted rule (PR #140, detect.go:21 — comment claimed "always" for sniff window but long headers can push directive past 512 bytes)
  # testing: promoted to testing-performance.instructions.md (PRs #121, #140 — use filepath.Join for portable temp file paths in tests)
  # testing: promoted to testing-performance.instructions.md (PRs #143, #159 — bounded waits, no t.Fatal from goroutines, concurrent pipe reads)
  # defensive-coding (PR #236): already covered by "Guard Nil Structs Consistently Across Output Formats" and "Defensive Coding — Validate Early, Fail Clearly" — nil-check client dependency in all caller paths before delegating to shared helper that unconditionally dereferences it
  # defensive-coding (PR #236 round 4): typed-nil interface bypasses != nil guard — already covered by "Guard Nil Structs Consistently" and reflect-based nil check added in 7fd4564
  # defensive-coding (PR #236 round 5): reflect.ValueOf().IsNil() panics on non-nilable dynamic types — already covered by "Guard Nil Structs Consistently"; extracted isNilInterface helper with Kind check in ed944c5
  # duplicate-parsing (PR #236 round 5): double-parse of EffectivePURL in fallback path — already covered by promoted "Extract Shared Helpers for Near-Duplicate Code Paths" rule; refactored checkNpmDeprecation to accept pre-parsed ns/name/ver in ed944c5
  # logging-consistency (PR #236 round 4): already covered by promoted "Structured Logging Conventions" rule — align error log prefix with file-local convention ("eol: ..." prefix) instead of event-id style
  # logging-consistency (PR #236 round 6): already covered by promoted rule — include caller-identifier logEvent as structured field in both debug and error logs for disambiguation
  # testing (PR #236 round 6): already covered by "Cover New Control Flow Branches with Tests" — add regression test for typed-nil client guard (isNilInterface + checkNpmDeprecation early return)
  # naming-consistency: promoted to coding-standards.instructions.md (PRs #144, #159 — output column header must match rendered data field)
  # defensive-coding: promoted to coding-standards.instructions.md (PRs #144, #159 — unique map keys for sentinels, framework-provided parsed args, classify before rounding)
  # comment-doc-drift: already covered by "Comment-Code Consistency" rule (PR #144, build_health_assessor.go:56 — comment claimed SLSA provenance but implementation excludes SLSA signals)
  # comment-doc-drift: already covered by promoted rule (PR #144, build_health_assessor.go:25 — ScorecardCheck comment referenced SLSA/Attestation but those signals are excluded)
  # api-consistency: already covered by "Consistent Conditional Columns Across Output Formats" (PR #144, scan_render.go:501,560 — JSON/CSV per-entry omitted "Ungraded" while summary counted it)
  # comment-doc-drift: already covered by "Comment-Code Consistency" rule (PR #148, boxdraw.go:422 — function header comment didn't document VerdictReplace early return)
  # naming-consistency: already covered by "Comment-Code Consistency" spirit (PR #148, verdict_test.go:68 — test function name implied build integrity drives verdict after semantics changed to ignore it)
  # testing: already covered by "Cover New Control Flow Branches with Tests" (PR #148, scan_render.go:362 — missing test for buildIntegrityDisplay VerdictReplace branch)
  # testing: already covered by "Cover New Control Flow Branches with Tests" (PR #148, boxdraw.go:449 — missing tests for writeBoxBuildIntegrity header+icon format and replace-verdict hiding)
  # defensive-coding: already covered by promoted rules (PR #159, analyzer.go:284 — IsUnused boolean flag defined on wrong field; diet_render.go:181 — display message inconsistent with flag semantics)
  # defensive-coding: already covered by promoted rules (PR #159, service.go:275 — Maven import path heuristic used groupId.artifactId instead of groupId alone)
  # defensive-coding: already covered by promoted rules (PR #159, commands.go:279 — use framework-provided parsed args instead of os.Args)
  # error-handling: already covered by promoted rules (PR #159, commands.go:272 — wrap underlying LookPath error with %w)
  # defensive-coding: already covered by "Consolidate Detection Heuristics" (PR #159, diet.go:36 — use centralized createAnalysisService instead of direct constructor)
  # testing: already covered by promoted rules (PR #159, e2e_test.go:94 — check pipe read/close errors; graph_test.go — check json.Marshal errors)
  # defensive-coding: already covered by "Nil vs Empty Map Semantics for Sentinel-Checked Maps" (PR #159, sbomgraph/types.go:118 + depgraph/graph.go:33)
  # comment-doc-drift: already covered by promoted rule (PR #159, analyzer.go:431 — misleading "BUG" labels for intentional import handling)
  # defensive-coding: already covered by "Nil vs Empty Map Semantics" (PR #159, analyzer.go:336 — return nil coupling map when no source files analyzed, not empty map that misclassifies all deps as unused)
  # defensive-coding: already covered by promoted rules (PR #159, analyzer.go:434 — blank imports are side-effect usage; skipping them misclassifies deps as unused)
  # defensive-coding: already covered by promoted rules (PR #159, analyzer.go:447 — shared sentinel key for blank/dot imports overwrites earlier entries)
  # defensive-coding: already covered by promoted rules (PR #159, analyzer.go:82 — use tree-sitter #eq? predicates to constrain overly broad query patterns)
  # defensive-coding: already covered by "Deterministic Output from Non-Deterministic Sources" (PR #159, analyzer.go:420,513 — nondeterministic map iteration for prefix matching; use longest-match selection)
  # defensive-coding: already covered by promoted rules (PR #159, commands.go:285 — use urfcli.Exit instead of os.Exit for testability and cleanup)
  # defensive-coding: already covered by "Defensive Coding — Validate Early, Fail Clearly" (PR #159, diet.go:31 — validate required --sbom flag before file I/O)
  # testing: promoted to testing-performance.instructions.md (PRs #159, #160 — mirror production JSON tags in test structs; use t.Cleanup for global restoration)
  # defensive-coding: already covered by "Deterministic Output from Non-Deterministic Sources" (PR #159, analyzer.go:494 — Python prefix matching used first-match instead of longest-match)
  # defensive-coding: already covered by promoted rules (PR #159, analyzer.go:461 — Go alias derivation needs vN suffix and gopkg.in heuristics for accurate call-site counting)
  # defensive-coding: already covered by "Normalize User-Provided Enum Values" spirit (PR #159, service.go:288 — PyPI distribution names need hyphen→underscore and lowercase normalization for import matching)
  # defensive-coding: already covered by "Nil vs Empty Map Semantics" (PR #159, sbomgraph/types.go:122 — ResolveDirectPURLs returned nil for 0 direct deps, conflating with "no graph info")
  # defensive-coding: already covered by "Nil vs Empty Map Semantics" (PR #159, depgraph/graph.go:37 — AnalyzeGraph used == nil instead of len == 0, misaligned with ResolveDirectPURLs empty-slice semantics)
  # defensive-coding: promoted to coding-standards.instructions.md (PRs #160, #173, #175, #176 — validate generated strings against target-language syntax; ecosystem-agnostic error hints; collect all AST matches; consistent map key normalization)
  # defensive-coding (PR #176): skip Maven groupId.artifactId candidates when artifactId contains chars invalid in Java package names
  # defensive-coding (PR #176 round 2): reject identifier-first-digit (e.g. "3scale") and validate dot-separated namespace segments independently
  # logging-consistency: already covered by promoted rule (PR #175, service.go:72 — log must report post-filter count when subsequent phases use filtered data)
  # defensive-coding: already covered by "Delete Unused Code" rule (PR #173, analyzer.go:516 — unused cfg parameter in resolvePythonPURL)
  # defensive-coding: already covered by "Collect All Matches in Collector Functions" spirit (PR #173, analyzer.go:518 — import x as y alias not registered; must handle all AST node variants)
  # testing: already covered by promoted rules (PR #160, e2e_test.go:366 — close pipe fds in t.Cleanup; e2e_test.go:387 — check Close() errors for consistency)
  # testing: already covered by promoted rules (PR #160, e2e_test.go:100 — surface pipe close/read errors in shared test helpers for diagnosability)
  # testing: already covered by "Use `t.Cleanup` When Replacing Process-Global State" (PR #160, e2e_test.go:353 — close stdinW in t.Cleanup to prevent FD leak on early abort)
  # testing: already covered by promoted rules (PR #160, e2e_test.go:380 — capture ReadFrom error via channel for consistency with runDiet)
  # defensive-coding (PR #176 round 3): already covered by "Normalize User-Provided Enum Values" spirit — case-insensitive override lookup key for Maven package overrides
  # defensive-coding (PR #176 round 3): already covered by "Validate Generated Strings Against Target-Language Syntax" — gate fallback artifactId candidate behind isJavaPackageSafe
  # defensive-coding (PR #176 round 4): already covered by "Use Case-Insensitive Comparison for URL Components" spirit — use strings.EqualFold for namespace/name equality in Maven candidate filtering
  # defensive-coding (PR #176 round 5): already covered by "Validate Generated Strings Against Target-Language Syntax" — validate namespace with isJavaDottedPackageSafe before emitting groupId.artifactId candidate
  # defensive-coding (PR #226): already covered by "Comment-Code Consistency" — clamp condition checked IsEOL but not MaintenanceStatus=="Archived", missing documented qualifier
  # defensive-coding: promoted to copilot-learned-coding.instructions.md (PRs #227, #237 — narrow candidate heuristics and map assertions to specific items)
  # naming-consistency (PR #198): already covered by "Use Domain Constants for Domain-Defined String Values" spirit — extract magic coefficients to named constants
  # comment-doc-drift (PR #198): already covered by "Comment-Code Consistency" rule — test case name didn't match domain mapping for healthRisk values
  # testing: promoted to testing-performance.instructions.md (PRs #197, #198, #223 — assert exact values not thresholds, omit unused test fixture fields, assert all new output fields)
  # testing: promoted to testing-performance.instructions.md (PRs #197, #198 — assert exact computed values; omit unused struct fields in test fixtures)
  # testing: promoted to testing-performance.instructions.md (PRs #197, #198 — assert exact values with tolerance; omit unused fixture fields)
  # defensive-coding (PR #199): already covered by "use tree-sitter predicates to constrain overly broad query patterns" — add !object negation to bare-call query to prevent double-counting
  # defensive-coding (PR #199): already covered by "Consolidate Detection Heuristics — Single Source of Truth" — handle wildcard static imports with sentinel alias consistent with Python
  # comment-doc-drift (PR #199): already covered by "Comment-Code Consistency" — clarify ImportFileCount vs import-statement count in test comment
  # defensive-coding (PR #200): already covered by "Deduplicate Inputs Before Batch API Calls" spirit — compact map[K][]V slices after append to prevent double-counting
  # comment-doc-drift (PR #200): already covered by promoted rule — blank-import comment said "multiple blank imports in one file" but key is per import path
  # testing (PR #200): already covered by promoted rules — Go collision test name implied unrealistic multi-version scenario; renamed to reflect actual behavior
  # comment-doc-drift (PR #223): already covered by "ADR and Documentation Must Describe Actual Behavior" — detailed output now renders a truncated symbols list (fixed in cf5ca6e)
  # testing (PR #223): promoted — assert all new output fields when extending structs (see testing-performance.instructions.md)
  # testing (PR #223 round 4): already covered by promoted rule — add IsUnused/HasWildcardImport assertions for blank-import case to lock in intended behavior
  # testing (PR #223 round 5): already covered by promoted rule — assert CallSiteCount baseline for dot-import case that documents "has baseline call sites"
  # comment-doc-drift (PR #230): already covered by "Comment-Code Consistency" rule — test name claimed constructor detection ("new FormData()") but call query only matches member_expression/call_expression, not new_expression
  # comment-doc-drift (PR #229): already covered by "Comment-Code Consistency" rule — goPackageFromHyphenated conditional logic skipped go- prefix stripping after -go suffix stripping, diverging from documented sequential heuristic
  # defensive-coding: promoted to copilot-learned-coding.instructions.md (PRs #227, #237 — narrow candidate heuristics and map assertions to specific items)
  # defensive-coding (PR #227): already covered by "Validate Generated Strings Against Target-Language Syntax" — add isPythonIdentifierSafe validation for PyPI import path candidates
  # defensive-coding (PR #227 round 2): already covered by "Validate Generated Strings Against Target-Language Syntax" — validate dotted module paths (e.g., zope.interface) by splitting on "." and checking each segment independently
  # comment-doc-drift (PR #227 round 2): WONT_FIX — PR description reflects initial implementation; code and tests were updated in prior commit
-->
