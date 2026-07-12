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
- **Architectural rationale belongs in ADRs, not comments — reference by ID, do not restate**: The reasoning behind a design decision (why we chose this, which alternatives we rejected, the forces at play) lives in an ADR, which is the single source of truth. Comments / godoc must NOT copy that reasoning; reference the ADR by ID instead (`// See ADR-NNNN.`). The two have different jobs: an ADR is an append-only decision history (it never erases the past), while a comment is a snapshot of what the code does *now*. Writing the rationale in both means a later code change updates only one, they drift apart, and the ADR loses the value that makes it worth keeping — stopping a rejected proposal from being re-proposed. Apply this at authoring time, before the `comment-impl-drift` / `narrative-drift` accumulator catches the drift after the fact. Cite by a stable anchor (ADR ID, function name, heading), never by copying the ADR's prose.

### Where to Write It: How / What / Why / Why Not

Before writing a comment, decide where it belongs using this filter:

- **How** (the mechanics of the implementation) → **the code itself**. Naming and function
  decomposition should carry this; do not restate it in prose.
- **What** (what the function does) → **the godoc's one sentence** for exported identifiers,
  or the test case name.
- **Why** (why this change was made) → **the commit message** / **PR description**. Design
  rationale spanning more than one sentence belongs in an ADR, referenced by ID (see the rule
  above).
- **Why not** (why an obvious-looking alternative was deliberately avoided) → **the comment**.
  This is the only case where a comment earns its keep — e.g. why a guard clause exists, why a
  naive approach was rejected, why a fallback is fail-open instead of fail-closed.

Comments that restate How rot the moment the implementation changes, since nothing forces
them to be updated in lockstep.

## API Design and Backward Compatibility

Any exported function, type, or constant is part of our public API. API stability is crucial.

- **Additive Changes are Preferred**: When modifying an exported struct, prefer adding new, optional fields over changing or removing existing ones.
- **Use the Options Pattern for Arguments**: Avoid adding new arguments to an existing exported function. Instead, use the "functional options pattern" for optional arguments to ensure backward compatibility.
- **Interfaces are (Almost) Forever**: Be very deliberate when designing exported interfaces, as adding methods to them is a breaking change.

## Learned from Copilot Reviews

Coding-standards rules learned from Copilot reviews are maintained in a dedicated file to reduce merge conflicts during parallel development. See `copilot-learned-coding.instructions.md`.
