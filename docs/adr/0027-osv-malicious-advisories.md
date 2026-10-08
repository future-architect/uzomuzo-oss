# 0027. A version named in a malicious-package advisory is replaced, whatever its lifecycle

Date: 2026-10-09
Status: Accepted

## Context

uzomuzo judges the project behind a package. [ADR-0026](0026-dormant-release-burst.md)
added one rule about how a single release was published, and it is deliberately
narrow: it only sees takeovers of packages that had gone quiet. Takeovers of
packages that were never quiet pass it by design. On `main` before this change:

```
✅ ok       pkg:npm/chalk@5.6.1               Active
✅ ok       pkg:npm/%40solana/web3.js@1.95.6  Active
🔍 review   pkg:cargo/rustdecimal@1.23.1      Review Needed
🔴 replace  pkg:npm/event-stream@3.3.6        Stalled
```

chalk@5.6.1 was published by the September 2025 npm worm; @solana/web3.js@1.95.6
stole private keys in December 2024. rustdecimal is a crate crates.io deleted as
malware; it is Review Needed only because it can no longer be found.
event-stream@3.3.6 is `replace` because its repository is archived, not because
of the backdoor.

Each of these versions is already named in a public advisory that OSV.dev serves
from the same `POST /v1/query` endpoint uzomuzo has called for cargo since
[ADR-0025](0025-rustsec-unmaintained-is-a-weak-cargo-signal.md):

| version | advisory | how it names the version |
|---|---|---|
| chalk@5.6.1 | MAL-2025-46969 | `affected.versions: ["5.6.1"]` |
| @solana/web3.js@1.95.6 | MAL-2024-11183 | SEMVER range `introduced 1.95.6`, `fixed 1.95.8`; no version list |
| rustdecimal (all versions) | MAL-2022-1 | SEMVER range `introduced 0`, no upper bound |
| github.com/boltdb-go/bolt (all versions) | MAL-2025-2545 | SEMVER range `introduced 0` |
| node-ipc@12.0.1, 9.2.3, 9.1.6 | MAL-2026-3744 | `affected.versions`, three entries |
| event-stream@3.3.6 | GHSA-mh6f-8j2x-4483 | `cwe_ids: ["CWE-506"]`, `versions: ["3.3.6"]` |
| node-ipc@10.1.1, 10.1.2 | GHSA-97m3-w2cp-4xx6 | `cwe_ids: ["CWE-506", "CWE-94"]`, SEMVER `introduced 10.1.1`, `fixed 10.1.3` |

