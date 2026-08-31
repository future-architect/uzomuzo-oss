# 0021. A version yank applies to the requested version only

Date: 2026-08-31
Status: Accepted

## Context

[ADR-0020](0020-archived-registry-liveness.md) established that `EOL-Confirmed`
requires an explicit primary-source EOL signal, and listed PyPI `yanked` (and its
crates.io equivalent) among the four signals that qualify. It did not say *which
version's* yank flag counts.

The shared helper `applyRegistryYanked`
(`internal/infrastructure/eolevaluator/evaluator.go`) resolved the version to
check as "PURL version, falling back to `ReleaseInfo.StableVersion` when the PURL
is unversioned". Its own godoc stated the opposite intent — that the PURL version
is preferred *because* "yanking is a version-specific signal". The fallback
silently inverted that whenever the caller supplied an unversioned PURL, which is
the normal case for a package-level catalog: it asked "is the registry's current
default release yanked?" and, on yes, marked the **package** `EOLEndOfLife` at
confidence 0.95.

Two facts make that resolution wrong rather than merely conservative:

1. **deps.dev cannot see yanks.** `depsdev.Version`
   (`internal/infrastructure/depsdev/api_types.go`) has no yank field — only
   `IsDeprecated`. PyPI follows PEP 592 and excludes yanked releases from
   `info.version`; deps.dev does not, and `pickStableDevAndMax`
   (`internal/infrastructure/depsdev/selection.go`) takes `IsDefault=true`
   unconditionally. So `StableVersion` can *be* the yanked release, and the rule
   then re-queries the registry about precisely the version that was yanked — a
   self-fulfilling loop.
2. **The resulting verdict is not a lifecycle measurement.** It tracks whichever
   release the registry currently calls default, so it toggles as new releases
   ship. Verified downstream: `cargo/aws-smithy-query` and `aws-smithy-xml` were
   `EOL-Confirmed` via this path in a 2026-06-05 snapshot and returned to Active
   once a non-yanked 0.62.0 was published. A signal that switches off by itself is
   not evidence of end-of-life.

Live confirmation of the reported case (2026-08-31, PyPI JSON API,
`pydantic-extra-types`): `info.version` is 2.11.1 and not yanked; only 2.11.2 has
all files yanked ("multiple feature mistakenly merged before release. no known
security risks."); 2.11.0 and 2.11.1 are not yanked. deps.dev reports 2.11.2 as
`isDefault=true, isDeprecated=false`. Users on 2.11.0 and 2.11.1 were reported as
end-of-life on the strength of a release they were not using (Zendesk #3471).

At the time of the fix, no test exercised the unversioned path: all six existing
yanked tests (four PyPI, two Cargo) used versioned PURLs.

## Decision

A yank is evidence about the version that was yanked, and about nothing else.

- The version checked by `applyRegistryYanked` is the **PURL version only**. The
  `ReleaseInfo.StableVersion` fallback is removed.
- An unversioned PURL makes the rule a **no-op**: it returns false and promotes
  nothing. It does not downgrade to a weaker EOL state either — the package
  continues through the remaining rules (PyPI inactive classifier, the
  archived/disabled → `Stalled` branch of ADR-0020, scorecard-driven
  `EOL-Effective`), so a package that is genuinely end-of-life can still be
  labelled by a signal that actually measures it.
- A versioned PURL whose version is yanked still resolves to `EOLEndOfLife`,
  unchanged. This is the case ADR-0020 contemplated and the case the godoc always
  described: a user pinned to a yanked release.

This narrows *when* the yank signal in ADR-0020's list applies. It does not remove
it from that list.

### Rejected alternative — downgrade to a human-review state

The consuming catalog proposed routing the unversioned case to a "Review Needed"
label instead of suppressing it. Rejected on layering grounds: `EOLState`
(`internal/domain/analysis/eol.go`) has exactly four values — `Unknown`,
`NotEOL`, `EOL`, `Scheduled` — and "Review Needed" is not one of them. It exists
only as the terminal fallback of `Analysis.FinalMaintenanceStatus()`, returned
when there is no EOL state *and* no lifecycle label. Reaching it from the
evaluator would mean adding an `EOLState` or suppressing the lifecycle assessment
— a new public API surface, to express something the evaluator has no evidence
for. Emitting nothing is the accurate statement: an unversioned PURL plus a yanked
default release tells us nothing about the package.

### Not addressed here — the stable-version source

`StableVersion` can still hold a yanked release, so a yanked version continues to
be *displayed* as stable, and version-linked aggregates (advisory counts and CVSS
maxima) are still computed against it. Fixing that means preferring PyPI's PEP
592-compliant `info.version` over deps.dev `isDefault` when selecting stable — a
change in a different layer, tracked separately. It is deliberately out of scope:
this ADR's decision alone stops the false end-of-life verdict.

## Consequences

- **Behavior change**: an unversioned PURL is no longer marked EOL because the
  registry's default release is yanked. Downstream, `MaintenanceStatus` moves off
  `EOL-Confirmed` for those packages unless another signal applies. Consumers that
  materialised the old verdict must re-derive after upgrading; the fix does not
  propagate to already-distributed data.
- **Unchanged**: versioned PURLs. The pinned-to-yanked detection that motivated
  the original rule is preserved.
- **Explainability**: the rule now emits no evidence at all for unversioned PURLs
  rather than evidence naming a version the caller never asked about.
