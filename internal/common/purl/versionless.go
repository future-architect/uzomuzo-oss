package purl

import "strings"

// VersionlessPreserveCase returns a versionless PURL while preserving original case.
// Unlike CanonicalKey, this does NOT lowercase the result.
//
// Use cases:
//   - User-facing display where original case matters
//   - Audit logs requiring exact input preservation
//   - Statistical analysis where case-sensitive grouping is needed
//
// Examples:
//
//	pkg:npm/React@18.3.1        → pkg:npm/React
//	pkg:maven/Org.Foo/Bar@1.0   → pkg:maven/Org.Foo/Bar
//	pkg:gem/Rails@7.0?key=val   → pkg:gem/Rails?key=val
func VersionlessPreserveCase(raw string) string {
	if raw == "" {
		return raw
	}

	// Separate qualifiers/fragments
	pos := strings.IndexAny(raw, "?#")
	main := raw
	suffix := ""
	if pos >= 0 {
		main = raw[:pos]
		suffix = raw[pos:]
	}

	// Parse to extract version
	parsed, err := NewParser().Parse(main)
	if err != nil {
		return raw // Return original on parse failure
	}

	version := parsed.Version()
	if version == "" {
		return raw // Already versionless
	}

	// Remove @version suffix
	if strings.HasSuffix(main, "@"+version) {
		return main[:len(main)-len(version)-1] + suffix
	}

	return raw
}
