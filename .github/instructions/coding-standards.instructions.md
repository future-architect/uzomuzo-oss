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
- **Comment-Code Consistency**: When changing implementation behavior (e.g., switching from three-dot to two-dot diff), update all comments and documentation that reference the old behavior in the same commit.
- **Documentation Command Accuracy**: When adding or updating shell commands in documentation (README, CONTRIBUTING, etc.), verify they work by checking the actual project structure. Use `go build -o <binary> .` (package target) instead of `go build -o <binary> main.go` (single file) for multi-file packages. Ensure version references match `go.mod` and CI configuration.
- **Markdown Link Validity**: When adding or editing Markdown files under `.github/` (templates, workflows, docs), use absolute paths from the repo root (e.g., `/docs/development.md`) for links to repo files, since relative paths resolve from the file's directory. Always verify that linked files actually exist before committing.
- **Nullable Field Documentation**: When documenting a pointer or optional field, enumerate **all** conditions under which it can be nil/empty — not just the primary case. For example, a `ForkSource` field should note it is empty when `IsFork` is false **and** when the parent is private/inaccessible. Similarly, ensure the comment names the correct upstream API field (e.g., `parent` vs `source`) that the implementation actually uses.
- **Defensive Coding — Validate Early, Fail Clearly**: When a constructor or factory function receives a required dependency (e.g., a service, client, or parser), validate it is non-nil and return a descriptive error rather than allowing a nil-pointer panic later. Similarly, when CLI flags are mutually exclusive, reject the invalid combination at the validation layer with a clear message instead of silently preferring one. When a data field is a collection (slice/array), emit all items in serialized output rather than silently taking only the first. When sniffing file formats, validate field **values** (not just key presence) — e.g., check `bomFormat == "CycloneDX"`, not just that `bomFormat` exists.
- **File Type Detection — Use Exact Basename, Not Suffix**: When detecting file types by name (e.g., `go.mod`), use `filepath.Base(path) == "go.mod"` instead of `strings.HasSuffix(path, ".mod")`. Suffix matching can misclassify unrelated files (e.g., `deps.mod`) and route them to the wrong parser.
- **Reject Flags That Silently Have No Effect**: When a CLI flag only applies to a specific input mode (e.g., `--sample` for PURL list files), explicitly reject it with a clear error when the input is a different mode (e.g., go.mod or SBOM). Do not silently ignore the flag — users assume their flags take effect.
- **Deduplicate Inputs Before Batch API Calls**: When accepting user-provided input lists (PURLs, URLs) that feed into batch API calls, deduplicate them while preserving first-seen order before processing. Duplicates cause redundant external calls, skew logging/counts, and waste resources.
- **Normalize User-Provided Enum Values**: When accepting string values for format selectors, mode switches, or other enums from CLI flags, normalize with `strings.TrimSpace(strings.ToLower(...))` before validation. Case-sensitive matching rejects common inputs like `--format JSON` or `--format "json "`.
