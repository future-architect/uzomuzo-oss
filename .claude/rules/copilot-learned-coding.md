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
- **Use Spec-Compliant Parsers for Standardized Formats**: When parsing standardized file formats (CSV, RECORD, TSV), use the language's spec-compliant parser (e.g., `encoding/csv`) instead of naive `strings.Split` or similar. Naive splitting misparses quoted or escaped fields, producing silent data corruption.
- **Reject Flags That Silently Have No Effect**: When a CLI flag only applies to a specific input mode (e.g., `--sample` for PURL list files), explicitly reject it with a clear error when the input is a different mode (e.g., go.mod or SBOM). Do not silently ignore the flag — users assume their flags take effect.
- **Deduplicate Inputs Before Batch API Calls**: When accepting user-provided input lists (PURLs, URLs) that feed into batch API calls, deduplicate them while preserving first-seen order before processing. Duplicates cause redundant external calls, skew logging/counts, and waste resources.
- **Normalize User-Provided Enum Values**: When accepting string values for format selectors, mode switches, or other enums from CLI flags, normalize with `strings.TrimSpace(strings.ToLower(...))` before validation. Case-sensitive matching rejects common inputs like `--format JSON` or `--format "json "`.
- **Normalize Config Values Once Before Guard and Use**: When a config-sourced value is validated (e.g., `strings.TrimSpace(v) != ""`) and then used, assign the normalized result to a variable and use that variable for both the guard and subsequent operations (`SetBaseURL`, logging). Checking `TrimSpace(v)` in the guard but passing the original `v` to the consumer silently passes whitespace-padded values, producing invalid URLs or config entries.
- **Doc Comments Must Match Type-Level Constraints**: When writing doc comments, ensure stated conditions are possible for the parameter types — do not document "nil" for non-pointer types (e.g., `string`, `int`), do not claim a knob controls APIs it does not actually affect, and do not name only a subset of the languages/contexts that use a shared constant. When test names or comment examples reference specific inputs, they must match the actual values under test. Comments that describe impossible states or overstate scope create false confidence and mislead future maintainers.
- **Match `net/url` Function to Semantic Context**: When manipulating URL components, select the `net/url` function that matches the component's semantics — `u.Hostname()` (not `u.Host`) when only the hostname is needed (avoids including the port), `url.PathUnescape`/`url.PathEscape` (not `url.QueryUnescape`/`url.QueryEscape`) for URL path segments (avoids `+`-as-space misinterpretation). Mismatched functions silently corrupt values containing reserved characters like `:`, `+`, or `@`.
- **Enforce Access Constraints on All CI Trigger Paths**: When a CI workflow guards against cross-repository or fork PRs on the `pull_request` trigger (e.g., `head.repo.full_name == github.repository`), enforce the same constraint in code paths reachable by other triggers (`schedule`, `workflow_dispatch`) that bypass the trigger-level guard. Unguarded paths can fire privileged operations (e.g., GraphQL mutations with a PAT) on fork PRs, causing auth failures or unintended side effects. Similarly, use generous page sizes (e.g., `first:100` instead of `first:20`) in paginated API verification queries to avoid false negatives that trigger unnecessary retries.
- **Interface Contract Documentation Must Match Signature Semantics**: When documenting an interface method, the doc comment must accurately reflect the method's full signature — including error returns, nil semantics, and parameter constraints. If the signature returns `(T, error)`, do not document it as "returns nil/empty on failure" — state that errors may be returned and describe the caller's expected handling (e.g., non-fatal/graceful degradation). Mismatched contract documentation misleads implementers and callers.
- **GitHub Actions `||` Treats Empty as Falsy**: When a workflow input documents "empty = X behavior", do not use `${{ inputs.foo || 'default' }}` — the `||` operator treats empty string as falsy and applies the default, preventing users from intentionally selecting the empty option. Instead, pass the raw input via an env var and apply defaults conditionally (e.g., only for scheduled triggers).
- **Guard Downstream Jobs Against Missing Outputs**: When a CI job produces outputs that downstream jobs depend on (exit codes, flags), gate downstream jobs on `needs.<job>.outputs.<key> != ''` to prevent execution when the upstream job fails before setting outputs. Otherwise, empty values may be misinterpreted (e.g., empty exit code `""` compared with `!= "0"` evaluates to true, creating misleading reports).
- **CI Job Gating — Key on Outputs, Not Job Result**: When a CI job intentionally exits non-zero for a primary use case (e.g., policy violations), downstream jobs must not gate on `needs.<job>.result == 'success'`. Use explicit output variables (exit codes, flags) to control downstream behavior, so jobs run in the scenarios they are designed for.
- **Remove Dead Configuration Inputs**: When a configuration surface (CLI flag, workflow input, env var) is no longer honored by the implementation (e.g., hardcoded internally), remove it entirely rather than leaving a misleading interface. A visible input that silently does nothing is worse than no input at all.
- **CI Permissions Documentation — Verify Inheritance**: When documenting GitHub Actions job permissions, verify each job's actual `permissions:` block in the workflow file. Jobs without an explicit `permissions:` key inherit the workflow-level permissions — do not describe them as having "no permissions" or "no extra permissions". State what each job actually has, including inherited defaults.
- **Lazy I/O During Format Detection**: When probing a file's format, prefer path-based checks first, then read only a small prefix for content-based heuristics. Read the full file only after confirming the format to avoid wasted I/O on non-matching files (e.g., reading an entire docker-compose.yml just to check if it's a GitHub Actions workflow).
- **Deterministic Output from Non-Deterministic Sources**: When building ordered output from non-deterministic sources (Go map iteration, goroutine-collected results, API directory listings), sort the data before further processing. This applies to rendered text, BFS seed queues, and any "first-seen wins" algorithm where input order determines provenance.
- **Post-Filter Fuzzy Search Results**: When using search APIs that perform fuzzy or word-level matching (e.g., GitHub issue search), add a post-filter to verify exact matches before acting on results. Fuzzy matches can cause false-positive deduplication or incorrect state transitions.
- **Rerun Analyzers with Combined Input Sets on Retry**: When retrying a subset of inputs through an analyzer that tracks collisions or shared matches, rerun with the combined input set (original + retry) to preserve attribution consistency. Subset reruns can misattribute shared matches to the wrong source.
- **Consolidate Detection Heuristics — Single Source of Truth**: When a detection heuristic (file type sniffing, format detection, path matching) is used in multiple locations, centralize it in the responsible package and have callers delegate. Duplicating the heuristic across layers (e.g., `cmd/` and `infrastructure/`) risks drift when one copy is updated but the other is not.
- **Use Correct GitHub API Media Types**: When calling GitHub REST APIs, use the documented `Accept` header for the desired response format. For raw file content use `application/vnd.github.raw` (not `application/vnd.github.raw+json`). Incorrect media types may cause silent content-negotiation failures or unexpected response formats. Refer to GitHub's REST API media type documentation before adding a new endpoint call.
- **Narrow Typed Error Matching to Specific Conditions**: When checking typed errors (e.g., `IsResourceNotFoundError`), verify the error's message or context matches the expected source — not just the error type. A single error type can be returned by multiple code paths with different semantics (e.g., "repo not found" vs "no package managers"), and a broad type check can trigger incorrect fallback behavior for unrelated error origins.
- **ADR and Documentation Must Describe Actual Behavior**: When writing ADRs or design documents alongside implementation, verify that documented output formats, UI behavior, and feature descriptions match what the code actually produces. Do not document aspirational behavior (e.g., "source is embedded in per-entry headers") when the implementation has known limitations (e.g., only the summary table shows source). Document the current state accurately and note planned improvements separately.
- **Consistent Conditional Columns Across Output Formats**: When a column or field is conditionally shown in one output format (e.g., table omits RELATION when all entries are Unknown), apply the same conditional logic to all other formats (CSV, JSON). Unconditionally including a column in one format while conditionally hiding it in another creates inconsistent API surfaces and confuses downstream consumers.
- **Nil vs Empty Map Semantics for Sentinel-Checked Maps**: When a function returns a map that callers check for `nil` as a sentinel (e.g., "no data available" vs "data resolved but empty"), return `nil` when the resolved set is empty rather than an empty non-nil map. An empty non-nil map can cause callers to misinterpret "no results found" as "all items excluded", leading to incorrect classification or silent data loss.
- **Normalize Repo-Scoped Paths with `path.Clean`**: When accepting user- or YAML-supplied paths that are scoped within a repository (e.g., local action `./` references), normalize with `path.Clean` (not `filepath.Clean`) and reject results that equal `"."` or start with `".."`. Also reject backslashes. This prevents traversal beyond the repository root via the Contents API without blocking valid intra-repo `..` segments (e.g., `./foo/../bar` → `bar`).
- **Preserve Original Input Through Heuristic Fallback Chains**: In chained heuristic pipelines where each step transforms an intermediate result, fallback on empty must return the original input — not the intermediate value from a prior step. Returning an intermediate value violates the documented contract and can produce silently incorrect results when later steps depend on the untransformed original.
- **Accurate Error Map Keys**: When recording errors in a `map[string]error` keyed by file path, use the actual resolved path — not a hardcoded filename. If a fetch tries `action.yml` then falls back to `action.yaml`, the error key must reflect which file was attempted, or use the parent path without a filename assumption.
- **Exported API Must Not Leak Unexported Types**: When an exported function or method returns (or accepts) an unexported type, it creates an API that other packages cannot use. Either export the type, unexport the function if all callers are package-internal, or use an exported interface/struct. Similarly, when a JSON struct tag uses `omitempty` on a boolean or always-present slice field, the serialized output becomes ambiguous (absent vs false/empty) for downstream consumers — omit `omitempty` for fields whose zero value is semantically meaningful.
- **Handle All Valid Input Forms in Format Parsers**: When parsing a structured format (ZIP entries, RECORD files, manifests), handle all valid representations defined by the spec — not just the common case. For example, Python wheel RECORD files contain both package directories (`pkg/__init__.py`) and root-level modules (`six.py`); skipping root-level entries silently drops valid import names for single-module packages.
- **Explicit Fallback for Unknown Enum Values**: When mapping external values (API responses, YAML fields) to internal enums or display strings, map unrecognized values to an explicit fallback (e.g., `"unknown(X)"`) rather than silently defaulting to a valid enum member. Silent defaults hide data quality issues and make debugging harder.
- **Enforce HTTP Client Hardening on All Code Paths**: When constructing an HTTP client with security hardening (redirect policies, SSRF guards, timeout caps), ensure the hardening applies uniformly — including on test-injected clients and across all status-code branches. Specifically: (1) when a constructor accepts an injected `*http.Client`, set missing security callbacks (e.g., `CheckRedirect`) to the hardened default rather than relying on callers to attach them manually; (2) classify retryable HTTP statuses (408 Request Timeout, 429 Too Many Requests) as transient alongside 5xx — do not negative-cache them as authoritative failures; (3) verify redirect-counting logic against `net/http`'s `via` slice semantics where `len(via)` counts prior requests, so `len(via) > maxRedirects` allows exactly N hops while `len(via) >= maxRedirects` allows only N-1.
- **Machine-Readable Columns Must Contain Single Values**: When adding columns to machine-readable output (CSV, JSON), each column must contain exactly one data type — do not combine a label and a number in a single field (e.g., `"HIGH (7.5)"`). Split compound values into separate columns (e.g., `max_advisory_severity` + `max_cvss3_score`). Mixed-format cells break downstream parsing and sorting.
- **Use Domain Constants for Domain-Defined String Values**: When display or mapping logic switches on string values that are defined as domain constants (e.g., `LicenseSource*`), reference the constants — not duplicated raw strings. Duplicating values causes silent drift when constants are renamed or new values are added.
- **Branch Output Display on Each Field's Own Availability**: When rendering output fields (CLI text, CSV, JSON), branch display logic on each field's own availability — do not couple display of one field to the presence of an unrelated field. Ensure all output formats use the same data-source fallback chain as domain logic. Use host-agnostic labels (e.g., `Repository:` not `GitHub:`) unless the host is confirmed, and render all populated data fields rather than silently dropping them.
- **Use `utf8.RuneCountInString` for Terminal Display Widths**: When computing string widths for terminal display (box drawing, alignment), use `utf8.RuneCountInString` — not `len` — to avoid incorrect sizing with multi-byte characters (box-drawing glyphs, emoji). Clamp computed padding to zero when content already exceeds the budget rather than forcing a minimum that widens output beyond the declared width.
- **Filter and Normalize IDs Before Batch API Calls**: When building batch API requests from collected IDs, filter empty/whitespace values and deduplicate before processing to prevent invalid HTTP requests and cache pollution. Use `select` on `ctx.Done()` alongside channel operations in batch goroutines to avoid blocking after context cancellation.
- **Guard Nil Structs Consistently Across Output Formats**: When a struct field may be nil (e.g., `ReleaseInfo`), apply the nil guard in every output renderer that accesses it (text, CSV, JSON). If one renderer has the guard and another does not, the unguarded path will panic on nil input.
- **Gate Fallback Logic on Error, Not Result Nilness**: When deciding whether to trigger fallback or retry logic, check the error value — not whether the result is nil. A nil result with nil error is a valid success case (e.g., zero matches found), and treating it as a failure triggers unnecessary retries or incorrect fallback paths.
- **Minimize Allocations in Hot Paths**: In batch-processing or frequently-called functions, avoid unnecessary O(n) allocations when only a subset of data is needed. Cache results of expensive parsing calls when the same value is checked multiple times in a loop iteration, and iterate to a known cutoff point rather than materializing the full collection (e.g., iterate runes up to a count rather than converting the entire string to `[]rune`).
- **Use Structured Parsers for Structured Identifier Properties**: When checking properties of structured identifiers (PURLs, URIs, import paths), use the appropriate parser rather than naive string operations (`strings.Contains`, `strings.Split`). For example, `strings.Contains(purl, "@")` misclassifies npm scoped packages like `pkg:npm/@scope/name` as versioned because `@` appears in the namespace. Use `packageurl.FromString(p).Version != ""` or an equivalent parser-based check.
- **Use `u.Hostname()` for Port-Safe Host Comparison**: When comparing URL hostnames, use `u.Hostname()` instead of `u.Host`. The `Host` field includes the port component (e.g., `github.com:443`), so direct string comparison against a bare hostname fails silently, misclassifying URLs and triggering unnecessary fallback processing. Similarly, when parsing multi-entry `go-import`/`go-source` meta tags, select the entry whose import prefix most specifically matches the requested path per the Go module spec — blindly taking the first match can resolve to the wrong repository on monorepo vanity pages.
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
- **Continue AST Ancestor Walks Past Non-Matching Nodes**: When walking AST ancestors to find a guarding condition (e.g., `if TYPE_CHECKING:` blocks), continue past intermediate nodes of the same type that don't match the target condition. Returning early on the first type match (e.g., the first `if_statement`) misses the actual guard when the import is nested inside inner conditionals.
- **Normalize Map Keys Consistently Across Insert and Lookup**: When building a `map[string]T` with normalized keys (e.g., `strings.ToLower` at insertion), apply the same normalization at every lookup site. A mismatch causes silent lookup failures for inputs with non-canonical casing (e.g., mixed-case Python module names like `OpenSSL`). Audit all functions that query the map, not just the one you're currently editing.
- **Sanitize Dynamic Content in GitHub Actions Workflow Commands**: When embedding dynamic content (shell variables, step outputs) into GitHub Actions workflow commands (`::warning::`, `::error::`, `::set-output::`), sanitize multi-line content and `::` sequences first — they break command parsing and can inject accidental workflow commands. Emit a short single-line summary and log the full payload separately.
- **Populate Sentinel Error Fields on Graceful Skip Paths**: When short-circuiting a function that returns a result struct whose Error field is inspected downstream as a sentinel (e.g., batch assembly "mark not found" logic), populate the Error field even on graceful skip paths. A zero-value struct with nil Error breaks sentinel checks and silently omits the entry from result maps.
- **Use Ecosystem-Neutral Language in Multi-Language Error Messages**: When a CLI tool supports multiple ecosystems, error hints and suggestions must not reference language-specific files (e.g., `go.mod`) unless the current context is confirmed to be that language. Generic messages like "dependency manifest not found" are safer than ecosystem-specific ones.
- **Extract Shared Helpers for Near-Duplicate Code Paths**: When two functions follow the same sequence (e.g., parse input → call external API → interpret result → populate output) differing only in how one parameter is obtained, extract the shared sequence into a single helper parameterized on that value. Near-duplicate paths drift silently when logging, error handling, or evidence formatting is updated in one copy but not the other.
- **Narrow Candidate Heuristics and Map Assertions to Specific Items**: When generating candidate values (import paths, match keys) from heuristics, validate each candidate against its target domain to avoid false-positive attribution from overly broad matching. Similarly, when asserting on `map[K]V` results, check the specific key under test (`paths := m[key]; len(paths) == 0`) — not the whole map (`len(m) == 0`), which only confirms any key has data without verifying the key you care about.
- **Narrow Heuristic Candidate Sets to Avoid False Attribution**: When building candidate lists for matching (e.g., import-path heuristics, file-type detection), prefer precise patterns over broad substring matching. An overly broad heuristic (e.g., taking only the last segment after a delimiter) can collide with unrelated entries and cause false attribution (e.g., marking an unrelated dependency as "used"). Add validation or specificity constraints to each candidate before insertion.
- **Verify Tree-Sitter Query Patterns Do Not Overlap**: When adding new tree-sitter (or similar AST) query patterns to a multi-pattern query, verify that the new pattern does not match nodes already captured by an existing pattern via parent-child nesting. For example, a standalone `member_expression` pattern already matches the inner `pkg.Foo` node inside `new pkg.Foo()`, so adding a `new_expression` wrapping `member_expression` pattern would double-count the same call site. Test with representative code that exercises both the new and existing patterns.
- **Constrain Tree-Sitter Queries with Predicates for Framework-Specific Patterns**: When adding tree-sitter query patterns that target framework-specific AST shapes (e.g., Angular decorators, Vue component registrations), use `#eq?` or `#match?` predicates to constrain matches to the intended decorator names, function names, and property keys. Unconstrained structural patterns (e.g., "any decorator with an array argument") match far more broadly than intended and introduce false-positive call sites from unrelated code that happens to share the same AST shape. Always verify that `FilterPredicates` is called in the match loop so predicates are actually applied, and use dedicated capture names for predicate-only captures (e.g., `@decorator`, `@metaKey`) that are excluded from counting logic.

