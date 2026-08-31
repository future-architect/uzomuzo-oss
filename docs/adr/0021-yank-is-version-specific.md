# 0021. A version yank applies to the requested version only

Date: 2026-08-31
Status: Accepted

## Context

[ADR-0020](0020-archived-registry-liveness.md) established that `EOL-Confirmed`
requires an explicit primary-source EOL signal, and listed PyPI `yanked` among the
four signals that qualify (its list predates the crates.io rule, which is the same
mechanism against a different registry). It did not say *which version's* yank flag
counts.

The shared helper `applyRegistryYanked`
(`internal/infrastructure/eolevaluator/evaluator.go`) resolved the version to
check as "PURL version, falling back to `ReleaseInfo.StableVersion` when the PURL
is unversioned". Its own godoc stated the opposite intent — that the PURL version
is preferred *because* "yanking is a version-specific signal". The fallback
silently inverted that whenever the caller supplied an unversioned PURL, which is
the normal case for a package-level catalog: it asked "is the registry's current
default release yanked?" and, on yes, marked the **package** `EOLEndOfLife` — at
confidence 0.95 for PyPI, 1.0 for crates.io.

Two facts make that resolution wrong rather than merely conservative:

1. **deps.dev cannot see yanks.** `depsdev.Version`
   (`internal/infrastructure/depsdev/api_types.go`) exposes no yank field at all;
   the nearest thing it carries is `IsDeprecated`, which is a different claim.
   PyPI follows PEP 592 and excludes yanked releases from
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

### A second way the same wrong version arrives

Removing the `StableVersion` fallback is not sufficient, because the rule read
`Package.PURL` — the coordinate *analyzed*, not the one *requested*. On the GitHub
URL entry path (`AnalyzeFromGitHubURL`), Step 3 asks deps.dev for a default release
and synthesizes a versioned PURL from it, then passes that down as the analyzed
coordinate. `AnalyzeFromPURLs` writes its input into all three identity fields, so
the synthesized version reached `OriginalPURL` too, contradicting the identity model
documented in `internal/domain/analysis/aggregates.go` (for a GitHub URL,
`OriginalPURL` is the unversioned base and `EffectivePURL` carries the resolved
version). The yank rule therefore still saw uzomuzo's own choice as a caller pin:
`uzomuzo scan https://github.com/pydantic/pydantic-extra-types` reproduced the
original false `EOL-Confirmed` even with the fallback removed.

This is why the decision below is stated in terms of a named identity field rather
than "the PURL version" — the phrase is ambiguous exactly where the bug lives.

## Decision

A yank is evidence about the version that was yanked, and about nothing else.

- The version checked by `applyRegistryYanked` is the version in
  **`Analysis.OriginalPURL`** — the coordinate the caller asked about — and nothing
  else. The `ReleaseInfo.StableVersion` fallback is removed, and the rule no longer
  reads `Package.PURL` / `EffectivePURL`, which may carry a version uzomuzo selected.
- Correspondingly, the GitHub URL entry path stops leaking its synthesized version
  into `OriginalPURL`: `fetchAndValidateGitHubAnalysis` takes the requested
  (unversioned) base PURL alongside the analyzed one and restores `OriginalPURL` to
  it, matching `aggregates.go`. `EffectivePURL` and `Package.PURL` keep the resolved
  version, so analysis and display are unaffected. `CanonicalKey` is versionless and
  therefore unchanged.
- An `OriginalPURL` that is unversioned, empty, unparsable, or of another ecosystem
  makes the rule a **no-op**: it returns false and promotes
  nothing. It does not downgrade to a weaker EOL state either. Nothing else in the
  chain is suppressed: the PyPI inactive classifier already ran ahead of this rule,
  the remaining evaluator rules still run, and the lifecycle assessor still reaches
  the archived/disabled → `Stalled` branch of ADR-0020, scorecard-driven
  `EOL-Effective`, or `Review Needed`. A package that is genuinely end-of-life can
  still be labelled by a signal that actually measures it.
