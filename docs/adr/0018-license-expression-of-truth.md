# 0018. License Expression as Source of Truth

Date: 2026-04-30
Status: Accepted

## Context

The license model in `internal/domain/analysis` previously represented
license information as `ResolvedLicense{Identifier, Source, Raw, IsSPDX}`,
where `Identifier` held a single SPDX-canonical token (e.g., `"MIT"`,
`"Apache-2.0"`). At the version level the field was a slice
(`Analysis.RequestedVersionLicenses []ResolvedLicense`); the project level
was already singular (`Analysis.ProjectLicense ResolvedLicense`).

Two empirical observations forced a re-design:

1. **deps.dev's `Version.Licenses` and `LicenseDetails[].Spdx` already
   contain SPDX-syntactic compound expressions** — we routinely see
   `"MIT OR Apache-2.0"`, `"(MIT AND BSD-3-Clause)"`, and
   `"GPL-2.0-only WITH Classpath-exception-2.0"`. Treating these as a
   single atomic `Identifier` token was wrong: the substring `" OR "` does
   not normalize to any SPDX ID and `IsSPDX=false` discarded the
   legally-binding structure.
2. **The CSV exporter used a substring heuristic (`compositeExpr`)** —
   `strings.Contains` over `" OR "`, `" AND "`, `(`, `)` — to detect
   compound expressions. This was fragile (false positives on names like
   `"GNU Library OR Lesser GPL"`) and produced a boolean flag instead of
   actionable structure.

PR #358 introduced a real SPDX expression parser
(`internal/domain/licenses/expression.go`) returning an
`ExpressionResult{Raw, Root *ExprNode}` AST with `Leaves()`, `String()`
(canonical re-render), and full per-leaf `Identifier`, `Normalization`,
`OrLater`, `Exception` metadata. The parser handles real-world adversities
(free-text Maven `<name>`, `(A OR B) WITH X`, edge-operator junk).

External standards now also assume a license expression is the canonical unit:

- **SPDX 2.3 §10** (License Expressions) defines `simple-expression /
  compound-expression / license-id+ / license-id WITH exception-id` as the
  authoritative grammar; SPDX 2.3 §10.2.3 specifies operator precedence
  (`WITH > AND > OR`).
- **CycloneDX 1.6** — the `licenses[].expression` field on a component
  requires a single SPDX expression string when a license is logically
  combined; emitting a slice forces consumers to infer compound semantics.
- **EU Cyber Resilience Act (Regulation (EU) 2024/2847)** — Annex VII
  requires SBOM data sufficient to identify each component's license;
  half-normalized identifiers are not sufficient under audit.

The slice-per-version model also forced consumers to reconstruct operator
semantics from a flat list. AND vs OR mean fundamentally different things in
compliance ("must comply with all" vs "choose any one"); the information was
irrecoverable once operators were stripped. Beyond that, the existing slice
was not a true "version axis" — it was a leak of deps.dev's API shape: one
upstream version with N license strings produced N slice entries, with the
implicit-OR operator silently lost.

## Decision

The single source of truth for license data is a **canonical SPDX
expression string**, persisted alongside the upstream-original `Raw` and
provenance `Source`. The version-level slice collapses to a single
`ResolvedLicense`.

### New `ResolvedLicense` shape

```go
type ResolvedLicense struct {
    // Expression is canonical SPDX, "", or "NOASSERTION". Never half-normalized.
    Expression string
    // Source records provenance (LicenseSource* constants). Unchanged semantics.
    Source string
    // Raw holds the upstream-original string verbatim. NEVER overwritten.
    Raw string
}
```

The previous `Identifier` and `IsSPDX` fields are **removed, not renamed**:

- `Identifier` was a single SPDX token, which cannot represent compound
  expressions. Renaming it to `Expression` while keeping the same callers
  would silently break the contract for consumers reading
  `len(Identifier) > 0 ? "valid SPDX" : "non-standard"`.
- `IsSPDX` is redundant: `Expression != "" && Expression != "NOASSERTION"`
  carries the same information without imposing a leaf-level mental model
  on what may be a compound.

### NOASSERTION semantics (per SPDX 2.3 §A.1.5)

