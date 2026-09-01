# 0023. The registry's own current release bounds stable-version selection

Date: 2026-09-01
Status: Accepted

## Context

[ADR-0021](0021-yank-is-version-specific.md) removed the `ReleaseInfo.StableVersion`
fallback from the yank rule, so a yanked release no longer marks an unversioned
package end-of-life. It left the other half of the problem open, and named it: the
stable version itself can still *be* a yanked release.

`pickStableDevAndMax` (`internal/infrastructure/depsdev/selection.go`) selected
stable by taking deps.dev's `isDefault=true` unconditionally. The deps.dev versions
payload is decoded in `fetchLatestRelease`
(`internal/infrastructure/depsdev/release.go`) into a struct with exactly four
fields — `versionKey.version`, `publishedAt`, `isDefault`, `isDeprecated`. deps.dev
publishes no yank data at all, for any ecosystem. PyPI does: `info.yanked` per
release, and an `info.version` that in practice excludes yanked releases.

Observed 2026-08-31 on `pkg:pypi/pydantic-extra-types`:

```
PyPI     info.version = 2.11.1   yanked=false
         2.11.2                  all files yanked
deps.dev isDefault    = 2.11.2   isDeprecated=false
```

The consequences are not confined to display. Stable selection decides the version
used for the internal deps.dev package lookup (`depsdev/batch_details.go`), which
determines which version's package info, licenses and advisory keys are fetched;
direct advisories are then indexed from that response
(`integration/populate_project.go`). Advisory severity and transitive advisories
are written back to the stable and max-semver slots only
(`integration/enrich_advisory.go`, `integration/enrich_transitive_advisory.go`),
and the lifecycle assessor reads advisory counts and max CVSS from the same two
slots (`domain/analysis/lifecycle_assessor.go`). So every version-linked aggregate
described a release nobody should install.

For an **unversioned** PURL the dependency graph follows too: `purl_batch.go`
injects `latestReleaseVersion` (stable > max-semver > pre-release) before calling
the `:dependencies` endpoint. A versioned PURL keeps the caller's version, since
`Analysis.EffectivePURL` is never replaced with stable.

## Decision

For pypi, the version the registry itself presents as current is an **upper bound**
on stable selection.

`pickStableDevAndMax` takes a `preferredStable` argument, resolved by
`registryStableVersion` from PyPI `info.version`. Stable is then:

1. a version matching `preferredStable` exactly; else
2. the greatest version `<= preferredStable` under PEP 440, ties broken by latest
   `publishedAt` then by version string; **if no version qualifies, Stable is left
   empty** — the bound is never exceeded; else
3. when the bound does not apply at all, the previous deps.dev rules — latest
   `isDefault`, else latest `IsStableVersion`.

Rule 2 is what makes this a bound rather than a lookup. When PyPI reports 2.11.1
and deps.dev has indexed only up to yanked 2.11.2, an exact-match-or-fall-back
design would select 2.11.2 and preserve the bug for the whole indexing-lag window.
Leaving Stable empty when nothing qualifies is deliberate: falling back there would
reopen the same hole for the case where deps.dev lists *only* versions above the
bound.

Ordering inside rule 2 is by version alone. An earlier draft preferred
non-pre-releases ahead of version order; that let a hint of `100.0` select stable
`1.0` over `99.0rc1`, which is a worse answer than the behavior being replaced.
Stability is not a tier here.

The bound does not apply — leaving selection entirely on rule 3 — when the PURL is
not pypi, no PyPI client is wired, PyPI returns 404, or `info.version` is empty,
not PEP 440 parseable, or `info.yanked` is true. That last condition matters: a
project whose every release is yanked still reports an `info.version`, and that
version is itself yanked, so it cannot bound anything. Rule 1 runs before parsing,
so a non-PEP-440 `info.version` that deps.dev happens to list verbatim is still
honoured — an exact agreement between the two registries is stronger evidence than
our ability to parse the string.

`registryStableVersion` classifies errors from the error value with `errors.Is`,
not from `ctx.Err()`: a registry failure that happens to race an unrelated
cancellation must stay a suppressed registry failure. Context cancellation and
deadlines propagate; registry, HTTP and decode failures do not.

PEP 440 ordering is supplied by `github.com/aquasecurity/go-pep440-version`.
String equality alone is not sufficient — PEP 440 makes `1.0RC1` and `1.0rc1`,
and `1.0-1` and `1.0.post1`, the same version — and rule 2 needs ordering, which
no amount of string normalization provides. `purl.IsStableVersion` is deliberately
not used inside the bound: it is a keyword-substring heuristic that classifies the
PEP 440 short forms `1.0a1`, `1.0b1` and `1.0c1` as stable.

## What this does and does not guarantee

It guarantees that Stable is never a release *above* what the registry currently
presents. It does **not** guarantee the selected release is itself un-yanked: PyPI
yanks arbitrary older releases, and we hold no per-version yank data at selection
time. Aggregates therefore describe a release the registry has not superseded, not
one proven installable.

## Rejected alternatives

**Correct `StableVersion` in an integration-layer enricher.** By the time the
integration layer runs, the deps.dev package lookup has already happened for the
wrong version, so its advisory keys are the wrong version's and would have to be
re-fetched or re-attached. Fixing the selection instead makes display, the internal
lookup, advisory aggregates and lifecycle signals correct together.

**Interrogate candidate versions for yank status, newest first.** This is
ecosystem-general, would close the "older release is also yanked" gap above, and is
what cargo will need, because crates.io exposes per-version `yanked` but its
`max_stable_version` is not documented as excluding yanked releases. It costs an
unbounded number of registry requests per package and needs a call budget and a
negative-caching policy. PyPI hands us a usable bound in one request; paying for
the general mechanism to solve the pypi case would be premature.

## Not addressed here

- **`MaxSemverVersion` can still be the yanked release.** This is not cosmetic:
  max-semver feeds `GetDaysSinceLatestPublish`
  (`domain/analysis/aggregates.go`), which lifecycle branches consume
  (`domain/analysis/lifecycle_assessor.go`). Bounding max-semver is a different
  decision — its whole purpose is "highest version that exists", and a yanked
  release does still exist.
- **Other ecosystems.** Only PyPI and crates.io expose per-version yank at all.
  npm has deprecation and unpublish rather than yank; NuGet and Packagist are
  package-level; Maven has relocation. RubyGems.org does expose `yanked` on
  `/api/v1/gems/[name].json` and `/api/v2/rubygems/[name]/versions/[version].json`,
  though our own client surfaces none of it. A follow-up issue covers cargo.
- **The EOL verdict**, settled in ADR-0021.
- **`applyNpmStableDeprecation`**, which reads `StableVersion` and has its own
  whole-package versus single-version asymmetry.
