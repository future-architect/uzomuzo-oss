package common

import (
	"testing"
)

func TestNormalizeRepositoryURL(t *testing.T) {
	tests := []struct {
		name     string
		rawURL   string
		expected string
	}{
		{
			name:     "git_plus_https_url",
			rawURL:   "git+https://github.com/owner/repo.git",
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "git_protocol_url",
			rawURL:   "git://github.com/owner/repo.git",
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "https_url_with_git_suffix",
			rawURL:   "https://github.com/owner/repo.git",
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "https_url_without_git_suffix",
			rawURL:   "https://github.com/owner/repo",
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "https_url_with_trailing_slash",
			rawURL:   "https://github.com/owner/repo/",
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "https_url_with_git_suffix_and_trailing_slash",
			rawURL:   "https://github.com/owner/repo.git/",
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "empty_url",
			rawURL:   "",
			expected: "",
		},
		{
			name:     "complex_git_plus_https_url",
			rawURL:   "git+https://github.com/microsoft/TypeScript.git",
			expected: "https://github.com/microsoft/TypeScript",
		},
		{
			name:     "git_protocol_without_git_suffix",
			rawURL:   "git://github.com/owner/repo",
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "http_url_with_git_suffix",
			rawURL:   "http://github.com/owner/repo.git",
			expected: "http://github.com/owner/repo",
		},
		{
			name:     "non_github_git_url",
			rawURL:   "git+https://gitlab.com/owner/repo.git",
			expected: "https://gitlab.com/owner/repo",
		},
		{
			name:     "git_plus_ssh_github",
			rawURL:   "git+ssh://git@github.com/owner/repo",
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "ssh_github",
			rawURL:   "ssh://git@github.com/owner/repo",
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "scp_style_git_at_colon",
			rawURL:   "git@github.com:owner/repo",
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "url_with_path_parameters",
			rawURL:   "git+https://github.com/owner/repo.git/tree/main",
			expected: "https://github.com/owner/repo.git/tree/main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeRepositoryURL(tt.rawURL)
			if result != tt.expected {
				t.Errorf("NormalizeRepositoryURL(%q) = %q, want %q", tt.rawURL, result, tt.expected)
			}
		})
	}
}

func TestIsValidGitHubURL(t *testing.T) {
	tests := []struct {
		name     string
		urlStr   string
		expected bool
	}{
		{
			name:     "valid_https_github_url",
			urlStr:   "https://github.com/owner/repo",
			expected: true,
		},
		{
			name:     "valid_http_github_url",
			urlStr:   "http://github.com/owner/repo",
			expected: true,
		},
		{
			name:     "valid_github_com_format",
			urlStr:   "github.com/owner/repo",
			expected: true,
		},
		{
			name:     "valid_owner_repo_format",
			urlStr:   "owner/repo",
			expected: true,
		},
		{
			name:     "valid_complex_owner_repo",
			urlStr:   "microsoft/TypeScript",
			expected: true,
		},
		{
			name:     "empty_url",
			urlStr:   "",
			expected: false,
		},
		{
			name:     "invalid_url_with_spaces",
			urlStr:   " invalid url ",
			expected: false,
		},
		{
			name:     "non_github_domain",
			urlStr:   "https://gitlab.com/owner/repo",
			expected: false,
		},
		{
			name:     "github_url_missing_repo",
			urlStr:   "https://github.com/owner",
			expected: false,
		},
		{
			name:     "github_url_missing_owner",
			urlStr:   "https://github.com/",
			expected: false,
		},
		{
			name:     "owner_repo_missing_repo",
			urlStr:   "owner/",
			expected: false,
		},
		{
			name:     "owner_repo_missing_owner",
			urlStr:   "/repo",
			expected: false,
		},
		{
			name:     "single_word_not_github",
			urlStr:   "repository",
			expected: false,
		},
		{
			name:     "purl_format_should_be_invalid",
			urlStr:   "pkg:npm/lodash@4.17.21",
			expected: false,
		},
		{
			name:     "url_with_at_symbol",
			urlStr:   "owner@domain/repo",
			expected: false,
		},
		{
			name:     "url_with_colon",
			urlStr:   "owner:name/repo",
			expected: false,
		},
		{
			name:     "github_url_with_path",
			urlStr:   "https://github.com/owner/repo/tree/main",
			expected: true,
		},
		{
			name:     "github_url_with_git_suffix",
			urlStr:   "https://github.com/owner/repo.git",
			expected: true,
		},
		{
			name:     "github_com_with_path",
			urlStr:   "github.com/owner/repo/issues",
			expected: true,
		},
		{
			name:     "malformed_url",
			urlStr:   "https://github.com//repo",
			expected: false,
		},
		{
			name:     "github_subdomain",
			urlStr:   "https://api.github.com/owner/repo",
			expected: true,
		},
		{
			name:     "case_insensitive_github",
			urlStr:   "https://GitHub.COM/owner/repo",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidGitHubURL(tt.urlStr)
			if result != tt.expected {
				t.Errorf("IsValidGitHubURL(%q) = %v, want %v", tt.urlStr, result, tt.expected)
			}
		})
	}
}

func TestURLUtilsEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		testFunc    func() bool
		description string
	}{
		{
			name: "normalize_handles_whitespace",
			testFunc: func() bool {
				result := NormalizeRepositoryURL("  git+https://github.com/owner/repo.git  ")
				// The function doesn't trim whitespace, and with leading/trailing spaces,
				// neither git+ prefix nor .git suffix matches, so input is unchanged
				expected := "  git+https://github.com/owner/repo.git  "
				return result == expected
			},
			description: "NormalizeRepositoryURL preserves whitespace and doesn't match prefixes/suffixes with spaces",
		},
		{
			name: "is_valid_github_url_trims_spaces",
			testFunc: func() bool {
				result := IsValidGitHubURL("  github.com/owner/repo  ")
				return result == true
			},
			description: "IsValidGitHubURL handles whitespace correctly",
		},
		{
			name: "multiple_git_prefixes",
			testFunc: func() bool {
				result := NormalizeRepositoryURL("git+git+https://github.com/owner/repo.git")
				expected := "git+git+https://github.com/owner/repo"
				return result == expected
			},
			description: "git+ prefix not removed when there are multiple (doesn't match git+https:// pattern)",
		},
		{
			name: "multiple_dot_git_suffixes",
			testFunc: func() bool {
				result := NormalizeRepositoryURL("https://github.com/owner/repo.git.git")
				expected := "https://github.com/owner/repo.git"
				return result == expected
			},
			description: "Only last .git suffix is removed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.testFunc() {
				t.Errorf("Edge case test failed: %s", tt.description)
			} else {
				t.Logf("Edge case test passed: %s", tt.description)
			}
		})
	}
}

func TestURLUtilsIntegration(t *testing.T) {
	tests := []struct {
		name            string
		inputURL        string
		shouldNormalize bool
		shouldBeValid   bool
	}{
		{
			name:            "git_plus_https_workflow",
			inputURL:        "git+https://github.com/owner/repo.git",
			shouldNormalize: true,
			shouldBeValid:   false, // Raw git+ URLs are not considered valid GitHub URLs
		},
		{
			name:            "git_protocol_workflow",
			inputURL:        "git://github.com/owner/repo.git",
			shouldNormalize: true,
			shouldBeValid:   false, // Raw git:// URLs are not considered valid GitHub URLs
		},
		{
			name:            "already_normalized_url",
			inputURL:        "https://github.com/owner/repo",
			shouldNormalize: false,
			shouldBeValid:   true,
		},
		{
			name:            "non_github_url",
			inputURL:        "https://gitlab.com/owner/repo",
			shouldNormalize: false,
			shouldBeValid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test normalization
			normalized := NormalizeRepositoryURL(tt.inputURL)
			if tt.shouldNormalize {
				if normalized == tt.inputURL {
					t.Errorf("Expected URL to be normalized, but got same result: %q", normalized)
				}
			}

			// Test validation of original URL
			isValidOriginal := IsValidGitHubURL(tt.inputURL)
			if isValidOriginal != tt.shouldBeValid {
				t.Errorf("IsValidGitHubURL(%q) = %v, want %v", tt.inputURL, isValidOriginal, tt.shouldBeValid)
			}

			// Test validation of normalized URL (should be valid for GitHub URLs)
			if tt.shouldBeValid {
				isValidNormalized := IsValidGitHubURL(normalized)
				if !isValidNormalized {
					t.Errorf("Normalized URL should be valid: %q -> %q", tt.inputURL, normalized)
				}
			}
		})
	}
}

func TestExtractGitHubOwnerRepo_Accepts(t *testing.T) {
	tests := []struct {
		in        string
		wantOwner string
		wantRepo  string
	}{
		{"https://github.com/owner/repo", "owner", "repo"},
		{"http://github.com/owner/repo", "owner", "repo"},
		{"github.com/owner/repo", "owner", "repo"},
		{"owner/repo", "owner", "repo"},
		{"git@github.com:owner/repo.git", "owner", "repo"},
		{"git+ssh://git@github.com/owner/repo", "owner", "repo"},
		{"ssh://git@github.com/owner/repo", "owner", "repo"},
		{"https://github.com/owner/repo.git", "owner", "repo"},
		{"https://github.com/owner/repo/", "owner", "repo"},
		{"https://github.com/owner/repo/tree/main?x=y#z", "owner", "repo"},
	}

	for _, tt := range tests {
		gotOwner, gotRepo, err := ExtractGitHubOwnerRepo(tt.in)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.in, err)
		}
		if gotOwner != tt.wantOwner || gotRepo != tt.wantRepo {
			t.Fatalf("ExtractGitHubOwnerRepo(%q) = %s/%s; want %s/%s", tt.in, gotOwner, gotRepo, tt.wantOwner, tt.wantRepo)
		}
	}
}

func TestExtractGitHubOwnerRepo_Rejects(t *testing.T) {
	tests := []string{
		"",
		"https://gitlab.com/owner/repo",
		"https://github.com/owner",
		"pkg:npm/lodash@4.17.21",
	}
	for _, in := range tests {
		if _, _, err := ExtractGitHubOwnerRepo(in); err == nil {
			t.Fatalf("expected error for %q; got none", in)
		}
	}
}