`Expression == ""` means "no license data was found upstream". This is
distinct from `Expression == "NOASSERTION"` which means "upstream
explicitly claims no assertion". CycloneDX 1.6 and SPDX 2.3 distinguish
these two cases.

| Expression | Meaning |
|---|---|
| `""` | No upstream data |
| `"NOASSERTION"` | Upstream explicitly preserved an SPDX NOASSERTION |
| canonical SPDX | Parsed and re-canonicalized expression |

### Singular `RequestedVersionLicense`

`Analysis.RequestedVersionLicenses []ResolvedLicense` becomes
`Analysis.RequestedVersionLicense ResolvedLicense` (singular). When deps.dev
returns multiple upstream license entries for one version (e.g.,
`["MIT", "Apache-2.0"]` from `Version.Licenses`), we collapse them to a
single OR-joined SPDX expression: `Expression = "MIT OR Apache-2.0"`.

The upstream array is already an implicit license set — deps.dev does not
expose the original boolean operator. SPDX's most generous reading of
"package is licensed under any of these" is OR (the licensee may pick one).
We make this implicit OR explicit. Consumers that need legal-policy
decisions can call `licenses.ParseExpression(expr).Leaves()` to re-derive
the leaf set; the operator-aware AST is one parse away.

### `NormalizeExpression` and `JoinExpressions` API

Two new package-level functions in `internal/domain/licenses`:

```go
// NormalizeExpression returns canonical SPDX, "", or "NOASSERTION".
// - empty / whitespace          → ""
// - "NOASSERTION" / "NONE"      → "NOASSERTION"
// - all-recognized leaves       → canonical re-rendered expression
// - any non-SPDX leaf           → "" (caller must keep raw separately)
func NormalizeExpression(raw string) string

// JoinExpressions OR-joins a slice of upstream license strings into one
// canonical SPDX expression. Each input is independently normalized;
// entries that yield "" are dropped. Order is preserved (first-seen wins
// on duplicates). All-NOASSERTION → "NOASSERTION"; mixed NOASSERTION +
// recognized → NOASSERTION dropped.
func JoinExpressions(rawInputs []string) string
```

Both helpers live in the **domain** layer because they manipulate domain
values without I/O — consistent with the existing `Normalize` /
`ParseExpression` placement.

### Source semantics for collapsed licenses

When a version's collapsed expression is built from multiple upstream
entries, `Source` follows: if at least one entry survived normalization
(Expression non-empty), the source is the SPDX-typed channel
(`LicenseSourceDepsDevVersionSPDX`). If every entry failed normalization,
the source is the raw channel (`LicenseSourceDepsDevVersionRaw`) and `Raw`
holds the OR-joined upstream concatenation for audit.

### Method semantics post-collapse

```go
func (r ResolvedLicense) IsZero() bool {
    return r.Expression == "" && r.Source == "" && r.Raw == ""
}

func (r ResolvedLicense) IsNonStandard() bool {
    if r.IsZero() {
        return false
    }
    if r.Expression != "" { // any recognized SPDX (incl. "NOASSERTION") => standard
        return false
    }
    switch r.Source {
    case LicenseSourceDepsDevProjectNonStandard,
         LicenseSourceGitHubProjectNonStandard,
         LicenseSourceDepsDevVersionRaw,
         LicenseSourceGitHubVersionRaw,
         LicenseSourceMavenPOMNonStandard:
        return true
    default:
        return false
    }
}
```

`IsSPDX` is **removed**. Callers should test
`r.Expression != "" && r.Expression != "NOASSERTION"` directly, or use
`licenses.ParseExpression(r.Expression).Leaves()` when leaf identity is
needed.

### Promotion rule

`promoteProjectLicenseFromVersion` (in `internal/infrastructure/integration/`)
elevates a single-leaf version expression to project-level when
`ProjectLicense` is empty or non-standard. **Compound expressions
(`"MIT OR Apache-2.0"`) are NOT promoted**: a project-level claim must be
unambiguous, and a project's own LICENSE file may pick a single leaf in
ways the upstream package metadata does not capture. The implementation
parses the version expression and gates promotion on `Root.License != nil`
(single leaf). Expressions carrying a `WITH` exception (e.g.,
`"GPL-2.0-only WITH Classpath-exception-2.0"`) are still single-leaf and
are promoted.

