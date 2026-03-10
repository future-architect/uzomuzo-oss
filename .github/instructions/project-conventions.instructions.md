# Project Conventions

## Test Data Management

- **Test Data Location**: All test data MUST be stored in the `testdata/` directory
- **Test Data Organization**: Follow Go conventions for test data structure and naming
- **Test Data Reuse**: Reuse existing test data files instead of creating duplicates

## General Principles

- Explain concepts clearly, as if to a beginner. Avoid jargon, or explain it simply.
- If the user's intent is unclear, ask for clarification.
- Break down complex problems into smaller, manageable steps and explain each one carefully.
- Before writing new code, thoroughly review existing code and describe its behavior.
- Consider how the solution will be hosted, managed, monitored, and maintained, highlighting operational concerns.
- Proactively offer advice on good coding habits and best practices.
- Adjust the approach based on feedback, ensuring proposals evolve with the project's needs.
- **Systematically Manage Large Tasks and Refactoring:** When we undertake a large-scale task, such as refactoring a major component, act as a systematic planner.
  1. **Start with a Checklist:** Before writing any code, propose a high-level plan as a numbered or bulleted checklist (a TODO list) that covers all the necessary steps. Use Markdown format for the checklist with `- [ ]` for uncompleted items and `- [x]` for completed items.
  2. **Track and Report Progress:** After completing each step, **re-display the entire checklist**. Mark the completed items using `- [x]` and explicitly state which step we are about to begin next.
  3. **Proceed Step-by-Step:** Focus on one step at a time. Await my confirmation or feedback before moving to the next item on the list.

## Configuration & Flags Policy (Environment Variables / CLI Args)

We intentionally keep the runtime configuration surface **small and stable**.

### Core Principles

1. **Do NOT add a new environment variable or main() argument casually.** Add only when there is clear, recurring operational need that cannot be solved by code defaults or existing config files.
2. **Prefer Sensible Defaults.** If 90% of use cases share one setting, bake it in as a default instead of exposing a toggle.
3. **Optimize for Operability.** A setting should exist only if an operator is realistically expected to change it across environments (dev/staging/prod) or over time.
4. **Avoid Option Bloat.** Every new flag / env var increases cognitive load, documentation surface, test matrix, and support burden.
5. **Prefer Derived / Computed Values.** If a value can be inferred (e.g., from PURLs, repository metadata, file paths), do not expose it as configuration.

### Mandatory Pre-Addition Checklist

Before adding ANY new env var or CLI flag, you MUST be able to answer YES to at least one of:

- It must vary between deployments/environments (AND cannot be inferred deterministically).
- It is a secret / credential (and thus must not be hard-coded).
- It gates an experimental feature under active evaluation (and we have a plan to remove or solidify it).
- It mitigates a production incident / scale issue that cannot be resolved with code changes alone.

If none apply: do not add it.

### Prohibited / Discouraged Additions

- Flags that simply mirror a constant because "we might want it later" (YAGNI applies).
- Duplicated knobs that control the same behavior through different mechanisms.
- Highly granular tuning parameters (thread counts, tiny timeouts) unless a proven scaling / latency case exists.

### Naming & Implementation Rules

- **Environment Variables**: UPPER_SNAKE_CASE, project-scoped prefix if needed (e.g., `UZOMUZO_`).
- **CLI Flags**: Use concise, kebab-case (e.g., `--dry-run`). Avoid abbreviations unless industry-standard.
- **Single Source of Truth**: Centralize parsing/normalization in the existing config layer (do not scatter `os.Getenv` across packages).
- **No Hidden Globals**: Pass resolved config via explicit structs; do not rely on package-level mutable state.
- **Deprecation**: Mark with a `// DEPRECATED:` comment and keep backward compatibility until fully removed in a planned cleanup.

### Removal / Refactoring

If a flag/env var becomes obsolete:

- Keep a shim reading it (if public) for at least one release cycle with a warning.
- Remove related code paths promptly after deprecation window.

### Decision Record (Lightweight)

For each new configuration surface addition, add a short inline comment or PR description rationale ("Why it must be dynamic"). This avoids re-litigating its existence later.

### Example (GOOD)

```
// Rationale: Needs to vary per deployment; external service URL differs per environment.
const envDependencyAPIEndpoint = "UZOMUZO_DEP_API_URL"
```

### Example (REJECTED)

```
// Proposed: UZOMUZO_ENABLE_COOL_FLOW (Would just toggle a code path that can auto-detect need.)
```

### Golden Rule

If in doubt: **leave it out** and revisit only when a concrete, repeated operational requirement is demonstrated.

## Tooling Constraints

### JSON Manipulation — Use Python, NOT PowerShell

When editing JSON files programmatically (e.g., applying batch judgments to `eol.catalog.*.json`), **always use Python**.

- **✅ REQUIRED**: Use Python's `json` module (`json.load` / `json.dump`) for reading, modifying, and writing JSON files. It handles Unicode escaping and string quoting correctly.
- **❌ FORBIDDEN**: Using PowerShell's `ConvertTo-Json` / `ConvertFrom-Json` + `Regex.Unescape` for JSON editing. This combination can corrupt JSON by unescaping `\"` inside string values, producing invalid output.

**Rationale**: PowerShell's `[System.Text.RegularExpressions.Regex]::Unescape()` converts valid JSON escapes (`\"`) inside string values into bare `"`, breaking JSON syntax. Python's `json` module is safe by default.
