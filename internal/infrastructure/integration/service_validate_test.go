package integration

import "testing"

func TestValidateRepoURLMatch(t *testing.T) {
	s := &IntegrationService{}

	tests := []struct {
		name            string
		resolvedRepoURL string
		inputGitHubURL  string
		wantMatch       bool
	}{
		{
			name:            "exact_match",
			resolvedRepoURL: "https://github.com/actions/checkout",
			inputGitHubURL:  "https://github.com/actions/checkout",
			wantMatch:       true,
		},
		{
			name:            "case_insensitive_match",
			resolvedRepoURL: "https://github.com/Actions/Checkout",
			inputGitHubURL:  "https://github.com/actions/checkout",
			wantMatch:       true,
		},
		{
			name:            "mismatch_different_owner",
			resolvedRepoURL: "https://github.com/bmeck/node-checkout",
			inputGitHubURL:  "https://github.com/actions/checkout",
			wantMatch:       false,
		},
		{
			name:            "mismatch_different_repo",
			resolvedRepoURL: "https://github.com/actions/setup-node",
			inputGitHubURL:  "https://github.com/actions/checkout",
			wantMatch:       false,
		},
		{
			name:            "empty_resolved_url_passes",
			resolvedRepoURL: "",
			inputGitHubURL:  "https://github.com/actions/checkout",
			wantMatch:       true,
		},
		{
			name:            "resolved_with_git_suffix",
			resolvedRepoURL: "https://github.com/lodash/lodash.git",
			inputGitHubURL:  "https://github.com/lodash/lodash",
			wantMatch:       true,
		},
		{
			name:            "resolved_non_github_url",
			resolvedRepoURL: "https://gitlab.com/owner/repo",
			inputGitHubURL:  "https://github.com/owner/repo",
			wantMatch:       false,
		},
		{
			name:            "input_as_owner_repo_shorthand",
			resolvedRepoURL: "https://github.com/actions/checkout",
			inputGitHubURL:  "actions/checkout",
			wantMatch:       true,
		},
		{
			name:            "both_with_trailing_slash",
			resolvedRepoURL: "https://github.com/actions/checkout/",
			inputGitHubURL:  "https://github.com/actions/checkout/",
			wantMatch:       true,
		},
		{
			name:            "completely_unrelated_package",
			resolvedRepoURL: "https://github.com/nicktomlin/checkout",
			inputGitHubURL:  "https://github.com/actions/checkout",
			wantMatch:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.validateRepoURLMatch(tt.resolvedRepoURL, tt.inputGitHubURL)
			if got != tt.wantMatch {
				t.Errorf("validateRepoURLMatch(%q, %q) = %v, want %v",
					tt.resolvedRepoURL, tt.inputGitHubURL, got, tt.wantMatch)
			}
		})
	}
}
