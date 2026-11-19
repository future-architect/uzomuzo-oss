# ADR-0037: Score published CVSS v4 vectors from OSV

Date: 2026-11-15
Status: Accepted

## Context

deps.dev advisory details can return CVSS3 score zero for an advisory whose OSV record publishes a CVSS v4 vector. Treating that advisory as unknown can conservatively raise a MEDIUM advisory to a possible HIGH in lifecycle assessment. Dependency advisories also need publication and reviewed-rating facts for the age rule in ADR-0032. ADR-0036 already batches OSV by-ID requests for withdrawal confirmation.

## Decision

For a successful deps.dev detail with CVSS3 score zero, join the advisory ID to the existing bounded OSV by-ID pass. Keep withdrawal and known-HIGH age requests ahead of these fallback requests. Score the published vector unchanged, including its threat metric, with `github.com/pandatix/go-cvss/40` v0.6.4 (MIT); supplemental metrics do not change the score. The library remains in infrastructure. A malformed or ambiguous vector (including a duplicate or variant-spelled key in the record or in a severity entry), missing record, withdrawn record, or unsuccessful lookup supplies no v4 severity. Scan and diet each emit one WARN line per run with the number of analyses affected by failed or over-budget lookups. Missing vectors and OSV 404 remain unknown without that warning. No GitHub advisory API request is added.

Keep v4 score and knownness separate from `CVSS3Score`. A known v3 score takes precedence; otherwise a known v4 score drives the same numeric LOW, MEDIUM, HIGH, and CRITICAL bands, including NONE at zero. Existing v3-named output fields stay v3-only. Add `max_cvss4_score` and `max_transitive_cvss4_score`, append them after existing CSV columns, and use the highest effective band for `max_advisory_severity`. V4 maxima include only advisories without a deps.dev v3 score. Preserve this evidence during GitHub URL severity-only enrichment.

## Consequences

This amends ADR-0032's exclusion of dependency advisories with unknown CVSS3 from OSV by-ID lookup and its former no-WARN limit. It amends ADR-0036's statement that the direct pass only verifies withdrawal: the same pass now also obtains v4 fallback evidence, while its withdrawal rule and scope stay intact. Request budgets remain separate for direct and dependency passes.

A dormant package whose own v4-only advisory is below HIGH can move EOL-Effective to Stalled. If all residual advisories become known below HIGH, the residual-vulnerability override no longer applies and the existing decision tree may yield Active after a recent stable release; this permits EOL-Effective or Stalled to become Active. An aged v4 HIGH dependency advisory may move Stalled to EOL-Effective under ADR-0032's publication and reviewed-rating rule. A published `E:U` vector can stay below HIGH until the publisher updates it, so that change is not anticipated by this assessment.

## Alternatives

- Compute a base-only score: omitting `E` defaults to the worst-case `E:A` and does not preserve the publisher's selected threat state.
- Read GitHub's advisory API score: this covers GHSA only and adds token, rate-limit, and request dependencies.
- Implement a scorer in this repository: CVSS v4 requires the MacroVector lookup table and scoring algorithm, adding code and conformance maintenance that the pinned library already supplies.
