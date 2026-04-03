package actionscan

import (
	"context"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
)

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "standard URL",
			url:       "https://github.com/actions/checkout",
			wantOwner: "actions",
			wantRepo:  "checkout",
		},
		{
			name:      "trailing slash",
			url:       "https://github.com/actions/checkout/",
			wantOwner: "actions",
			wantRepo:  "checkout",
		},
		{
			name:      "with extra path",
			url:       "https://github.com/github/codeql-action/init",
			wantOwner: "github",
			wantRepo:  "codeql-action",
		},
		{
			name:    "not GitHub URL",
			url:     "https://gitlab.com/foo/bar",
			wantErr: true,
		},
		{
			name:    "missing repo",
			url:     "https://github.com/actions",
			wantErr: true,
		},
		{
			name:    "empty",
			url:     "",
			wantErr: true,
		},
		{
			name:    "owner only with trailing slash",
			url:     "https://github.com/actions/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseGitHubURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for URL %q", tt.url)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestDiscoverActions_InvalidURLs(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token:          "test-token",
			MaxConcurrency: 5,
		},
	}
	client := github.NewClient(cfg)
	svc := NewDiscoveryService(client)

	urls, errs, err := svc.DiscoverActions(context.Background(), []string{"not-a-url", "https://gitlab.com/foo/bar"})
	if err != nil {
		t.Fatalf("DiscoverActions should not return fatal error: %v", err)
	}
	if len(urls) != 0 {
		t.Errorf("expected 0 URLs, got %d", len(urls))
	}
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got %d", len(errs))
	}
}

func TestDiscoverActions_EmptyInput(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token:          "test-token",
			MaxConcurrency: 5,
		},
	}
	client := github.NewClient(cfg)
	svc := NewDiscoveryService(client)

	urls, errs, err := svc.DiscoverActions(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 0 {
		t.Errorf("expected 0 URLs, got %d", len(urls))
	}
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d", len(errs))
	}
}
