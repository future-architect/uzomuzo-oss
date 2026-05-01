package licenses

import "strings"

// NormalizeExpression returns the canonical SPDX rendering of raw, "" when raw
// cannot be fully recognized as SPDX, or "NOASSERTION" when raw is the SPDX
// NOASSERTION sentinel (case-insensitive, standalone).
//
// Contract:
//   - empty / whitespace-only        → ""
//   - "NOASSERTION" / "NONE"         → "NOASSERTION" (preserved as sentinel)
//   - parses to all-recognized leaves → canonical re-rendered expression
//   - any leaf is non-SPDX (heuristic / no match) → "" (callers must keep the
//     upstream original in ResolvedLicense.Raw — never embed it here)
//
// "all-recognized" means each leaf normalized to a canonical SPDX identifier
// OR the SPDX NOASSERTION sentinel; in both cases the renderer can emit a
// spec-compliant string. A compound containing even one heuristic / unknown
// leaf is rejected as a whole because principle #1 of issue #360 forbids
// half-normalized output ("MIT OR ProprietaryFoo" leaks a non-SPDX token).
//
// Inputs must be license expression strings, not PURLs / URLs / arbitrary
// metadata.
func NormalizeExpression(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.EqualFold(s, "NOASSERTION") || strings.EqualFold(s, "NONE") {
		return "NOASSERTION"
	}
	res := ParseExpression(s)
	if res.Root == nil {
		return ""
	}
	leaves := res.Leaves()
	if len(leaves) == 0 {
		return ""
	}
	for _, l := range leaves {
		if !leafRecognized(l) {
			return ""
		}
		// Canonicalize NOASSERTION leaves so the renderer emits the
		// canonical "NOASSERTION" literal regardless of input casing
		// (e.g. "noassertion", "NoAssertion" → "NOASSERTION").
		if l.Normalization.MatchType == MatchNoAssertion {
			l.Raw = "NOASSERTION"
		}
	}
	return res.Root.String()
}

// JoinExpressions OR-joins a slice of upstream license strings into one
// canonical SPDX expression — the dedicated entry for upstream-array shapes
// (deps.dev Version.Licenses, Version.LicenseDetails[].Spdx, Maven POM
// <licenses>). Each input is independently normalized via
// NormalizeExpression; entries that yield "" are dropped (preserving the
// successfully-recognized survivors). Order is preserved (first-seen wins on
// duplicates).
//
// Contract:
//   - nil / empty slice              → ""
//   - all entries drop               → ""
//   - every surviving entry is "NOASSERTION" → "NOASSERTION"
//   - mixed NOASSERTION + recognized → NOASSERTION dropped, survivors joined
//   - one survivor                   → that survivor verbatim
//   - 2+ survivors                   → re-parsed and re-rendered as a
//     canonical OR compound (flattens nested ORs from already-compound
//     entries; e.g. ["MIT OR Apache-2.0", "BSD-3-Clause"] →
//     "MIT OR Apache-2.0 OR BSD-3-Clause", not a parens-nested form)
//
// Asymmetry vs NormalizeExpression: a single compound input with a non-SPDX
// leaf returns "" (rejected as a whole), but an array with a mix of
// recognized and non-SPDX entries keeps the recognized ones — the caller's
// upstream presented those as independent declarations, not as one logical
// unit.
func JoinExpressions(rawInputs []string) string {
	if len(rawInputs) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(rawInputs))
	var ordered []string
	hasNonNoAssertion := false
	for _, r := range rawInputs {
		norm := NormalizeExpression(r)
		if norm == "" {
			continue
		}
		if norm != "NOASSERTION" {
			hasNonNoAssertion = true
		}
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		ordered = append(ordered, norm)
	}
	if len(ordered) == 0 {
		return ""
	}
	if !hasNonNoAssertion {
		return "NOASSERTION"
	}
	survivors := make([]string, 0, len(ordered))
	for _, e := range ordered {
		if e != "NOASSERTION" {
			survivors = append(survivors, e)
		}
	}
	if len(survivors) == 1 {
		return survivors[0]
	}
	joined := strings.Join(survivors, " "+opOR+" ")
	res := ParseExpression(joined)
	if res.Root == nil {
		return ""
	}
	return res.Root.String()
}

// leafRecognized reports whether a parsed leaf carries a renderable SPDX
// identity — either a canonical SPDX ID (post-normalization) or the SPDX
// NOASSERTION sentinel. Heuristic / unknown leaves return false so callers
// can reject expressions that would otherwise produce non-spec output.
func leafRecognized(l *ExprLicense) bool {
	if l == nil {
		return false
	}
	if l.Normalization.SPDX {
		return true
	}
	return l.Normalization.MatchType == MatchNoAssertion
}
