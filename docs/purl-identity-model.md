# PURL Identity Model (OriginalPURL / EffectivePURL / CanonicalKey)

[← Back to README.md](../README.md)

A 3-layer structure to balance stable internal matching with complete preservation of user input.

## 3-Layer Overview

| Field | Persistence Scope | Mutation | Purpose | Example |
|-------|-------------------|----------|---------|---------|
| `OriginalPURL` | Returned externally | Write-once | The coordinate supplied to the analysis layer (preserving case / collapsed forms). For audit logs, reproducibility, and answering "which version did the caller ask about?" | `pkg:npm/React` |
| `EffectivePURL` | Returned externally | Updated during resolution | Resolved/normalized form used for API calls (may add version, expand coordinates) | `pkg:npm/react@18.3.1` |
| `CanonicalKey` | Internal only | Lazily generated | Stable key with version stripped + lowercased (dedup / EOL matching). Not exposed to users | `pkg:npm/react` |

## Helper Methods

- `DisplayPURL()` — Prefers `OriginalPURL`, falls back to `EffectivePURL`
- `IsVersionResolved()` — True if `EffectivePURL` contains `@version`
- `EnsureCanonical()` — Generates `CanonicalKey` if empty (uses Original preferentially)

## Rationale for Separation

1. **Case preservation**: Prevents corruption in case-sensitive ecosystems (Maven paths / Go import paths)
2. **User input preservation**: Maintains original input for audit and reproducible reruns
3. **Non-destructive rewrites**: Version resolution and path expansion do not modify the original request
4. **Centralized matching**: Consolidates internal maps / EOL matching to a single deterministic transform (`CanonicalKey`)
5. **Telling a caller pin apart from our own choice**: version-specific EOL rules must know whether a version was requested or selected on the caller's behalf. `OriginalPURL` is the only field that answers this

## What "user input" means per entry path

`OriginalPURL` is the coordinate handed to the analysis layer, which is not always
byte-identical to what the user typed:

| Entry path | `OriginalPURL` |
|------------|----------------|
| PURL passed directly (CLI arg, PURL list) | verbatim |
| `go.mod` | PURL constructed from the module path, with `replace` directives applied |
| CycloneDX SBOM | component PURL with tool-added qualifiers and subpath stripped |
| GitHub URL, package resolved | the derived **unversioned** base PURL |
| GitHub URL, no package identity | the raw GitHub URL |

Every adapter preserves the caller-selected version. None of them writes a version
uzomuzo resolved.

The last row is the case where no PURL exists to record: the repository has no
registry package, or deps.dev does not know the derived one, so `Package` and
`ReleaseInfo` are nil and the analysis is GitHub-only (`buildGitHubOnlyAnalysis`
and the not-found branch of `fetchAndValidateGitHubAnalysis`). A raw URL does not
parse as a PURL, so version-specific rules stay a no-op on it — which is the right
answer when there is no package coordinate at all.

## OriginalPURL is load-bearing, not decorative

`applyRegistryYanked` (PyPI / crates.io yank detection) reads the version from
`OriginalPURL` and does nothing when it is absent. Two rules follow:

- **Never back-fill `OriginalPURL` with a resolved version.** Doing so makes a
  version uzomuzo picked look like a version the caller pinned, and a yank on that
  version then marks the whole package end-of-life. This was a real defect on the
  GitHub URL path — see [ADR-0021](adr/0021-yank-is-version-specific.md).
- **Every entry path must populate it.** A source that leaves it empty silently
  loses yank detection rather than failing loudly. As a safety net,
  `AnalysisService.enrichAndAssess` repairs an empty value from the map key and logs
  a warning, because reaching it means an `AnalysisSource` broke its contract. The
  map key is what the caller requested, so the repair reproduces the table above: a
  PURL on the PURL path, the raw URL on the GitHub URL path. It cannot recover the
  derived base PURL — only the adapter that resolved it knows that — so a source
  that wants the resolved identity must set the field itself.

Rules that need the coordinate actually analyzed must read `EffectivePURL` instead.

## Common Divergence Scenarios

| Scenario | OriginalPURL | EffectivePURL |
|----------|--------------|---------------|
| npm Mixed Case | `pkg:npm/React` | `pkg:npm/react@18.3.1` |
| Maven collapsed coordinates | `pkg:maven/org.slf4j:slf4j-api@2.0.16` | `pkg:maven/org.slf4j/slf4j-api@2.0.16` |
| GitHub URL base + version resolution | `pkg:golang/github.com/gin-gonic/gin` | `pkg:golang/github.com/gin-gonic/gin@v1.10.0` |
| Resolution failure (no version) | `pkg:pypi/Django` | `pkg:pypi/Django` |

## Operating Rules

1. Record the caller's coordinate as `OriginalPURL` **exactly once** at ingestion
2. Reflect transformation results in `EffectivePURL` **only** — never write a
   resolved version back into `OriginalPURL`
3. Call `EnsureCanonical()` **immediately before** storing in internal maps / merge logic
4. Arbitrary lowercasing is performed **only** via `CanonicalKey` utilities

## Quick Reference

```text
OriginalPURL  = The caller's coordinate (what was asked about)
EffectivePURL = Final resolved/fetched form
CanonicalKey  = Internal versionless lowercase key (generated by EnsureCanonical())
DisplayPURL() = Display form (Original preferred)
```
