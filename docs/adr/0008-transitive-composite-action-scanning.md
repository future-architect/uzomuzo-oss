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

### Implement transitive Composite Action scanning as part of `--include-actions`

When `--include-actions` is specified:

1. Discover direct Actions from workflow files (existing Phase 1 behavior)
2. For each discovered Action, fetch `action.yml` (or `action.yaml`) from the Action's repository via GitHub Contents API
3. If `runs.using: composite`, extract `steps[].uses` references using the existing `ghaworkflow` parser patterns
4. Add newly discovered Actions to the scan, recursing until no new Actions are found
5. Deduplicate via a `seen` set to avoid redundant API calls and prevent cycles

### Distinguish transitive Actions in output via `SOURCE` column

Direct and transitive Actions share the same threat model (runner-level execution), but differ in **actionability**:

- **Direct Action** (`source: "actions"`): The user chose to use it and can replace it directly.
- **Transitive Action** (`source: "actions-transitive"`): The user's direct Action depends on it internally. Remediation requires filing an issue with the Action maintainer or replacing the parent Action entirely.

A new `EntrySource` constant `SourceActionsTransitive = "actions-transitive"` is added to the domain layer. No new CLI flags are added — the distinction is automatic when transitive Actions are discovered.

#### Output format changes

The existing section-based separation (`--- GitHub Actions ---`) is replaced with a `SOURCE` column across all formats. This design anticipates future expansion (e.g., library transitive dependencies) without requiring additional section markers.

**Table format**: A `SOURCE` column is added to the verdict table.

```
VERDICT  SOURCE              PURL                              LIFECYCLE   EOL
ok       direct              pkg:npm/express@4.18              Active      No
caution  transitive          pkg:npm/qs@6.5                    Stalled     No
ok       action              github.com/actions/checkout       Active      No
replace  action-transitive   github.com/some/abandoned-action  Abandoned   Yes
```

**Detailed format**: The source is embedded in the per-entry header label.

```
--- PURL 1 (direct) ---
📦 Package: pkg:npm/express@4.18
⚖️  Result: Active
...

--- PURL 3 (action) ---
📦 Package: github.com/actions/checkout
⚖️  Result: Active
...

--- PURL 5 (action-transitive) ---
📦 Package: github.com/some/transitive-action
⚖️  Result: Stalled
...
```

**JSON/CSV format**: The existing `source` field carries the new values (`"actions-transitive"`). No structural changes needed.

### No `--depth` flag

Following the Configuration & Flags Policy (YAGNI), no `--depth` flag is added. The natural rarity of deep Composite Action chains means full traversal terminates quickly. If a depth limit is needed in the future, it can be added without breaking changes.

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
- **No new CLI flags**: Users get deeper scanning automatically with existing `--include-actions`, following the principle of sensible defaults.
- **Actionability-aware output**: Direct and transitive Actions are visually separated in output and distinguishable in machine-readable formats, so users immediately know what they can fix directly vs. what requires upstream action.

### Negative

- **Additional API calls**: Each Composite Action requires 1-2 API calls to fetch `action.yml`/`action.yaml`. Mitigated by deduplication and the natural scarcity of deep chains.
- **Possible noise**: Some transitive Actions may be flagged as "Review Needed" when deps.dev cannot resolve them. This is consistent with Phase 1 behavior.

### Neutral

- ADR-0005's mention of "Phase 3 (`--depth N`) is planned" is superseded — `--depth N` is not implemented. Full traversal is the default behavior.
- Phase 3 (Action dependency package scanning) remains an independent future consideration, unaffected by this decision.
