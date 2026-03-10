package purl

import "testing"

// TestNormalizeMavenCollapsedCoordinates validates normalization of collapsed Maven coordinates
// into canonical namespace/name form while leaving non-matching or already-normalized inputs
// unchanged. It focuses purely on syntactic transformation rules documented on the function.
func TestNormalizeMavenCollapsedCoordinates(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		expected string
	}{
		{
			name:     "already canonical with version",
			in:       "pkg:maven/org.slf4j/slf4j-api@1.7.36",
			expected: "pkg:maven/org.slf4j/slf4j-api@1.7.36",
		},
		{
			name:     "collapsed with version normalizes",
			in:       "pkg:maven/org.slf4j:slf4j-api@1.7.36",
			expected: "pkg:maven/org.slf4j/slf4j-api@1.7.36",
		},
		{
			name:     "collapsed without version with qualifier",
			in:       "pkg:maven/org.slf4j:slf4j-api?classifier=sources",
			expected: "pkg:maven/org.slf4j/slf4j-api?classifier=sources",
		},
		{
			name:     "collapsed without version with subpath",
			in:       "pkg:maven/org.slf4j:slf4j-api#javadoc",
			expected: "pkg:maven/org.slf4j/slf4j-api#javadoc",
		},
		{
			name:     "percent encoded colon (lowercase) converts and normalizes",
			in:       "pkg:maven/org.example%3Aartifact@1.0.0",
			expected: "pkg:maven/org.example/artifact@1.0.0",
		},
		{
			name:     "percent encoded colon (uppercase %3A) converts and normalizes",
			in:       "pkg:maven/org.example%3Aartifact?foo=bar",
			expected: "pkg:maven/org.example/artifact?foo=bar",
		},
		{
			name:     "groupId missing dot => unchanged (heuristic rejects)",
			in:       "pkg:maven/log4j:log4j@1.2.17",
			expected: "pkg:maven/log4j:log4j@1.2.17",
		},
		{
			name:     "multiple colons => unchanged",
			in:       "pkg:maven/org.slf4j:slf4j:api@1.7.36",
			expected: "pkg:maven/org.slf4j:slf4j:api@1.7.36",
		},
		{
			name:     "already canonical with slash and qualifiers",
			in:       "pkg:maven/org.slf4j/slf4j-api?classifier=sources",
			expected: "pkg:maven/org.slf4j/slf4j-api?classifier=sources",
		},
		{
			name:     "non maven prefix => unchanged",
			in:       "pkg:npm/lodash@4.17.21",
			expected: "pkg:npm/lodash@4.17.21",
		},
		{
			name:     "empty after prefix => unchanged",
			in:       "pkg:maven/",
			expected: "pkg:maven/",
		},
		{
			name:     "mixed case groupId preserved (no mutation besides structural)",
			in:       "pkg:maven/Org.Slf4J:slf4j-API@1.7.36",
			expected: "pkg:maven/Org.Slf4J/slf4j-API@1.7.36",
		},
	}

	for _, tc := range cases {
		c := tc // capture
		if got := NormalizeMavenCollapsedCoordinates(c.in); got != c.expected {
			// Provide diff-friendly message
			// NOTE: Using t.Errorf (not Fatal) to aggregate all failures in a single run.
			t.Errorf("%s: expected %q got %q", c.name, c.expected, got)
		}
	}
}
