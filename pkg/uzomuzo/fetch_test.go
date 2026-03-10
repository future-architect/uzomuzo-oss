package uzomuzo_test

import (
	"testing"

	uzomuzo "github.com/future-architect/uzomuzo/pkg/uzomuzo"
)

func TestIsGitHubURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"https", "https://github.com/owner/repo", true},
		{"http", "http://github.com/owner/repo", true},
		{"with .git", "https://github.com/owner/repo.git", true},
		{"with path", "https://github.com/owner/repo/tree/main", true},
		{"git+https", "git+https://github.com/owner/repo.git", false},
		{"ssh", "git@github.com:owner/repo.git", false},
		{"bare host", "github.com/owner/repo", true},
		{"empty", "", false},
		{"non-github", "https://gitlab.com/owner/repo", false},
		{"no repo", "https://github.com/owner", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uzomuzo.IsGitHubURL(tt.url); got != tt.want {
				t.Errorf("IsGitHubURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"https", "https://github.com/future-architect/uzomuzo", "future-architect", "uzomuzo", false},
		{"with .git", "https://github.com/owner/repo.git", "owner", "repo", false},
		{"with trailing slash", "https://github.com/owner/repo/", "owner", "repo", false},
		{"with path", "https://github.com/owner/repo/tree/main", "owner", "repo", false},
		{"bare host", "github.com/owner/repo", "owner", "repo", false},
		{"empty", "", "", "", true},
		{"non-github", "https://gitlab.com/owner/repo", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := uzomuzo.ParseGitHubURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseGitHubURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
				return
			}
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("ParseGitHubURL(%q) = (%q, %q), want (%q, %q)", tt.url, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}
