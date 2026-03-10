package purl

import "strings"

// CanonicalKey returns a canonical, case-insensitive, versionless key used for
// internal lookups, caching, and catalog matching. It removes the @version
// portion (if present) while preserving qualifiers (?...), subpath (#...), and
// fragment ordering, then lowercases the entire string.
//
// ⚠️ DO NOT use for user-facing display - use VersionlessPreserveCase instead.
//
// Use cases:
//   - Internal map keys
//   - Catalog deduplication
//   - EOL matching
//
// Examples:
//
//	pkg:maven/Com.Example/Lib@1.2.3 => pkg:maven/com.example/lib
//	pkg:npm/%40scope/Package@4.5.6?foo=bar => pkg:npm/%40scope/package?foo=bar
//	pkg:pypi/Django@5.0.0#vuln => pkg:pypi/django#vuln
//	pkg:golang/github.com/Org/Repo/v2@v2.3.4 => pkg:golang/github.com/org/repo/v2
//
// If the input is empty or whitespace only, an empty string is returned.
func CanonicalKey(p string) string { // pure utility: keep here (common layer)
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	at := strings.Index(p, "@")
	if at == -1 { // already versionless
		return strings.ToLower(p)
	}
	// Determine where version substring ends (before qualifiers or fragment)
	end := len(p)
	if q := strings.Index(p[at:], "?"); q != -1 {
		if at+q < end {
			end = at + q
		}
	}
	if h := strings.Index(p[at:], "#"); h != -1 {
		if at+h < end {
			end = at + h
		}
	}
	// Remove @version portion
	base := p[:at] + p[end:]
	return strings.ToLower(base)
}
