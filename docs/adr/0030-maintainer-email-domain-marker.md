# 0030. An optional maintainer e-mail domain notice

Date: 2026-10-20
Status: Proposed

## Context

An expired e-mail domain associated with an npm maintainer can allow an account takeover. In the public node-ipc incident, the account `atiertant` used `a.tiertant@atlantis-software.net` in earlier release metadata. Current RDAP registration starts on 2026-05-07, after that address first appeared in the npm packument on 2020-11-11 and before the first malicious publication on 2026-05-14. This is a registration timing signal, not proof of who controlled the account.

The existing npm release-history request ([ADR-0028](0028-dormant-release-marker.md)) already retrieves the full packument, and ADR-0028 recorded the node-ipc domain timing as left for later. An optional check can use its maintainer addresses without an extra registry request. RDAP adds a separate network lookup, so the check must remain opt-in.

## Decision

`scan --maintainer-domain` checks top-level npm maintainers. For each e-mail address, the earliest dated appearance in a version's `maintainers` or `_npmUser` field is compared with the current registration event from RDAP. The comparison uses the scan time as its base. An address absent from version history cannot be dated and is `unchecked` (reason `not_in_history`).

An address without a usable mail domain (`invalid_address`), a shared mail provider's host or registrable domain (`free_mail`), a private public suffix (`private_suffix`), and a TLD absent from the embedded IANA RDAP bootstrap or served there only over plain http (`no_rdap`) are `unchecked`. RDAP is queried over https only, so a network attacker cannot forge a registration date. A failed lookup is `lookup_failed`; HTTP 404 is `unregistered`. RDAP status `redemption period` or `pending delete` is `lapsing`, checked before the registration date so it applies when RDAP omits that date. A found record with neither a registration date nor a deletion status is `lookup_failed`. Registration at or after the base is `reregistered_after`; registration strictly after first appearance and before the base is `reregistered_before`; an older registration is `normal`.

Only `unregistered`, `lapsing`, and `reregistered_before` produce a notice. JSON lists the notice-bearing findings and the `unchecked` findings and counts `lookup_failed`; `normal` and `reregistered_after` findings are not listed. The detailed box shows one notice sentence per notice-bearing address. Table and CSV output are unchanged. The sentence is conditional because packument e-mails can be stale: if the maintainer still uses the address, npm password-reset mail may reach the current domain holder; it asks for the account e-mail and 2FA to be confirmed. It does not change verdicts, lifecycle labels, exit codes, or `--fail-on`. The flag is off by default; the default output and RDAP traffic remain unchanged. Other ecosystems are unaffected.

When RDAP provides a registrar, the notice names it. A fixed list of known drop-catch registrar prefixes (DropCatch, SnapNames, NameJet, Pheenix; Lauinger et al., "Game of Registrars", USENIX Security 2017, <https://www.usenix.org/conference/usenixsecurity17/technical-sessions/presentation/lauinger>) adds a separate evidence marker; it does not change the seven classifications. A drop-catch registrar shows the domain was caught at deletion, not who holds it now.

The measurement command uses the same classification and RDAP client for incident and lockfile scopes. It prints aggregate counts to standard output; names and row-level evidence are written only to a caller-selected CSV path. Measurements are performed outside this repository. A public-case check has one checkable package out of one evaluated package, with one `reregistered_before` result; broader counts require the external input populations and are not recorded here.

## Consequences

RDAP requests are spaced at least one second apart per RDAP server, time out after 10 seconds, and are retried once after 30 seconds on a transport error, HTTP 408, 429, or 5xx. A scan with the flag is therefore slower by roughly one second per distinct domain on the same server. Results are kept in memory for one run; only the measurement command can keep a disk cache, which stores found and not-found records without expiry and never stores failures. There is no `--fail-on` value for the notice.

## Limits

RDAP exposes the current registration event, not historical ownership or WHOIS history. Re-registration can be legitimate, and the rule cannot identify who received mail or published a release. A later publication from the same address does not erase its first appearance. A missing timestamp, malformed legacy maintainer field, private suffix, unsupported TLD, or failed RDAP lookup cannot establish the timing signal. The fixed shared-provider list is deliberately conservative but incomplete.

No attempt is made to register a domain or receive mail. Treat a notice as a prompt for manual review, not an accusation against a maintainer.