- **Align Gating Predicates with Gated Function Semantics**: When a predicate function (e.g., `needsX()`) decides whether to invoke a downstream operation (e.g., `applyX()`), its conditions must mirror the downstream function's actual write/replace rules. A looser predicate triggers wasted work (e.g., HTTP fetches whose results are never applied); a tighter predicate silently skips cases the downstream function would handle. Similarly, when short-circuiting a function that returns a result struct inspected by downstream sentinel checks, populate all sentinel fields even on graceful skip paths — a zero-value struct with nil sentinel fields breaks downstream logic.
- **Guard Ecosystem-Specific Heuristics by PURL Type**: When a detection heuristic (aggregator flattening, workspace detection, monorepo inference) is designed for a specific package ecosystem, guard it with an explicit PURL type check (e.g., `rootType != "maven"`). Structural properties like shared namespaces and parent-child dependency trees exist across ecosystems (Maven groupIds, npm scopes, PyPI namespaces) but have different semantics. Without a type guard, a Maven-specific heuristic can misfire on npm or other ecosystems that happen to share the same structural pattern, silently rewriting dependency graphs.
- **Verify External-Service URL Conventions Against the Live Service, Not Folklore**: When generating URLs for external services (deps.dev, npm registry, mvnrepository, etc.), don't trust the obvious-looking pattern from the package coordinates — services often have non-obvious URL conventions (e.g., deps.dev's React Router pattern `/:system/:name/:version?` matches `:name` against `[^/]+` only, so multi-segment names *must* be path-escaped; Maven uses `groupId:artifactId` joined with `:`, not `/`; deps.dev does not host Packagist/Hex/Swift despite accepting any path server-side because its SPA shell always returns 200). Verify by hitting the service's actual data endpoint (or simulating its router) for both common cases and edge cases (multi-segment names, scoped packages, ecosystem-specific separators) before claiming the URL works. Bake the verification into the test suite as an opt-in live probe (env-var gated, e.g., `UZOMUZO_LIVE_PROBE=1`, so `go test ./...` stays hermetic) and pin the expected-URL fixture inside the test file so the cross-file convention can't drift silently between near-duplicate helpers.

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
  - category: "defensive-coding"
    summary: "Sanitize shell variables before embedding in GitHub Actions ::warning:: workflow commands — multi-line content or :: sequences break command parsing and can inject accidental workflow commands; emit a short single-line warning and log the full payload separately"
    pr: 338
    file: ".github/workflows/copilot-clean-label.yml"
    date: "2026-04-28"
  - category: "testing"
    summary: "Test failure branch accessed struct field through potentially-nil pointer in error message — split nil guard (t.Fatalf) from value assertion to prevent panic masking the actual regression"
    pr: 318
    file: "internal/infrastructure/integration/populate_project_test.go"
    date: "2026-04-20"
  - category: "defensive-coding"
    summary: "When a multi-branch resolution function (e.g., name-first then URL fallback) records evidence in a Raw/provenance field, set Raw to the input that actually produced the match — not the input from a prior branch that failed. Misattributed Raw values lose traceability for debugging and audit"
    pr: 345
    file: "internal/infrastructure/maven/license.go"
    date: "2026-04-29"
  - category: "concurrency"
    summary: "Acquire bounded-concurrency semaphore before launching goroutine (not inside it) and select on ctx.Done to stop dispatch on cancellation — avoids spawning thousands of parked goroutines and respects context lifecycle"
    prs: [345]
    instances: 2
    file: "internal/infrastructure/integration/populate_manifest_license.go"
    date: "2026-04-29"
  - category: "defensive-coding"
    summary: "When a conditional replacement function overwrites low-quality data (non-SPDX) with a new source, guard the write on the new source being strictly higher quality (e.g., contains at least one SPDX entry) — replacing non-standard with non-standard is a no-op that wastes provenance"
    pr: 345
    file: "internal/infrastructure/integration/populate_manifest_license.go"
    date: "2026-04-29"
  - category: "whitespace-agnostic-matching"
    summary: "Use bytes.Fields tokenization instead of fixed-separator prefix checks when matching directives — tabs and multiple spaces are valid separators"
    pr: 140
    file: "internal/infrastructure/depparser/detect.go"
    date: "2026-04-05"
  - category: "comment-doc-drift"
    summary: "Doc comments must match implementation boundary conditions — RetryConfig said retryDecider controls 429 retries (it doesn't); rateLimitBackoff comment said 'negative' for a zero-inclusive guard (should say 'non-positive')"
    pr: 359
    file: "internal/infrastructure/httpclient/client.go"
    date: "2026-04-29"
  - category: "defensive-coding"
    summary: "Guard time.Duration arithmetic against integer overflow — use strconv.ParseInt, reject values exceeding math.MaxInt64/time.Second, and do not clamp to an arbitrary policy constant (let the caller's configured cap decide)"
    pr: 359
    file: "internal/infrastructure/httpclient/client.go"
    date: "2026-04-29"
  - category: "logging-consistency"
    summary: "Pass typed error values (e.g., *QueryError) directly to slog instead of pre-stringifying via .Error() — preserves type/structure and keeps logging consistent with slog conventions"
    pr: 346
    file: "internal/infrastructure/treesitter/analyzer.go"
    date: "2026-04-29"
  - category: "testing"
    summary: "When generating time-based test fixtures with coarse-grained formatters (e.g., http.TimeFormat at 1-second granularity), truncate to the format boundary and add enough offset (e.g., 2s) so the formatted value is deterministically in the expected range — sub-granularity offsets (50ms) can collapse to the current or past second"
    pr: 359
    file: "internal/infrastructure/httpclient/client_test.go"
    date: "2026-04-29"
  - category: "testing"
    summary: "Assert diagnostic context fields on typed error structs; match context-error assertions to the specific context constructor (WithCancel → Canceled only, WithTimeout → DeadlineExceeded only) — permissive OR hides misrouted error paths"
    pr: 359
    file: "internal/infrastructure/httpclient/client_test.go"
    date: "2026-04-29"
  - category: "defensive-coding"
    summary: "Cap response body reads with io.LimitReader in retry paths (429/5xx) to prevent unbounded memory and log growth across retry attempts — consistent with existing codebase pattern for HTTP body reads"
    pr: 359
    file: "internal/infrastructure/httpclient/client.go"
    date: "2026-04-29"
  - category: "defensive-coding"
    summary: "Use time.NewTimer + Stop/drain instead of time.After in select with ctx.Done() to prevent timer accumulation during long cancellable waits"
    pr: 359
    file: "internal/infrastructure/httpclient/client.go"
    date: "2026-04-29"
  - category: "api-consistency"
    summary: "Redundant gh pr view API call to fetch labels when pr_json from repos/.../pulls already contains label data — reuse already-fetched API response data instead of making redundant calls for a subset of the same information"
    pr: 338
    file: ".github/workflows/copilot-clean-label.yml"
    date: "2026-04-27"
