package analysis

import (
	"regexp" // retained for fallback heuristic removal soon
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/domain/licenses"
)

// NormalizeLicenseIdentifier normalizes a raw license string toward SPDX canonical IDs.
//
// SPDX reference:
//
//	SPDX License List (canonical IDs): https://spdx.org/licenses/
//
// Strategy:
//  1. Case-insensitive match against a curated subset of canonical SPDX IDs -> return canonical spelling.
//  2. Case-insensitive match against common aliases / long-form names -> mapped to canonical ID.
//  3. Fallback: trim, collapse internal whitespace, replace spaces with '-', return result (unless NOASSERTION).
//
// Returns (normalizedValue, isCanonicalSPDX).
func NormalizeLicenseIdentifier(raw string) (string, bool) {
	res := licenses.Normalize(raw)
	if res.SPDX {
		return res.CanonicalID, true
	}
	// Preserve previous semantics: return transformed fallback or raw canonicalization,
	// but treat NOASSERTION as non-canonical false result.
	if strings.EqualFold(raw, "NOASSERTION") {
		return "NOASSERTION", false
	}
	// Heuristic fallback replicating old behavior (collapse whitespace -> '-').
	reCollapsed := regexp.MustCompile(`\s+`)
	ws := strings.TrimSpace(reCollapsed.ReplaceAllString(raw, " "))
	if ws == "" {
		return "", false
	}
	fb := strings.ReplaceAll(ws, " ", "-")
	return fb, false
}
