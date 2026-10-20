# 0030. An optional maintainer e-mail domain notice

Date: 2026-10-20
Status: Proposed

## Context

An expired e-mail domain associated with an npm maintainer can allow an account takeover. In the public node-ipc incident, the account `atiertant` used `a.tiertant@atlantis-software.net` in earlier release metadata. Current RDAP registration starts on 2026-05-07, after that address first appeared in the npm packument on 2020-11-11 and before the first malicious publication on 2026-05-14. This is a registration timing signal, not proof of who controlled the account.

The existing npm release-history request already retrieves the full packument. An optional check can use its maintainer addresses without an extra registry request. RDAP adds a separate network lookup, so the check must remain opt-in.

## Decision

`scan --maintainer-domain` checks top-level npm maintainers. For each e-mail address, the earliest dated appearance in a version's `maintainers` or `_npmUser` field is compared with the current registration event from RDAP. The comparison uses the scan time as its base. An address absent from version history cannot be dated and is `unchecked`.

Shared mail providers, private public suffixes, and TLDs absent from the embedded IANA RDAP bootstrap are `unchecked`. A failed lookup is `lookup_failed`; HTTP 404 is `unregistered`. RDAP status `redemption period` or `pending delete` is `lapsing`. Registration at or after the base is `reregistered_after`; registration strictly after first appearance and before the base is `reregistered_before`; an older registration is `normal`.

Only `unregistered`, `lapsing`, and `reregistered_before` produce a notice. The notice appears in JSON and the detailed box. It does not change verdicts, lifecycle labels, exit codes, or `--fail-on`. The flag is off by default; the default output and RDAP traffic remain unchanged. Other ecosystems are unaffected.

The measurement command uses the same classification and RDAP client for incident and lockfile scopes. It prints aggregate counts to standard output; names and row-level evidence are written only to a caller-selected CSV path. Measurements are performed outside this repository. A public-case check has one checkable package out of one evaluated package, with one `reregistered_before` result; broader counts require the external input populations and are not recorded here.

## Limits

RDAP exposes the current registration event, not historical ownership or WHOIS history. Re-registration can be legitimate, and the rule cannot identify who received mail or published a release. A later publication from the same address does not erase its first appearance. A missing timestamp, malformed legacy maintainer field, private suffix, unsupported TLD, or failed RDAP lookup cannot establish the timing signal. The fixed shared-provider list is deliberately conservative but incomplete.

No attempt is made to register a domain or receive mail. Treat a notice as a prompt for manual review, not an accusation against a maintainer.