```

<!-- Promotion history (kept for audit trail):
  # defensive-coding: promoted to copilot-learned-coding.instructions.md (PRs #340, #345 — align gating predicates with gated function semantics: predicate conditions must mirror downstream write/replace rules; populate sentinel fields on graceful skip paths)
  # comment-doc-drift (PR #345): already covered by "Comment-Code Consistency" rule — doc comment described non-existent debug-level logging, rate-limit signal, and per-client caching
  # naming-consistency (PR #345): trivial spelling fix ("licence" → "license"), not recorded as pattern
  # defensive-coding: promoted to copilot-learned-coding.instructions.md (PR #345 round 2 — normalize config values once before guard and use: TrimSpace in guard but passing untrimmed value to SetBaseURL)
  # testing (PR #345 round 2): already covered by "Assert Exact Computed Values, Not Just Thresholds" in testing-performance.instructions.md — assert exact POM fetch count, not minimum
  # concurrency (PR #345 rounds 2+3): accumulated (2 instances) — bound fan-out with semaphore before goroutine launch + ctx.Done select
  # comment-doc-drift (PR #345 round 3): already covered by "Comment-Code Consistency" rule — Raw precedence comment inconsistent with implementation
  # comment-doc-drift: promoted to copilot-learned-coding.instructions.md (PRs #298, #299, #318, #336 — doc comments must match type-level constraints: no "nil" for non-pointer types, scope claims must match actual implementation, test names/examples must match exercised code)
  # testing: promoted to testing-performance.instructions.md (PRs #318, #336 — keep tests network-independent by default: env-var opt-in for live probes, stub transports to avoid real HTTP calls)
  # defensive-coding: promoted to copilot-learned-coding.instructions.md (PRs #324, #336 — match net/url function to semantic context: u.Hostname() not u.Host, PathEscape not QueryEscape for path segments)
  # defensive-coding: promoted to copilot-learned-coding.instructions.md (PRs #338, #340 — sanitize dynamic content in GH Actions workflow commands; populate sentinel Error fields on graceful skip paths)
  # defensive-coding: promoted to copilot-learned-coding.instructions.md (PRs #324, #338 — u.Hostname() for port-safe host comparison + go-import prefix matching per Go module spec)
  # defensive-coding: newly authored in copilot-learned-coding.instructions.md (PR #338 — Enforce Access Constraints on All CI Trigger Paths + generous pagination page sizes in verification queries)
  # security: promoted to security.instructions.md (PRs #276, #324 — normalize hostnames in SSRF denylists/cache keys: strip trailing dots + lowercase + IPv6 zone IDs before denylist checks and cache-key construction)
  # comment-doc-drift (PR #324): already covered by promoted rule — dedup comment overstated resolver cache normalization scope (trailing slash/path casing)
  # defensive-coding: promoted to copilot-learned-coding.instructions.md (PRs #318, #324 — enforce HTTP client hardening on all code paths: CheckRedirect on injected clients, transient 408/429 classification, redirect off-by-one)
  # defensive-coding (PR #324): already covered by "Use Case-Insensitive Comparison for URL Components" — hostOf used case-sensitive HasPrefix for scheme detection
  # api-consistency: promoted to copilot-learned-coding.instructions.md (PRs #223, #318 — omitempty ambiguity on boolean/slice JSON tags, exported function returning unexported type)
  # performance: promoted to copilot-learned-coding.instructions.md (PRs #315, #318 — cache expensive parsing, avoid full-collection materialization for prefix-only operations)
  # defensive-coding: promoted to copilot-learned-coding.instructions.md (PRs #281, #315 — preserve original input through heuristic fallback chains, use structured parsers for structured identifier properties)
  # defensive-coding: promoted to copilot-learned-coding.instructions.md (PRs #276, #280 — rerun analyzers with combined input, gate fallback on error, spec-compliant parsers, AST ancestor walk continuation)
  # comment-doc-drift: promoted to copilot-learned-coding.instructions.md (PRs #253, #276 — interface contract doc must match signature semantics)
  # testing: promoted to testing-performance.instructions.md (PRs #276, #282, #298 — nil map merge tests, sibling assertions, test name/code consistency, unconditional test assertions)
  # defensive-coding: promoted to copilot-learned-coding.instructions.md (PRs #277, #276 — handle all valid input forms in format parsers)
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
  # duplicate-parsing (PR #281): already covered by promoted "Extract Shared Helpers for Near-Duplicate Code Paths" rule — aliasFromPkg closure inconsistent with handleGoImport; extracted goAliasFromImportPath shared helper
  # comment-doc-drift (PR #283): already covered by "Comment-Code Consistency" rule — scoped_type_identifier comment inaccurately described capture matching behavior in countCallSites
  # comment-doc-drift (PR #283 round 2): already covered by "Comment-Code Consistency" rule — scoped_type_identifier comment still inaccurate after first fix; rewording must reflect positional capture semantics of countCallSites
  # comment-doc-drift (PR #283): already covered by "Comment-Code Consistency" rule — test comment incorrectly attributed scoped constructor match to method_invocation pattern
  # testing (PR #283): already covered by "Cover New Control Flow Branches with Tests" and "Verify Tree-Sitter Query Patterns Do Not Overlap" — each tree-sitter query pattern variant (generic vs non-generic scoped constructor) needs its own test case
  # comment-doc-drift (PR #283 round 4): already covered by "Comment-Code Consistency" rule — scoped constructor comment described 2-capture positional behavior but queries only have a single @func capture
  # comment-doc-drift (PR #285): already covered by "Comment-Code Consistency" rule — HasBlankImport comments/docs said "no callable API" but flag covers broader patterns (Python feature-detection) that may have callable APIs; aliasMap comment claimed safety without noting lack of scope resolution
  # comment-doc-drift (PR #315): already covered by "Comment-Code Consistency" rule — enrichDependentCounts comment said "stable release version" but code uses resolvedVersion() with Package.Version > StableVersion > MaxSemverVersion preference chain
  # comment-doc-drift (PR #318): already covered by "Comment-Code Consistency" rule — godoc said "chars" but NormalizeSummary counts runes; use correct unit in comments for multibyte-aware caps
  # defensive-coding (PR #318): already covered by "Use Case-Insensitive Comparison for URL Components" and existing URL handling rules — GHES REST /api/v3 suffix must rewrite to /api/graphql for GraphQL endpoint
  # comment-doc-drift (PR #323): already covered by "Comment-Code Consistency" rule — listFallbackVersions doc said "sorted by publishedAt desc" but actual sort is stability tier → semver desc → publishedAt tiebreak
  # defensive-coding (PR #323): already covered by "Preserve Original Input Through Heuristic Fallback Chains" spirit — Go module +incompatible suffix not normalized in origVersion before skipVersion comparison in fallback path
  # performance (PR #323): already covered by "Minimize Allocations in Hot Paths" — semver.NewVersion called twice per candidate (isStableReleaseForFallback + sort key); parse once and derive both
  # defensive-coding (PR #323): already covered by "Defensive Coding — Validate Early, Fail Clearly" spirit — add cheap HasVersion guard before calling expensive fallback helper for versionless PURLs
-->
