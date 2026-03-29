package common

import "testing"

// FuzzNormalizeRepositoryURL fuzzes repository URL normalization for panics.
func FuzzNormalizeRepositoryURL(f *testing.F) {
	seeds := []string{
		"https://github.com/owner/repo",
		"git+https://github.com/owner/repo.git",
		"git://github.com/owner/repo.git",
		"git+ssh://git@github.com/owner/repo",
		"ssh://git@github.com/owner/repo",
		"git@github.com:owner/repo",
		"git@github.com:owner/repo.git",
		"github:owner/repo",
		"github:owner/repo#branch",
		"https://github.com/owner/repo?tab=readme",
		"",
		"not-a-url",
		"http://",
		"git@:",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = NormalizeRepositoryURL(input)
	})
}

// FuzzExtractGitHubOwnerRepo fuzzes GitHub URL owner/repo extraction.
func FuzzExtractGitHubOwnerRepo(f *testing.F) {
	seeds := []string{
		"https://github.com/owner/repo",
		"http://github.com/owner/repo",
		"github.com/owner/repo",
		"owner/repo",
		"git@github.com:owner/repo.git",
		"git+ssh://git@github.com/owner/repo",
		"ssh://git@github.com/owner/repo",
		"",
		"   ",
		"pkg:golang/example.com/foo",
		"https://gitlab.com/owner/repo",
		"not-a-url",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_, _, _ = ExtractGitHubOwnerRepo(input)
	})
}

// FuzzIsValidGitHubURL fuzzes GitHub URL validation.
func FuzzIsValidGitHubURL(f *testing.F) {
	seeds := []string{
		"https://github.com/owner/repo",
		"http://github.com/owner/repo",
		"github.com/owner/repo",
		"owner/repo",
		"",
		"pkg:golang/foo",
		"https://gitlab.com/owner/repo",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = IsValidGitHubURL(input)
	})
}

// FuzzMapApacheHostedToGitHub fuzzes Apache URL to GitHub mapping.
func FuzzMapApacheHostedToGitHub(f *testing.F) {
	seeds := []string{
		"https://gitbox.apache.org/repos/asf?p=commons-lang.git;a=summary",
		"https://gitbox.apache.org/repos/asf/commons-lang.git",
		"https://git-wip-us.apache.org/repos/asf/commons-lang.git",
		"",
		"https://github.com/apache/commons-lang",
		"not-apache",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = MapApacheHostedToGitHub(input)
	})
}
