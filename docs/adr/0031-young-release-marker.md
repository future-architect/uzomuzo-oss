# ADR-0031: Mark recently published versions

Date: 2026-10-22
Status: Accepted

## Context

An OSV malicious-package record can identify a version only after an advisory exists ([ADR-0027](0027-osv-malicious-advisories.md)). The measurement in [ADR-0028](0028-dormant-release-marker.md) found that most recorded incidents were detected within seven days, but a scan in the first days still has limited evidence. The dormant-release marker of [ADR-0026](0026-dormant-release-burst.md) and ADR-0028 covers a return after long silence, not a new version of an actively released package.

## Decision

For a caller-selected version in any ecosystem, a scan records `release_age` when its publish time is no more than `young_release_days` old (default seven), or when no usable time is available. The marker uses the same scan clock as the dormant-release marker. A future time is unknown. Prereleases follow the same rule. An old version has no marker, preserving its prior output.

The publish time comes from the npm packument's `time[version]`, then from a PyPI response already fetched during this scan, then from deps.dev's requested-version release time. The earliest file upload in a PyPI release or version response represents the version's publication. npm retains times for some removed versions; those remain eligible. No fetch is added for this marker. When no fetched PyPI response has an upload time for the version (the PyPI request fails, the project is not found, or the response does not list the version), deps.dev is the fallback; if it has no time, the state is `unknown`.

The detailed view shows one notice for a young version, with its date, integer age, and the date the window ends. It omits that notice when a dormant-release marker or malicious advisory already covers the version. JSON still includes the `release_age` object. Unknown time appears only in JSON. Table and CSV output do not change.

The marker does not affect lifecycle labels, verdicts, exit codes, `--show-only`, or `--fail-on`. A dedicated failure policy can be considered after its noise is measured; the marker alone is not a finding.

## Limits and follow-ups

deps.dev can lag a newly published version, so `unknown` does not mean old or safe. The marker records version age, not each file's upload age: a new wheel added to an old PyPI version is outside its scope. It also cannot identify who published a release or whether it is malicious. A future allow-list for first-party packages may help if measured noise warrants it.
