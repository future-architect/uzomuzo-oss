# 0024. deps.dev reports cargo yanks, and they bound stable selection

Date: 2026-09-02
Status: Accepted

## Context

[ADR-0023](0023-stable-version-source.md) stopped a yanked PyPI release from being
selected as `StableVersion`, by bounding selection with the version PyPI itself
presents as current. It named cargo as the remaining gap and said cargo would need
a different, more expensive mechanism, because `pickStableDevAndMax`
(`internal/infrastructure/depsdev/selection.go`) takes deps.dev's `isDefault=true`
unconditionally and deps.dev was believed to hold no yank data.

That belief is wrong for cargo. Both ADR-0021 and ADR-0023 state that deps.dev
publishes no yank data for any ecosystem; ADR-0023 says so explicitly ("deps.dev
publishes no yank data at all, for any ecosystem") and ADR-0021 makes the
equivalent claim that `IsDeprecated` is a different signal. deps.dev does publish
cargo yanks, in the same versions payload we already fetch, under
`deprecatedReason`.

Observed 2026-09-01 on `pkg:cargo/promptforge-gateway-config`, from
`/v3alpha/systems/cargo/packages/promptforge-gateway-config`:

```
1.1.0   isDefault=true    isDeprecated=true   deprecatedReason="yanked"
0.2.0   isDefault=false   isDeprecated=false  deprecatedReason=""
```

crates.io agreed that 1.1.0 was yanked and reported `max_stable_version: "0.2.0"`.
deps.dev nonetheless marked the yanked 1.1.0 as the default version, so stable
selection returned it — the same class of bug ADR-0023 fixed for pypi, with the
same downstream consequences: the internal deps.dev package lookup, direct and
transitive advisory aggregates, and the lifecycle assessor all described a release
nobody should install.

0.2.0 was itself yanked on 2026-09-02, hours after this observation, leaving the
crate with no un-yanked release. It therefore appears in the tests only as the
all-yanked specimen; the ordinary case is pinned with `owo-colors`, whose 5.0.0 is
`isDefault` and yanked while 4.4.0 is clean.

The field was invisible to us only because the decode struct in `fetchLatestRelease`
(`internal/infrastructure/depsdev/release.go`) enumerated four fields and
`deprecatedReason` was not among them.

`deprecatedReason` is not documented to carry "yanked". The deps.dev v3alpha
reference defines `isDeprecated` as "If true, this version has been marked as
deprecated" and `deprecatedReason` as "The reason why this version is deprecated",
with no ecosystem-specific statement. The value was therefore established by
measurement. Across 17 crates and 2819 versions — including crates with substantial
yank history (`clap` 92, `num` 35, `futures` 23, `chrono` 22, `syn` 17) — deps.dev
`isDeprecated` and crates.io `yanked` disagreed on **zero** versions.

The correspondence is cargo-specific. On `pkg:pypi/pydantic-extra-types`, whose
2.11.2 release PyPI has yanked (the specimen in ADR-0023), deps.dev reports
`isDeprecated=false` for every version. PyPI yanks are invisible to deps.dev, so
ADR-0023's PyPI bound remains necessary and is not superseded.

## Decision

For cargo, a release deps.dev marks `isDeprecated` with `deprecatedReason`
"yanked" is excluded from Stable selection.

`Version` carries `DeprecatedReason`, decoded from the payload we already fetch.
`pickStableDevAndMax` partitions candidates through `withdrawnFromStable` before
any Stable rule runs, so the exclusion applies to the `preferredStable` bound, to
`isDefault`, and to the `purl.IsStableVersion` fallback alike. When every stable
release is withdrawn, Stable is left empty rather than falling back to a yanked
release — the same choice ADR-0023 made when its bound excludes every candidate,
and the same answer crates.io gives, which reports `max_stable_version: null` for
such crates (observed on `gap` and `minae-term`).

The reason string is matched with `strings.EqualFold` after trimming. A cargo
release that is `isDeprecated` with an **unrecognised** reason is logged at WARN
and treated as not withdrawn. The string is an observed value, not a contract: if
deps.dev renames it, the filter must degrade to the previous behaviour audibly
rather than going quiet. The WARN log is what surfaces that drift at runtime; the
unit tests pin our reading of the string against accidental local edits, but they
run on fixtures and cannot observe upstream change.

Dev and `MaxSemverVersion` keep the unfiltered candidate list. Max's purpose is
"highest version that exists", and a yanked release does still exist — that
reasoning is ADR-0023's and is unchanged here.

## Rejected alternatives

**Bound cargo with crates.io `max_stable_version`, mirroring the pypi mechanism.**
This was the path the follow-up issue anticipated, and it works: `max_stable_version`
does exclude yanked releases, confirmed in the crates.io source on both code paths
that build it (`Krate::top_versions` filters `versions.yanked = false`;
`controllers/krate/metadata.rs` filters `!v.yanked` before constructing
`TopVersions`, which itself only drops pre-releases). It was rejected because it
costs one HTTP request per cargo package, a new `crates.Client.GetCrate`, a new
client dependency on `DepsDevClient` and its wiring, and a cache policy — to obtain
data already present in a response we had already parsed. Its one genuine advantage,
independence from deps.dev's indexing lag, is largely illusory: the candidate list
being filtered comes from deps.dev regardless, so a package deps.dev has not
re-indexed is stale in both designs.

It would also have inherited a defect. `pickByRegistryStable` orders the bound and
the candidates with `pep440.Parse`, which is correct for pypi but not for cargo:
a semver build-metadata version such as `1.2.3+build` fails to parse and would be
silently dropped from the bound comparison, and a hint that fails to parse turns
the bound off entirely, reopening the hole being closed. Adopting deps.dev's own
flag needs no version ordering at all and avoids the question.

**Filter on `isDeprecated` alone, ignoring the reason.** `isDeprecated` is
deps.dev's ecosystem-general deprecation signal — npm deprecation is the common
case — and conflating "the author deprecated this" with "the registry withdrew
this" would change the meaning of Stable for reasons unrelated to yanking. Keying
on the reason keeps the rule to the signal actually measured, and gives the
unknown-value branch somewhere to report.

## Not addressed here

- **The premise in ADR-0021 and ADR-0023.** Both state that deps.dev holds no yank
  data. That is now known to be wrong for cargo and correct for pypi. The
  corrections are tracked separately; the decisions themselves are unaffected.
  ADR-0021's conclusion — a version yank is not package-level EOL — does not depend
  on where the yank data comes from. ADR-0023's PyPI bound is still required,
  because deps.dev does not see PyPI yanks.
- **`MaxSemverVersion` can still be a yanked release**, in cargo as in every other
  ecosystem. See ADR-0023's "Not addressed here". This bounds what an empty Stable
  buys on its own: `batch_details.go` resolves the effective PURL as
  Stable -> MaxSemver -> PreRelease -> original, so for a crate whose every release
  is yanked, resolution still lands on the yanked max-semver release. That is not a
  regression — before this change the same crate selected the yanked release as
  Stable outright — but closing it needs the max-semver decision ADR-0023 deferred,
  not this one. Where an un-yanked release exists, which is the common case, Stable
  now selects it and the fallback never runs.
- **Ecosystems other than cargo.** Whether deps.dev's `deprecatedReason` carries
  registry-withdrawal values for any other system was not surveyed beyond the pypi
  check above.
- **RubyGems.** RubyGems.org exposes `yanked`, but our own client surfaces none of
  it and deps.dev was not measured for gem yanks here.
