# ADR-0012: Verdict "replace" Naming

## Status

Accepted

## Context

uzomuzo's `--show-only` flag filters scan output by verdict. The four verdict values are `ok`, `caution`, `replace`, and `review`. A concern was raised that `replace` could be confused with Go's `go.mod` `replace` directive, especially when scanning go.mod files.

Two alternatives were considered:

1. **Rename to `eol`** — eliminates go.mod ambiguity, but becomes semantically narrow if future checks (e.g., build health, vulnerability severity) also warrant a "should be replaced" verdict.
2. **Rename to `end-of-life`** — more explicit, but the same extensibility concern applies plus verbosity.

## Decision

**Keep `replace` as the verdict name.** Clarify its meaning in `--help` text.

### Rationale

The four verdicts function as severity levels (signal colors), not as descriptions of a specific check category:

| Verdict   | Signal | Meaning                          |
|-----------|--------|----------------------------------|
| `ok`      | Green  | No action needed                 |
| `caution` | Yellow | Warning signs, monitor           |
| `replace` | Red    | Dependency should be replaced    |
| `review`  | Grey   | Insufficient data, manual review |

`replace` is an **action recommendation**, not a diagnosis. Today it triggers for EOL and archived repositories; tomorrow it could equally apply to dependencies with unfixable build failures or critical unpatched vulnerabilities. An action-oriented name remains stable as the set of underlying checks grows.

Renaming to `eol` would require another rename when non-EOL conditions are added — violating the principle of stable CLI surfaces.

### Disambiguation

The go.mod `replace` directive confusion is addressed in `--help` text:

```
--show-only  Filter output by verdict (ok,caution,replace,review).
             replace = dependency should be replaced (EOL, archived, etc.)
```

## Consequences

- Verdict values (`ok`, `caution`, `replace`, `review`) are part of the stable CLI API.
- Future check categories (build health, vulnerability severity) can map to existing verdicts without renaming.
- `--help` text must clearly describe what each verdict means.
