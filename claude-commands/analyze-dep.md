---
description: Analyze a dependency for removal feasibility using 6-axis evaluation
arguments:
  - name: module
    description: "Module path to analyze (e.g. github.com/pkg/errors)"
    required: true
---

# Dependency Analysis: $ARGUMENTS

Analyze the dependency `$ARGUMENTS` in this project for removal feasibility. Produce a structured report covering ALL of the following sections.

## Analysis Steps

1. **Project context**:
   - Check `go.mod` for the project's Go version (determines which stdlib alternatives are available).
   - Run `go mod why -m $ARGUMENTS` to check if removing direct usage will completely eliminate this module, or if it remains as an indirect dependency.
   - Check the module's license and whether it depends on CGO.

2. **Import locations**: Find ALL files importing this module.
   - Categorize into production code (`*.go` excluding tests) and test code (`*_test.go`).

3. **Usage detail & API leakage**: For each file, identify specific functions, types, methods used.
   - **Critical**: Check if any types from this dependency appear in EXPORTED identifiers. If so, replacing it causes breaking changes.

4. **Code volume**: Count lines of code that directly interact with this dependency.

5. **Transitive dependencies**: Run `go mod graph | grep "^$ARGUMENTS"` to count how many modules this pulls in.

6. **Maintenance history**: Run `git log --all --oneline --grep="$ARGUMENTS" --since="2023-01-01"` to count historical update PRs.

7. **Replacement feasibility**: Evaluate options (ensuring compatibility with project's Go version):
   - Go standard library
   - Another dependency already in go.mod (consolidation)
   - A smaller/lighter alternative
   - Self-implementation (estimate lines for both logic and tests)

8. **Risk assessment**: Consider:
   - Is this crypto/protocol/security-related? (should NOT self-implement)
   - Is the library actively maintained?
   - Does it have domain-specific complexity?
   - Would self-implementation introduce subtle bugs?
   - Is it only used in tests?
   - Does `go mod why` show it will remain as indirect?

## Output Format

```
## [module name]

### Basic Info
| Item | Value |
|------|-------|
| Project Go version | Go 1.X |
| Full removal possible | Yes / Remains as indirect (via X) |
| License / CGO | MIT / None |
| Files using it | Total: N (prod: N, test: N) |
| Lines of interaction | N |
| Transitive deps | N |
| Update PRs (2023~) | N |
| Last updated | YYYY-MM-DD |
| Maintenance status | Active / Maintenance / Abandoned |

### Usage Details
- **Primary functions used**: (e.g., `errors.Wrap`, `uuid.New()`)
- **API leakage**: Yes / No (does the dependency's types appear in exported API?)
- **File-by-file breakdown**: (specific functions/types per file)

### Replacement Options
| Option | Estimated effort | Risk |
|--------|-----------------|------|
| (option 1) | N lines | High/Med/Low |
| (option 2) | N lines | High/Med/Low |

### 6-Axis Evaluation

| Axis | Rating | Notes |
|------|--------|-------|
| Update PR reduction | N/yr / No effect | If indirect dep remains, note "direct PRs only" |
| Clean dependency list | High/Med/Low | Does removing direct import clarify intent? |
| Code standardization | High/Med/Low | Can external API be unified with stdlib? |
| Future removal readiness | High/Med/Low | Will this auto-remove when upstream drops it? |
| Supply chain risk | High/Med/Low | Is this abandoned? Does removing reduce attack surface? |
| Code portability | High/Med/Low | Does removing make the code easier to extract/reuse? |

### Verdict
- **Recommendation**: Full removal / Self-implement / Submodule isolation / Upstream PR / Keep (wrap) / Test-only (ignore)
- **Reason**: (1-2 sentences)
- **Priority**: S / A / B / C (S=immediate, A=soon, B=when convenient, C=keep as-is)
- **Note**: Even if indirect dep remains, removing direct usage has independent value (version management delegation, future removal readiness, code portability)
```