- An `OriginalPURL` whose version is yanked still resolves to `EOLEndOfLife`,
  unchanged. This is the case ADR-0020 contemplated and the case the godoc always
  described: a user pinned to a yanked release. PURL-list, `go.mod`, and SBOM inputs
  all carry the caller's own version, so those pins keep working.

This narrows *when* the yank signal in ADR-0020's list applies. It does not remove
it from that list.

### Rejected alternative — downgrade to a human-review state

The consuming catalog proposed routing the unversioned case to a "Review Needed"
label instead of suppressing it. Rejected on layering grounds: "Review Needed" is
a **lifecycle-axis label**, not an EOL state. `EOLState`
(`internal/domain/analysis/eol.go`) has exactly four values — `Unknown`, `NotEOL`,
`EOL`, `Scheduled` — so the evaluator has no way to express it. `LabelReviewNeeded`
is reached either by the lifecycle assessor deciding the evidence is insufficient
(`lifecycle_assessor.go`) or as the terminal fallback of
`Analysis.FinalMaintenanceStatus()`; neither is the evaluator's to request. Doing
so would mean adding an `EOLState` or having the evaluator drive the lifecycle
axis — a new public API surface, to express something the evaluator has no
evidence for. Emitting nothing is the accurate statement: an unversioned PURL plus
a yanked default release tells us nothing about the package. Suppressing the
verdict lets the assessor reach `Review Needed` on its own when no other signal
fires, which is the outcome the request was after (observed on
`pkg:pypi/python-apt`).

### Not addressed here — the stable-version source

`StableVersion` can still hold a yanked release, so a yanked version continues to
be *displayed* as stable, and version-linked aggregates (advisory counts and CVSS
maxima) are still computed against it. Fixing that means preferring PyPI's PEP
592-compliant `info.version` over deps.dev `isDefault` when selecting stable — a
change in a different layer, tracked separately. It is deliberately out of scope:
this ADR's decision alone stops the false end-of-life verdict.

### Not addressed here — rules that still read the analyzed coordinate

Three deprecation rules still resolve a version the caller did not choose:
`applyNpmPURLDeprecation` reads the version out of `EffectivePURL`, while
`applyNpmStableDeprecation` and the `applyDepsDevDeprecated` fallback take it from
`ReleaseInfo.StableVersion` (the latter parses `Package.PURL` only for the ecosystem
and name). On the GitHub URL entry path each can therefore still act on a version
uzomuzo selected.

They are left as-is here. Registry deprecation is a different upstream signal from a
yank: npm's `deprecated` field and deps.dev's `IsDeprecated` are commonly set at the
package level and left on every release, so "the latest release is deprecated" is a
far more defensible package-level claim than "the latest release is yanked". Whether
they should nonetheless key on `OriginalPURL` is a separate behavior decision, not a
consequence of this one. The asymmetry is deliberate and recorded here so a future
reader does not mistake it for drift.

## Consequences

- **Behavior change**: a package the caller did not pin to a version is no longer
  marked EOL because the registry's default release is yanked. This covers both
  entry paths: an unversioned PURL, and a GitHub URL whose default release deps.dev
  reports as yanked. Downstream, `MaintenanceStatus` moves off
  `EOL-Confirmed` for those packages unless another signal applies. Consumers that
  materialised the old verdict must re-derive after upgrading; the fix does not
  propagate to already-distributed data.
- **Unchanged**: versioned PURLs. The pinned-to-yanked detection that motivated
  the original rule is preserved.
- **Explainability**: the rule now emits no evidence at all when the caller pinned
  no version, rather than evidence naming a version the caller never asked about.
- **Identity model**: `OriginalPURL` becomes load-bearing rather than merely
  informational — it is now the field a rule consults to answer "what did the caller
  ask about?". Any future entry path must populate it with the caller's own
  coordinate, and any future version-specific rule should read it rather than
  `EffectivePURL`.