`MAL-` records come from the OpenSSF malicious-packages project. Incidents from
before that project (event-stream 2018, node-ipc's 2022 protestware) exist only
as GitHub advisories tagged CWE-506, "Embedded Malicious Code".

## Decision

### What counts as malicious

An OSV record is admitted when

1. its own ID starts with `MAL-`, or its own ID starts with `GHSA-` and its
   top-level `database_specific.cwe_ids` contains `CWE-506`, and
2. it is not `withdrawn`.

The ID is read, never the aliases. chalk's MAL-2025-46969 and
GHSA-2v46-p5h4-248w name each other. If the MAL record were withdrawn as a
mistake, the GHSA record would still list it as an alias, and a rule that
admitted by alias would keep the version flagged. `database_specific` is defined
by each source, so `cwe_ids` is read only on a record whose ID says it came from
GitHub (the same reasoning as ADR-0025's RustSec prefix check).

There is no waiting period, unlike ADR-0025's 14 days. chalk's advisory was
filed the day the worm ran; a waiting period would let the malicious version
through for exactly the days it matters. The cost is real and accepted:
MAL-2024-2929 named `react@1.0.0` and `react@35.0.0` malicious on 2024-06-25 and
was withdrawn on 2024-07-01 as "False positive caused by problematic ingestion".
Under this rule a CI job pinned to one of those versions would have failed for
those six days. Withdrawal clears the flag on the next scan; it does not undo a
build that already failed.

### Which version is named

The rule judges the version in the PURL against each admitted record's
`affected` entries for the same package. A version is named when any entry

- lists it in `versions` (for Go, a leading `v` is dropped on both sides), or
- has a `SEMVER` range that contains it, read as OSV's schema defines: events
  in order, `introduced` opens an interval, `fixed` and `limit` close it before
  the version they name, `last_affected` closes it after. Versions are compared
  by Semantic Versioning 2.0 precedence, prereleases included and build
  metadata ignored, or
- has an `ECOSYSTEM` range consisting of `introduced: 0` alone (every version).

Anything else is not read: an `ECOSYSTEM` range with a bound (PyPI and Maven do
not order versions the way SemVer does), and a version or range that does not
parse. These are missed detections, never false ones — the standard
[ADR-0022](0022-all-releases-yanked-is-not-eol.md) set.

Without range reading, @solana/web3.js@1.95.6 and node-ipc@10.1.1 would be
missed: their records carry a range and no version list.

A PURL without a version is flagged only by a record that names every version.
A clean release after a malicious one is not flagged: chalk@5.6.2 is not named
by MAL-2025-46969 and stays `ok`.

### What changes in the output

The lifecycle label does not change. It is part of the library API
(`pkg/uzomuzo`), and consumers branch on it; a version being malicious says
nothing about whether the project is maintained. Instead the fact is carried on
the analysis, next to the label, the way `archived` already is, and every
consumer reads it:

| consumer | effect |
|---|---|
| status column (`DeriveVerdict`) | `replace`, checked before every other rule, including "not found" |
| `--fail-on` | fails the scan when any trigger is set; `--fail-on malicious` sets only this one |
| detailed view | the first verdict line names the advisory; the lifecycle line follows |
| table | the LIFECYCLE cell carries the advisory ID next to the label |
| JSON | a `malicious` object (advisory ID, summary, URL, whether it names one version or every version) |
| CSV | a `malicious_advisory` column, added last |
| `diet` | the maximum health risk for that dependency |

ADR-0026 rejected changing only the status column, because `--fail-on` read the
lifecycle label and the change would never stop CI. Here `--fail-on` reads the
fact itself, so that objection does not apply.

**Any non-empty `--fail-on` now also fails on a malicious version.** A CI job
with `--fail-on eol-confirmed` that passed chalk@5.6.1 before this change fails
after it, with no configuration change. A gate set up to stop bad dependencies
should stop the worst one; a job that wants the old behaviour has no way to ask
for it, which is deliberate.

Replacing the label with Review Needed, as ADR-0026 does, was rejected. That
rule replaces only Active and Legacy-Safe so that existing gates keep firing, so
a malicious version of a Stalled package would show nothing. And Review Needed
means a person must decide; for a version an advisory database has confirmed as
malware, there is nothing left to decide.

### Ecosystems and names

npm, PyPI, crates.io, Go, Maven, NuGet, RubyGems and Packagist: every ecosystem
with a `MAL-` feed in OSV that uzomuzo analyses. The PURL is turned into OSV's
package name: `@scope/name` for npm, `group:artifact` for Maven, the full module
path for Go, and the PEP 503 normalised name for PyPI (`Friendly_Bard` is
queried as `friendly-bard`). The other names are sent as written; OSV matches
them case-sensitively.

A package that the registry has deleted is still looked up: the advisory
outlives the package. rustdecimal@1.23.1 becomes `replace`.

### When the lookup fails

A failed or incomplete lookup (an error, or more than 10 pages of advisories) is
"not checked", never "not malicious". It does not change any verdict, so an OSV
outage cannot flip a whole scan. It is not silent either: the scan ends with one
warning naming how many dependencies could not be checked, and the JSON entry
reports the check as unknown. The count is of dependencies, the same entries
the JSON marks unknown: chalk@5.6.1 and chalk@5.6.2 share one OSV query, and
when it fails they count as two.

## Consequences

- One `POST /v1/query` per distinct package per scan, paginated. Cargo packages
  cost nothing extra: the ADR-0025 lookup for the same package is cached.
  Measured on 2026-10-09 with the two lockfiles of ADR-0026, sending the
  queries alone with 16 workers and no cache: microsoft/vscode (b99bfafc0a36)
  needs 1,188 queries in 36 seconds, npm/cli (b317f16c80df) 839 queries in 25
  seconds, with no failures. These are the OSV requests only, not a whole scan,
  where they run alongside the other lookups.
- **Not covered: takeovers recorded without a machine-readable marker.**
  ctx@0.2.6 (PyPI, 2022) is described as a takeover in PYSEC-2022-199's text,
  and its GitHub advisory has an empty `cwe_ids`. Neither is admitted, and
  reading advisory prose was rejected as a source of false positives. ctx is
  Review Needed today only because PyPI deleted it.
- Not covered: GitHub advisories for malware that are tagged with another CWE
  (rest-client 1.6.13 is CWE-94 only), RustSec's `malicious` category (every
  case checked also had a MAL record), bounded `ECOSYSTEM` ranges, and a
  package-level signal for "this package once shipped a malicious release".
- Advisories arrive after the attack. During the first hours, before a record
  exists, this rule says nothing; ADR-0026 remains the only signal for that
  window, and only for dormant npm packages.
- A withdrawn record stops flagging on the next scan. Until it is withdrawn, a
  false record fails every armed gate that resolves the named versions
  (MAL-2024-2929 above).
