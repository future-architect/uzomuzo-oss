# ADR-0008: Transitive Composite Action Scanning

## Status

Accepted (2026-04-03) — Supersedes [ADR-0007](0007-skip-transitive-actions-scanning.md)

## Context

Phase 1 of GitHub Actions health scanning (`--include-actions`, PR #101, ADR-0005) evaluates only the Actions directly referenced in a repository's `.github/workflows/*.yml` files. It does not follow the dependency chain — if Action A is a Composite Action that internally `uses:` Action B, Action B is invisible to Phase 1.

An earlier design review considered skipping transitive scanning entirely (Phase 2 of #98), citing the rarity of Composite Actions, lack of user actionability, and API cost. That assessment was reconsidered after analyzing the security implications more carefully.

### Attack vector: Composite Actions execute on the caller's runner

When a workflow `uses:` a Composite Action, the Action's `steps[].uses` entries execute on the **caller's GitHub Actions runner**. This means a compromised or abandoned transitive Action has direct access to:

- `GITHUB_TOKEN` (with whatever permissions the job grants)
- All secrets passed via `secrets: inherit`
- The checked-out source code
- Any environment variables set by prior steps

This is fundamentally different from transitive package dependencies (e.g., a Go module's `go.mod`), which are compiled into the Action author's artifact at build time. **A transitive Composite Action runs live in the user's CI environment — the user is the direct victim.**

### Risk comparison across phases

| Phase | Target | Execution context | Impact on user |
|-------|--------|-------------------|---------------|
| Phase 1 (done) | Direct Actions in workflows | User's runner | Direct — secrets, code, tokens exposed |
| Phase 2 (this) | Transitive Actions via composite | User's runner | **Direct — same exposure as Phase 1** |
| Phase 3 (future) | Packages in Action repos (go.mod, etc.) | Action author's build | Indirect — depends on how Action is built |

Phase 2 shares the same threat model as Phase 1: compromised code runs on the user's runner. Phase 3 is a weaker, indirect risk.

### Action health as a proxy indicator

A healthy, actively maintained Action is more likely to:

- Monitor and update its own dependencies (including nested `uses:` references)
- Respond to security advisories affecting its transitive Actions
- Remove or replace abandoned internal dependencies

Conversely, if Phase 2 flags a transitive Action as stalled or abandoned, it implies the parent Action's maintainer is not managing their supply chain — which raises concerns about their package dependencies too. **Phase 2 results serve as a proxy indicator for the risks that Phase 3 would directly measure.**

### Practical scoping: depth is bounded naturally

Composite Action chains deeper than 2-3 levels are virtually nonexistent in practice. Most Actions use `runs.using: node20` or `docker`, not `composite`. Among Composite Actions, few reference other Composite Actions. A full recursive traversal with cycle detection (via `seen` set) will terminate quickly without needing an artificial `--depth` limit.

## Decision

### Implement transitive Composite Action scanning via `--show-transitive`

`--include-actions` discovers only **direct** Actions from workflow files (default behavior unchanged). When `--show-transitive` is also specified, transitive composite action dependencies are resolved and included:

1. Discover direct Actions from workflow files (existing Phase 1 behavior)
2. When `--show-transitive` is set: for each discovered Action, fetch `action.yml` (or `action.yaml`) from the Action's repository via GitHub Contents API
3. If `runs.using: composite`, extract `steps[].uses` references using the existing `ghaworkflow` parser patterns
4. Add newly discovered Actions to the scan, recursing until no new Actions are found
5. Deduplicate via a `seen` set to avoid redundant API calls and prevent cycles

### Default to direct-only: health vs. vulnerability management

For **vulnerability management**, transitive dependencies are critical — a CVE at any depth is directly exploitable. For **health/lifecycle management** (uzomuzo's scope), the calculus is different:

- A healthy, actively maintained direct Action is likely managing its own internal dependencies
- Transitive health issues are not directly actionable by the user — remediation requires upstream coordination
- If a direct Action is flagged as stalled/abandoned, that is already sufficient signal without inspecting its internals

Therefore, `--include-actions` defaults to direct-only output. `--show-transitive` is the opt-in for users who want full supply chain visibility. This flag is designed to be generic — it will also control transitive library dependency display in future SBOM scanning enhancements.

### Distinguish transitive Actions in output via `SOURCE` column

Direct and transitive Actions share the same threat model (runner-level execution), but differ in **actionability**:

- **Direct Action** (`source: "actions"`): The user chose to use it and can replace it directly.
- **Transitive Action** (`source: "actions-transitive"`): The user's direct Action depends on it internally. Remediation requires filing an issue with the Action maintainer or replacing the parent Action entirely.

A new `EntrySource` constant `SourceActionsTransitive = "actions-transitive"` is added to the domain layer. Transitive entries appear only when `--show-transitive` is specified.

#### Output format changes

The existing section-based separation (`--- GitHub Actions ---`) is replaced with a `SOURCE` column across all formats. This design anticipates future expansion (e.g., library transitive dependencies) without requiring additional section markers.

**Table format**: A `SOURCE` column is added to the verdict table. The following example uses actual CLI output format with full GitHub URLs. The `transitive` source for library dependencies is a future extension; currently only `action-transitive` is implemented.

```
VERDICT  SOURCE              PURL                                              LIFECYCLE   EOL
ok       direct              https://github.com/future-architect/uzomuzo-oss   Active      Not EOL
ok       action              https://github.com/actions/checkout               Active      Not EOL
ok       action-transitive   https://github.com/actions/cache                  Active      Not EOL
```

**Detailed format**: The summary table at the top includes the `SOURCE` column. Per-entry headers include source annotation when entries have multiple source types (e.g., `--- PURL 1 (direct) ---`, `--- PURL 6 (action) ---`, `--- PURL 12 (action-transitive) ---`).

```
--- Summary Table ---
VERDICT  SOURCE              PURL                                              LIFECYCLE   EOL
ok       direct              https://github.com/future-architect/uzomuzo-oss   Active      Not EOL
ok       action              https://github.com/actions/checkout               Active      Not EOL
ok       action-transitive   https://github.com/actions/cache                  Active      Not EOL

--- PURL 1 (direct) ---
📦 Package: https://github.com/future-architect/uzomuzo-oss
⚖️  Result: Active
...

--- PURL 6 (action-transitive) ---
📦 Package: https://github.com/actions/cache
🔗 Via: https://github.com/aquasecurity/trivy-action
⚖️  Result: Active
...
```

**JSON/CSV format**: The existing `source` field carries the new value (`"actions-transitive"`) for transitive Action results. In addition, machine-readable output includes the direct parent Action for transitive entries via an additive `via` field in JSON and an additive `via` column in CSV. This is a backward-compatible format extension.

### No `--depth` flag

Following the Configuration & Flags Policy (YAGNI), no `--depth` flag is added. The natural rarity of deep Composite Action chains means full traversal terminates quickly. If a depth limit is needed in the future, it can be added without breaking changes.

### Default branch fetch for `action.yml` (no ref pinning)

When fetching `action.yml` from a composite Action's repository, the tool reads from the **default branch (HEAD)**, not the specific ref (tag/SHA) pinned in the `uses:` directive (e.g., `@v4`, `@abc123`).

This was considered and intentionally not implemented. The rationale:

1. **Health assessment is about the repository, not a point-in-time snapshot.** uzomuzo evaluates whether a project is actively maintained — commit activity, release cadence, EOL status. These are repository-level signals that do not vary by tag. A user asking "is this Action healthy?" wants to know its current maintenance posture, not what its `action.yml` looked like at a specific release.

2. **The latest `action.yml` reflects current risk.** If a composite Action's maintainer has since added or removed internal dependencies on their default branch, that is the reality the user will encounter when they next update their pin. Assessing the default branch shows the current supply chain, not a historical one.

3. **Transitive discovery precision is not the goal.** The purpose of `--show-transitive` is to surface whether the Actions in a user's CI pipeline have healthy internal dependencies. Minor differences between a pinned ref and HEAD (e.g., a step added in a newer version) do not change the health assessment meaningfully — the transitive Action's repository is either healthy or it isn't.

4. **Implementation cost is disproportionate.** Ref-pinned fetching would require extending the GitHub Contents API client to accept a `ref` parameter, parsing the ref from every `uses:` directive, and making ref-specific API calls. The precision gain does not justify this complexity for a health/lifecycle tool.

If uzomuzo's scope expands to vulnerability detection or precise dependency graph construction, ref-pinned fetching should be reconsidered.

### Phase 3 deferred

Scanning Action repositories' package dependencies (go.mod, package.json, etc.) is not included in this decision. The rationale:

1. **Lower impact**: Package dependencies are bundled at the Action author's build time, not executed live on the user's runner
2. **Proxy coverage**: Phase 2 results indirectly indicate whether an Action's maintainer is managing their full dependency chain
3. **Higher implementation cost**: Requires fetching and parsing multiple manifest formats per Action, with significantly more API calls

Phase 3 can be evaluated independently if the proxy indicator proves insufficient.

## Consequences

### Positive

- **Full direct-execution supply chain visibility**: All code that runs on the user's CI runner — whether from direct or transitive Action references — is evaluated for lifecycle health.
- **Security-motivated scope**: The decision is grounded in a concrete threat model (runner-level secret access), not speculative completeness.
- **Proxy coverage for Phase 3**: Flagging an unhealthy transitive Action implicitly warns about the broader maintenance posture of its parent Action.
- **Sensible defaults**: `--include-actions` shows only actionable (direct) results by default. `--show-transitive` is opt-in for full supply chain visibility.
- **Generic transitive flag**: `--show-transitive` is input-mode agnostic, designed to also control future SBOM transitive dependency display.
- **Actionability-aware output**: Direct and transitive Actions are visually separated in output and distinguishable in machine-readable formats, so users immediately know what they can fix directly vs. what requires upstream action.

### Negative

- **Additional API calls**: Each Composite Action requires 1-2 API calls to fetch `action.yml`/`action.yaml`. Mitigated by deduplication and the natural scarcity of deep chains.
- **Possible noise**: Some transitive Actions may be flagged as "Review Needed" when deps.dev cannot resolve them. This is consistent with Phase 1 behavior.

### Neutral

- ADR-0005's mention of "Phase 3 (`--depth N`) is planned" is superseded — `--depth N` is not implemented. Full traversal is the default behavior.
- Phase 3 (Action dependency package scanning) remains an independent future consideration, unaffected by this decision.
- `--show-transitive` was extended to also control SBOM dependency relation display (direct vs. transitive). See [ADR-0009](0009-sbom-dependency-relation-detection.md) for details.
