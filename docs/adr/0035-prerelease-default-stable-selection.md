# ADR-0035: Select a GA release when deps.dev defaults to a prerelease

Date: 2026-11-14
Status: Accepted

## Context

ADR-0023 rule 3 selected deps.dev's newest `isDefault` version without checking its stability. This made a prerelease the Stable version even when the package had published GA releases. Stable anchors the version used for licenses, advisories, lifecycle analysis, and the GitHub-URL versioned PURL. deps.dev's `publishedAt` can reflect indexing time, so it is not a reliable ordering for GA releases. For example, Spring Boot 4.1.x can be higher than a later-published 3.5.x maintenance patch; JavaFX GA releases can have timestamps seconds apart.

## Decision

Keep ADR-0023's PyPI registry hint exact match and PEP 440 bound ahead of the deps.dev fallback, and keep ADR-0024's cargo withdrawal filter. In the fallback, classify the deps.dev default and GA candidates consistently for Stable selection. The system is the case-insensitive PURL type in `VersionKey.System`: `maven`, `npm`, `cargo`, `pypi`, or `golang`. Maven and Go use `purl.IsStableVersion`. npm and cargo additionally require an empty SemVer prerelease when parseable; PyPI additionally requires a non-prerelease PEP 440 version when parseable. A failed ecosystem parse falls back to the heuristic. The parser checks affect Stable selection only; Dev continues to use the shared heuristic and latest `publishedAt`. Max and the PyPI bound are unchanged.

A stable default stays Stable. If a default in one of these five systems is unstable, choose the highest eligible GA across all versions, including GA above the default; if none exists, keep the default. Exclude Go pseudo-versions from this override's GA candidates using `module.IsPseudoVersion`, so an untagged commit does not replace a tagged prerelease. Other PURL types retain their default-first behavior.

When no default exists, Maven, npm, cargo, and PyPI choose the highest eligible GA by version order if one exists. Otherwise they retain the prior latest-`publishedAt` heuristic-stable fallback; this preserves a Stable value for npm/PyPI strings that only the ecosystem parser identifies as prereleases. Go and other systems always retain that old no-default fallback, including heuristic-stable Go pseudo-versions.

Rank eligible GA by the existing SemVer comparator if every npm/cargo/Go candidate parses, or the existing PEP 440 comparator if every PyPI candidate parses. Otherwise use one transitive ordering for every candidate: presence of a leading numeric core (after optional `v`), then dotted numeric components, then `publishedAt`, then raw version string. A numeric version outranks `r09`; `3.20.0.Final` is ordered by its numeric core. The final tie-break removes input-order dependence. No new dependency is needed.

Extend the shared heuristic to bounded Maven `M<n>`, `AM<n>`, `CR<n>`, and `ea` (optional digits) tokens. The left boundary is the start, `.`, `-`, or `_`; the right boundary is the end, `.`, `-`, `_`, or `+`. Ignore build metadata after `+` when checking all instability keywords. A name-like segment such as `featureAM2` stays stable under the new token rule, and `0.11.0+wasi-snapshot-preview1` remains stable. These shared changes also affect non-SemVer dependency probe ranking and integration prerelease flags.

## Consequences and limits

`apacheds-core` has GA 1.0.0–1.5.7, followed by a prerelease-only 2.0.0 line from M1 in 2011 to default AM27 in 2023. The issue's exception applies to packages that have only ever published prereleases, so it does not apply here or to the similarly shaped `apacheds-core-api`. Stable is 1.5.7; AM27 remains Dev/PreReleaseVersion. Licenses, advisories, and the GitHub-URL versioned PURL anchored to Stable use 1.5.7. The recent-prerelease signal still sees AM27. Its 2023 publication is already outside the 365-day recent-stable window, so this package's recent-stable outcome does not change. This is consistent with ADR-0023's PyPI path, where `info.version` supplies the newest GA regardless of its age.

An age exception would rely on deps.dev index timestamps; a major-version-gap exception cannot distinguish ApacheDS from Quarkus or Elytron. Unknown Maven qualifier names such as `-next` can still evade the heuristic. A rare Go pseudo-version whose hexadecimal hash is `ea` followed only by decimal digits, such as `v0.0.0-20260327151000-ea1234567890`, matches the new token rule and can enter Dev and the integration `IsPrerelease` flag. This known limitation is pinned in tests; the override still excludes that pseudo-version from GA candidates. The no-proxy Go module path's existing double escape is outside this decision.
