package npmjs

import "testing"

func TestExtractNpmSuccessor(t *testing.T) {
	tests := []struct {
		msg  string
		want string
	}{
		{"Use @scope/newpkg instead", "@scope/newpkg"},
		{"moved to pkg2", "pkg2"},
		{"Replaced by lodash", "lodash"},
		{"use newpkg.", "newpkg"},
		{"use newpkg, please", "newpkg"},
		{"Moved to @foo/bar!", "@foo/bar"},
		// Self reference patterns (we cannot filter without package name context here, but ensure extraction shape)
		{"Package is deprecated, use pkg instead", "pkg"},
		{"This package is deprecated: use @scope/pkg instead", "@scope/pkg"},
		// Negative/unsupported phrases
		{"Please migrate to newpkg", ""},
		{"No successor mentioned here", ""},
	}

	for _, tt := range tests {
		got := extractNpmSuccessor(tt.msg)
		if got != tt.want {
			t.Errorf("extractNpmSuccessor(%q) = %q; want %q", tt.msg, got, tt.want)
		}
	}
}
