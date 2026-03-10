package licenses

import "strings"

// LicenseMatchType indicates how an input matched SPDX data.
type LicenseMatchType int

const (
	MatchNone LicenseMatchType = iota
	MatchCanonicalExact
	MatchCanonicalCaseFold
	MatchAlias
	MatchNoAssertion
	MatchHeuristic
)

// NormalizationResult is the rich result of normalization.
type NormalizationResult struct {
	Raw           string
	CanonicalID   string
	CanonicalName string
	SPDX          bool
	Deprecated    bool
	MatchType     LicenseMatchType
}

// Normalize performs normalization using generated tables.
func Normalize(raw string) NormalizationResult {
	res := NormalizationResult{Raw: raw}
	s := strings.TrimSpace(raw)
	if s == "" {
		return res
	}
	if strings.EqualFold(s, "NOASSERTION") || strings.EqualFold(s, "NONE") {
		res.MatchType = MatchNoAssertion
		return res
	}
	if id, meta, ok := GeneratedNormalize(s); ok {
		res.CanonicalID = id
		res.CanonicalName = meta.Name
		res.Deprecated = meta.Deprecated
		res.SPDX = true
		if strings.EqualFold(id, s) {
			if id == s {
				res.MatchType = MatchCanonicalExact
			} else {
				res.MatchType = MatchCanonicalCaseFold
			}
		} else {
			res.MatchType = MatchAlias
		}
		return res
	}
	// Heuristic fallback: replace internal whitespace with '-'
	collapsed := strings.Join(strings.Fields(s), "-")
	res.CanonicalID = collapsed
	res.MatchType = MatchHeuristic
	return res
}
