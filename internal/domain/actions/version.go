// Package actions contains pure-domain logic for GitHub Actions pinned-version analysis,
// including a static catalog of known-deprecated versions and matching helpers.
//
// DDD Layer: Domain (no I/O, no external dependencies beyond the standard library).
package actions

import (
	"regexp"
	"strings"
)

// tagRefPattern matches semver-like tag refs: "v1", "v1.2", "v1.2.3", "1", "1.2", "1.2.3".
// Pre-release suffixes (e.g. "-rc1", "+build") are intentionally rejected; deprecation
// catalog entries target major versions and maintainers rarely pin pre-releases.
var tagRefPattern = regexp.MustCompile(`^v?\d+(\.\d+){0,2}$`)

// IsTagRef reports whether ref looks like a semver tag (e.g., "v4", "v3.1.0", "1.2").
// Returns false for commit SHAs, branch names ("main", "master"), empty strings,
// and any other unparseable reference.
//
// Callers use this to decide whether the deprecated-actions catalog can be
// consulted for a given pin. Unresolvable refs fall back to repo-level evaluation.
func IsTagRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	return tagRefPattern.MatchString(ref)
}

// MajorOf returns the canonical major component of a tag ref, always prefixed with "v".
// Examples: "v2" → "v2", "v2.3.1" → "v2", "3" → "v3", "3.4" → "v3".
// Returns "" for non-tag refs (branches, SHAs, empty).
func MajorOf(ref string) string {
	if !IsTagRef(ref) {
		return ""
	}
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "v")
	if i := strings.Index(ref, "."); i >= 0 {
		ref = ref[:i]
	}
	return "v" + ref
}

// MatchesMajor reports whether pin resolves to the same major as catalogMajor.
// catalogMajor must be in canonical form (e.g., "v2"). pin may be any tag form.
// Non-tag refs (branches, SHAs) always return false.
func MatchesMajor(pin, catalogMajor string) bool {
	m := MajorOf(pin)
	if m == "" {
		return false
	}
	return m == catalogMajor
}
