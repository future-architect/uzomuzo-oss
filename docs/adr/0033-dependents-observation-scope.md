# ADR-0033: Report the scope of dependents observations

Date: 2026-10-29
Status: Accepted

## Context

The legacy `DependentCount` field and `Used by` display imply a package-wide count. The deps.dev REST `:dependents` endpoint measures public packages whose computed resolution includes one queried version. A zero can also mean the endpoint was unavailable, returned 404, or was never queried. The [issue 511 design review](https://github.com/future-architect/uzomuzo-oss/issues/511#issuecomment-5534353314) rejects taking the maximum of two version anchors as a substitute for a package-wide count.

## Decision

Record a `DependentsObservation` with source, queried version for deps.dev, and separate direct and total counts. A non-nil observation means the provider returned a measurement, including zero. A nil observation means no measurement was obtained. A pointer represents unknown directly and avoids a second flag that could disagree with the observation.

The deps.dev observation is scoped to the selected version (`Package.Version`, then stable, then maximum semantic version), and its total remains the provider's `dependentCount`; direct and indirect are not added because their sets can overlap. RubyGems and Packagist observations cover packages declaring a dependency on any version. RubyGems includes development dependencies by default, as does Packagist's default dependents listing. The Go REST dependents endpoint is skipped because it returns 404 for Go versions.

The CLI labels the source and scope. JSON exposes `dependents` and CSV appends source, version, direct, and total columns. `Analysis.DependentCount`, JSON `dependent_count`, and CSV `dependents` retain their existing values and are deprecated for one release. Two versions of the same package in one batch now retain separate results rather than colliding on a versionless map key.

## Limits

The REST observation is not an installation count or a package-wide union. This change does not query a second version, take a maximum, or add BigQuery to the CLI. Package-wide population is a separate decision. The chosen version and lifecycle verdict rules remain unchanged.
