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
		"a://b://ȺȺȺȺȺȺȺȺȺȺȺȺ",
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
