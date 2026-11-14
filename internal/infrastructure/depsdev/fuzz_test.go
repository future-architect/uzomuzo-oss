package depsdev

import (
	"regexp"
	"testing"
)

var projectKeyShape = regexp.MustCompile(`^github\.com/[a-z0-9-]+/[a-z0-9._-]+$`)

// FuzzConvertRepoURLToProjectKey fuzzes repository URL conversion for panics and
// checks that any non-empty result is a well-formed github.com/<owner>/<repo> key.
func FuzzConvertRepoURLToProjectKey(f *testing.F) {
	seeds := []string{
		"https://github.com/Owner/Repo.git",
		"git+ssh://git@github.com:Owner/Repo.git",
		"git@github.com:Owner/Repo",
		"github.com:Owner/Repo",
		"https://git+ssh://git@github.com/Owner/Repo",
		"https://git@github.com:443@evil.example/Owner/Repo",
		"https://evil.example://github.com/Owner/Repo",
		"a://b://\u023a\u023a\u023a\u023a\u023a\u023a\u023a\u023a\u023a\u023a\u023a\u023a",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		got := convertRepoURLToProjectKey(in)
		if got != "" && !projectKeyShape.MatchString(got) {
			t.Fatalf("convertRepoURLToProjectKey(%q) = %q, not a github.com/<owner>/<repo> key", in, got)
		}
	})
}

// FuzzPickStableDevAndMax checks version parsing safety and Stable's input-order
// independence when publication dates are distinct.
func FuzzPickStableDevAndMax(f *testing.F) {
	for _, seed := range [][3]string{
		{"maven", "3.20.0.Final", "r09"},
		{"npm", "1.0.0.0", "0.9.0"},
		{"pypi", "2.0b3", "1.9.0"},
		{"golang", "v1.1.0-0.20260327151000-87f8819acf6d", "v1.0.0"},
		{"", "", "-"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	f.Fuzz(func(t *testing.T, system, first, second string) {
		versions := []Version{
			v("2.0.0-rc1", "2025-01-01T00:00:00Z", true),
			v(first, "2026-01-01T00:00:00Z", false),
			v(second, "2027-01-01T00:00:00Z", false),
		}
		for i := range versions {
			versions[i].VersionKey.System = system
		}
		forward, _, _ := pickStableDevAndMax(versions, "")
		versions[0], versions[2] = versions[2], versions[0]
		reverse, _, _ := pickStableDevAndMax(versions, "")
		if forward.VersionKey.Version != reverse.VersionKey.Version {
			t.Fatalf("Stable changed with input order: %q vs %q", forward.VersionKey.Version, reverse.VersionKey.Version)
		}
	})
}
