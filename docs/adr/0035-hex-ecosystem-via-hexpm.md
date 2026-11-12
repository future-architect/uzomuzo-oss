# ADR-0035: Support Hex through the hex.pm API

Date: 2026-11-12
Status: Accepted

## Context

deps.dev does not index Hex. `GET /v3/systems/hex/packages/:name` returns 404, so every `pkg:hex/...` PURL failed with "package not found in deps.dev", and `diet` scored Hex SBOM components as `Review Needed` with a placeholder health risk. Packagist has the same gap. There, a registry client resolves the repository URL through the deps.dev-404 fallback in `fetchBatchPURLs`, and a registry rule supplies the EOL signal.

One call, `GET https://hex.pm/api/packages/:name`, returns everything needed: `meta.links` (free-form labels such as `GitHub` or `Github`), `retirements` (a map of version to `{reason, message}`), `downloads` and `security_advisories`.

## Decision

Add `hex` as an ecosystem backed by `internal/infrastructure/hex`. The client fetches the package endpoint once per name, lowercased because Hex names are case-insensitive, and caches the response, including a 404, for five minutes. `AnalysisService` shares one client between the depsdev resolver and the EOL evaluator.

- Repository URL: `hexResolver` returns the first valid GitHub URL among `meta.links` values, checking labels in sorted order so the result is deterministic. `hex` joins `hasRegistryResolver`, so the existing fallback, project fetch and lifecycle assessment run unchanged. PURLs with a namespace (a private Hex organization) are not resolved.
- EOL: `applyHexRetired` promotes to EOL-Confirmed only when the version in `OriginalPURL` is retired, with all reasons (`deprecated`, `security`, `renamed`, `invalid`, `other`) treated alike. It reuses `applyRegistryYanked`, so an unversioned PURL is a no-op (ADR-0021). The evidence source is `hex.pm`, with confidence 1.0, and the reason and message as the summary. `hex` is added to `ecosystemsWithAuthoritativeRules`.
- Retirement is per release, not per package. Unlike the Packagist `abandoned` flag, a retired old release says nothing about the current one.
- Advisories: `security_advisories` from the package endpoint supply the advisories of `latest_stable_version`, which becomes `ReleaseInfo.StableVersion` (deps.dev leaves Hex analyses without one). hex.pm lists one vulnerability under a GHSA ID and an EEF-CVE ID that alias each other; the GHSA entry is kept, matching the IDs deps.dev reports elsewhere. Withdrawn advisories are dropped. An advisory applies when the version satisfies any of its `affected` ranges; an unparseable range counts as affecting. `cvss_score` is CVSS v4.0 and is stored in `CVSS3Score`: the severity bands match, the numeric values do not. The enrichment runs after the deps.dev severity lookup, which returns a zero CVSS3 score for these GHSA records. OSV's `Hex` ecosystem mapping serves the malicious-package check; namespaced PURLs are not mapped.
- Links: the registry URL is `https://hex.pm/packages/<name>`.

## Limits

`downloads` is not used as a dependents count, because it measures installs, not dependent packages (ADR-0033). Advisories cover the latest stable release only, as for the other ecosystems, and its publish date is not recorded. Only GitHub links resolve a repository. GitLab-only packages keep the not-found result. The EOL rule ignores the PURL namespace, as the PyPI and cargo rules do, so a private package that shares a public name would read the public retirements. Source coupling analysis does not cover Elixir or Erlang. SBOM `externalReferences` of type `vcs` remain unused, which is a separate, ecosystem-agnostic decision.