## Consequences

### Positive

- Compound expressions become first-class. CSV / JSON / SBOM consumers
  receive the canonical SPDX form and can call `ParseExpression` to recover
  AND / OR / WITH structure when policy decisions require it.
- The substring heuristic in CSV is replaced by a structural check
  (`len(ParseExpression(expr).Leaves()) > 1` for the
  `version_license_is_compound` column), eliminating false positives.
- SBOM emitters (CycloneDX 1.6 `licenses.expression`, SPDX 2.3
  `licenseConcluded`) get a directly-usable string without re-encoding.
- A latent bug is fixed: the previous leaf-only normalizer corrupted
  pre-compounded deps.dev entries like `"MIT OR Apache-2.0"` into
  `"MIT-OR-Apache-2.0"` via heuristic fallback. The rewrite preserves the
  compound shape end to end.

### Negative / breaking

- **Public API breakage**: `pkg/uzomuzo.ResolvedLicense` and
  `pkg/uzomuzo.Analysis` are type aliases; consumers reading
  `analysis.ProjectLicense.Identifier`, `analysis.RequestedVersionLicenses[0]`,
  or `.IsSPDX` will fail to compile. The team has confirmed the primary
  external consumer (futurevuls-backend) does not read these fields.
- **JSON output rename**: `version_licenses []string` becomes
  `version_license string` in `enrichedJSONEntry`.
- **CSV column changes**: identifier / raw / source columns are
  singularized; `version_license_count` is removed; `composite_expr` is
  replaced by `version_license_is_compound` and `version_license_leaf_count`.
- **No phased migration**: this is a single-PR breaking refactor. There is
  no compatibility shim, no deprecated alias.

### Neutral

- The on-disk catalog format is unaffected (catalog stores SPDX strings,
  not ResolvedLicense). EOL evaluation does not consult license data.
- Generated code (`spdx_generated.go`) is unaffected — the generator emits
  canonical IDs, which `ParseExpression` already consumes.

## Alternatives Considered

### A. Keep the slice, add an `Expression` field alongside

Rejected. Two sources of truth (slice + expression) drift; CSV / JSON
consumers will pick one or the other inconsistently. SPDX 2.3 mandates the
expression form for compound licenses, so the slice carries no information
the expression doesn't already carry.

### B. Keep `Identifier`, add a separate `Expression` only for compounds

Rejected. The "compound vs simple" branch becomes a second discriminator
that every consumer must respect. Worse, the boundary is unstable: upstream
may report `"MIT"` today and `"MIT OR Apache-2.0"` for the next version,
forcing every consumer to switch fields based on shape.

### C. Carry the `ExprNode` AST directly on `ResolvedLicense`

Rejected. AST nodes contain pointers, making `ResolvedLicense` no longer a
true value type. JSON marshaling becomes lossy. Storing the canonical
string and re-parsing on demand is O(microseconds) and avoids
shared-mutability hazards.

### D. NOASSERTION = empty string

Rejected per SPDX 2.3 §A.1.5: producers explicitly use NOASSERTION to mean
"I refuse to claim a license" — distinct from "I have no data". Conflating
them violates spec compliance.

### E. `[]string` (drop the struct entirely)

Rejected. `Source` (provenance) and `Raw` (upstream original) are
load-bearing for audit / SBOM fallback / promotion logic. Dropping them
loses the ability to distinguish "SPDX from deps.dev" from "SPDX promoted
from version" from "raw upstream that did not normalize."

## Migration Notes

- **No CHANGELOG.md exists** in the project today; the PR description for
  #360 lists the breaking field changes for downstream consumers.
- The existing `internal/domain/analysis/license_normalize.go`
  `NormalizeLicenseIdentifier` function is **kept** for non-expression
  callers (e.g., GitHub `licenseInfo.spdxId`, which is guaranteed-leaf by
  the GitHub API contract). The new `NormalizeExpression` is for
  expression-shaped inputs only.
- `docs/license-resolution.md` is updated to reference SPDX 2.3 §10 and the
  expression-of-truth model.
- `pkg/uzomuzo` callers must update field references; see
  `docs/library-usage.md`.
