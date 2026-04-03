# ADR-0009: SBOM Dependency Relation Detection

## Status

Accepted (2026-04-03) — Extends [ADR-0008](0008-transitive-composite-action-scanning.md)

## Context

Modern SBOM generators such as [Trivy](https://github.com/aquasecurity/trivy) and [Syft](https://github.com/anchore/syft) produce CycloneDX SBOMs that include a `dependencies` section (introduced in CycloneDX spec 1.2). This section encodes the dependency graph: which component depends on which other components. By parsing this graph, uzomuzo can distinguish **direct** dependencies (those depended on by the top-level component) from **transitive** dependencies (those pulled in indirectly through other packages).

### The actionability gap

For lifecycle governance, direct dependencies are the ones users can act on immediately — they appear in `package.json`, `go.mod`, `requirements.txt`, etc. Transitive dependencies require upstream coordination: the user must either wait for a direct dependency to update, or replace the direct dependency entirely. Presenting all dependencies as a flat list obscures this distinction and makes triage harder.

### Alignment with `--show-transitive`

ADR-0008 introduced the `--show-transitive` flag for GitHub Actions scanning, where direct Actions are shown by default and transitive Composite Action dependencies are opt-in. That ADR explicitly noted the flag was "designed to be generic — it will also control transitive library dependency display in future SBOM scanning enhancements." This ADR fulfills that design intent.

## Decision

### Parse CycloneDX `dependencies` section to classify direct vs. transitive

When an SBOM contains a `dependencies` section:

1. Identify the **root component** from the SBOM's `metadata.component.bom-ref`
2. Find the root's entry in the `dependencies` array — its `dependsOn` list contains the **direct dependencies**
3. All other components in the SBOM are classified as **transitive**
4. For each transitive dependency, record which direct dependency pulls it in (the "via-parent") by walking the dependency graph

### Default to direct-only output

By default, `uzomuzo scan --sbom` displays only direct dependencies. Transitive dependencies are filtered out **before** API calls, saving network bandwidth and API quota. This matches the actionability principle: users should focus on what they can directly control.

### `--show-transitive` includes transitive dependencies

When `--show-transitive` is specified alongside `--sbom`, transitive dependencies are included in the output. This reuses the same flag introduced in ADR-0008 for Actions, maintaining a consistent CLI surface.

### Output format additions

A `RELATION` column is added to output when SBOM input is used:

**Table format:**

```
VERDICT  RELATION                  PURL                        LIFECYCLE   EOL
ok       direct                    pkg:npm/express@4.18.2      Active      Not EOL
ok       transitive (express)      pkg:npm/accepts@1.3.8       Active      Not EOL
```

**Detailed format** includes a `Relation:` line per entry:

```
--- PURL 1 ---
📦 Package: pkg:npm/express@4.18.2
🔗 Relation: direct
⚖️  Result: 🟢 Active
...

--- PURL 2 ---
📦 Package: pkg:npm/accepts@1.3.8
🔗 Relation: transitive (express)
⚖️  Result: 🟢 Active
...
```

**JSON format** adds `relation` and `relation_via` fields:

```json
{
  "purl": "pkg:npm/accepts@1.3.8",
  "relation": "transitive",
  "relation_via": "pkg:npm/express@4.18.2",
  ...
}
```

**CSV format** adds `relation` and `relation_via` columns. Both are backward-compatible additive extensions.

### Graceful fallback for SBOMs without `dependencies`

Not all SBOMs include a `dependencies` section (older tools, minimal SBOM profiles). When the section is absent:

- All components are shown regardless of `--show-transitive`
- The `RELATION` column displays `Unknown` for all entries
- No error is raised — the tool degrades gracefully

### No additional CLI flags

Following the Configuration & Flags Policy (YAGNI), no new flags are introduced. The existing `--show-transitive` flag is sufficient. The `RELATION` column appears automatically when SBOM input is detected and dependency graph information is available.

## Consequences

### Positive

- **Actionable defaults**: Users see only the dependencies they can directly control, reducing noise in large SBOMs (which can contain hundreds or thousands of transitive dependencies).
- **API efficiency**: Filtering transitive dependencies before API calls significantly reduces external requests for large SBOMs.
- **Consistent CLI surface**: Reuses `--show-transitive` from ADR-0008, so users learn one flag for controlling transitive visibility across all input modes.
- **Via-parent traceability**: The `relation_via` field helps users understand *why* a transitive dependency is present, aiding remediation decisions.
- **Graceful degradation**: SBOMs without dependency graph information work without errors, just without relation classification.

### Negative

- **Graph parsing complexity**: The dependency graph traversal adds implementation complexity compared to treating all SBOM components equally.
- **Root component detection**: Some SBOMs may have ambiguous or missing root components, requiring heuristics to identify the top-level entry.

### Neutral

- The `RELATION` column only appears for SBOM inputs with dependency information. Non-SBOM inputs (PURL, go.mod, GitHub URL) are unaffected.
- ADR-0008's note about `--show-transitive` being "input-mode agnostic" is now realized for SBOM inputs. Future input modes can follow the same pattern.

## References

- [CycloneDX Specification — Dependencies](https://cyclonedx.org/docs/latest/json/#dependencies) (spec 1.2+)
- [ADR-0008: Transitive Composite Action Scanning](0008-transitive-composite-action-scanning.md) — introduced `--show-transitive`
