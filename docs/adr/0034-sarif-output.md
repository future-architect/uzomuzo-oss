# ADR-0034: SARIF output for dependency health findings

Date: 2026-10-30
Status: Accepted

## Context

The scan command needs a machine-readable report that code-scanning tools can
associate with dependency declarations. Existing formats and policy exit codes
must remain compatible.

## Decision

Add explicit `--format sarif` output using SARIF 2.1.0. Automatic format selection
continues to choose detailed or table. The driver reports the binary version.

An entry produces one result when its verdict is not `ok` or it matches the
active fail policy. The existing `--show-only` filter then limits displayed
results, without changing the policy decision over all entries.

Rule IDs describe facts, independently of the selected policy. All twelve rules
are listed in this fixed order: `uzomuzo/analysis-incomplete`,
`uzomuzo/malicious`, `uzomuzo/eol-confirmed`, `uzomuzo/eol-effective`,
`uzomuzo/eol-scheduled`, `uzomuzo/archived`, `uzomuzo/disabled`,
`uzomuzo/stalled`, `uzomuzo/legacy-safe`, `uzomuzo/review-needed`,
`uzomuzo/dormant-release`, and `uzomuzo/active`. The first applicable rule wins.
Analysis errors take precedence over facts; EOL and lifecycle rules use the
same final maintenance status as other renderers.

Each result is `error` when the entry matches `--fail-on`; otherwise `replace`
is `warning`, and `caution` or `review` is `note`. Per-entry matching uses the
existing raw lifecycle label and fact checks, including implied malicious
matching (ADR-0027). A final displayed EOL status can therefore differ from the
label used by the policy. The rule defaults are `warning`, overridden per result.

Locations come from the scanned input, with no invented line numbers:

- go.mod: the `require` declaration, even when a replacement changes the PURL.
- Workflow YAML and PURL lists: every matching declaration line, including
  repeated references. The first location is primary.
- CycloneDX SBOM files: the input artifact without a line region.
- Stdin, direct arguments, and remotely discovered Actions: no location.

Artifact URIs preserve the given path with forward slashes and a leading `./`
removed. Users should scan from the repository root with a relative input path
for code-scanning consumers.

The partial fingerprint key is `uzomuzoFinding/v1`. Its value is the hexadecimal
SHA-256 of `ruleId + "|" + versionless PURL (or URL) + "|" + artifact URI`.
PURL qualifiers and subpaths are preserved. An input that cannot be parsed as a
PURL retains its original text. A discovered Action has an empty artifact URI.

## Consequences

Changing the fail policy changes severity without changing rule IDs or
fingerprints. Upgrading a package version preserves its finding identity when
the other identity components stay the same. Multiple versions of the same
package can share a fingerprint; the report still contains one result per entry.

The output uses local structs and the standard JSON encoder, adding no dependency.
Uploading reports is outside the scan command. SBOM findings identify only the
SBOM file, not a source manifest declaration.
