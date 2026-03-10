package purl

import "strings"

// NormalizeMavenCollapsedCoordinates normalizes a Maven PURL that incorrectly embeds
// groupId:artifactId inside the name segment with an empty namespace into the canonical
// form with a slash separator (pkg:maven/<groupId>/<artifactId>@<version>[?qualifiers][#subpath]).
//
// Examples:
//
//	pkg:maven/org.slf4j:slf4j-api@1.7.36            -> pkg:maven/org.slf4j/slf4j-api@1.7.36
//	pkg:maven/org.slf4j:slf4j-api?classifier=sources -> pkg:maven/org.slf4j/slf4j-api?classifier=sources
//	pkg:maven/org.slf4j:slf4j-api%403 -> (unchanged unless single ':' present)
//
// It is tolerant of percent-encoded colons (%3A / %3a). If the PURL is already normalized
// (contains a slash separating namespace and name) or does not match the collapsed pattern,
// the original string is returned unchanged. Malformed inputs are returned unchanged.
// The function performs purely syntactic transformation and does not validate that the
// resulting coordinates exist upstream.
func NormalizeMavenCollapsedCoordinates(p string) string {
	if !strings.HasPrefix(p, "pkg:maven/") {
		return p
	}
	// Extract the segment after prefix up to first '@', '?', or '#', preserving remainder
	rest := p[len("pkg:maven/"):]
	if rest == "" {
		return p
	}
	cut := len(rest)
	for i, ch := range rest {
		if ch == '@' || ch == '?' || ch == '#' { // start of version, qualifiers, or subpath
			cut = i
			break
		}
	}
	core := rest[:cut]
	suffix := rest[cut:] // includes '@', '?', or '#', if any

	// Already has a slash => assume canonical
	if strings.Contains(core, "/") {
		return p
	}
	// Normalize percent-encoded colon variants before counting
	coreLower := strings.ToLower(core)
	if strings.Count(core, ":") == 0 && strings.Count(coreLower, "%3a") == 1 {
		core = strings.ReplaceAll(coreLower, "%3a", ":")
	}
	if strings.Count(core, ":") != 1 { // require exactly one ':'
		return p
	}
	parts := strings.SplitN(core, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return p
	}
	// Reject if groupId already looks like a slash path (should not happen without '/')
	if strings.Contains(parts[0], "/") || strings.Contains(parts[1], "/") {
		return p
	}
	// Basic heuristic: groupId should contain at least one dot (org.example)
	if !strings.Contains(parts[0], ".") {
		return p
	}
	return "pkg:maven/" + parts[0] + "/" + parts[1] + suffix
}
