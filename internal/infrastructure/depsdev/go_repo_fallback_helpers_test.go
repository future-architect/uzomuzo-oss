package depsdev

import (
	"net/url"
	"testing"
)

func TestFallbackGitHubOwnerRepo(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"github.com/owner/repo", "owner/repo"},
		{"github.com/owner/repo/sub/path", "owner/repo"},
		{"github.com/owner/repo/v2", "owner/repo"},
		{"github.com/owner/repo/v2/feature/x", "owner/repo"},
		{"github.com/owner/repo.git/sub", "owner/repo"},
		{"github.com/owner/repo/v2beta/sub", "owner/repo"}, // not a pure major suffix; ignored
		{"github.com/owner", ""},                           // not enough segments
		{"golang.org/x/tools", ""},                         // not github host
	}
	for _, tc := range cases {
		got := fallbackGitHubOwnerRepo(tc.in)
		if got != tc.want {
			t.Errorf("fallbackGitHubOwnerRepo(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestFallbackGoModuleCandidate(t *testing.T) {
	cases := []struct {
		in          string
		wantRaw     string
		wantEscaped string
	}{
		{"", "", ""},
		{"github.com/owner/repo", "github.com/owner/repo", url.PathEscape("github.com/owner/repo")},
		{"github.com/owner/repo/v2", "github.com/owner/repo/v2", url.PathEscape("github.com/owner/repo/v2")},
		{"github.com/owner/repo/v2/sub/path", "github.com/owner/repo/v2", url.PathEscape("github.com/owner/repo/v2")},
		{"github.com/owner/repo.git/v3/x", "github.com/owner/repo/v3", url.PathEscape("github.com/owner/repo/v3")},
		{"github.com/owner/repo/v2beta/x", "github.com/owner/repo", url.PathEscape("github.com/owner/repo")}, // v2beta not pure major, drop extra segments
		{"example.com/alpha/beta/gamma", "example.com/alpha/beta/gamma", url.PathEscape("example.com/alpha/beta/gamma")},
		{"github.com/owner", "github.com/owner", url.PathEscape("github.com/owner")}, // insufficient segments to trim
	}
	for _, tc := range cases {
		gotRaw, gotEsc := fallbackGoModuleCandidate(tc.in)
		if gotRaw != tc.wantRaw || gotEsc != tc.wantEscaped {
			t.Errorf("fallbackGoModuleCandidate(%q) = (%q, %q) want (%q, %q)", tc.in, gotRaw, gotEsc, tc.wantRaw, tc.wantEscaped)
		}
	}
}

func TestIsGoMajorVersionSuffix(t *testing.T) {
	positives := []string{"v2", "v10", "v0", "v01"}
	negatives := []string{"", "v", "v2beta", "vx", "v1.0", "V2"}
	for _, seg := range positives {
		if !isGoMajorVersionSuffix(seg) {
			t.Errorf("expected true for %q", seg)
		}
	}
	for _, seg := range negatives {
		if isGoMajorVersionSuffix(seg) {
			t.Errorf("expected false for %q", seg)
		}
	}
}
