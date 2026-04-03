package actionscan

import (
	"context"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/ghaworkflow"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
)

func TestNewDiscoveryService_NilClient(t *testing.T) {
	_, err := NewDiscoveryService(nil, 5)
	if err == nil {
		t.Fatal("expected error for nil github client")
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
	svc, err := NewDiscoveryService(client, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	directURLs, transitiveURLs, errs, err := svc.DiscoverActions(context.Background(), []string{"not-a-url", "https://gitlab.com/foo/bar"})
	if err != nil {
		t.Fatalf("DiscoverActions should not return fatal error: %v", err)
	}
	if len(directURLs) != 0 {
		t.Errorf("expected 0 direct URLs, got %d", len(directURLs))
	}
	if len(transitiveURLs) != 0 {
		t.Errorf("expected 0 transitive URLs, got %d", len(transitiveURLs))
	}
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got %d", len(errs))
	}
}

func TestBuildActionRefFromGitHubURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantPath  string
		wantErr   bool
	}{
		{
			name:      "simple repo URL",
			url:       "https://github.com/actions/checkout",
			wantOwner: "actions",
			wantRepo:  "checkout",
		},
		{
			name:      "subdirectory action",
			url:       "https://github.com/actions/cache/save",
			wantOwner: "actions",
			wantRepo:  "cache",
			wantPath:  "save",
		},
		{
			name:      "deep subdirectory",
			url:       "https://github.com/owner/repo/path/to/action",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantPath:  "path/to/action",
		},
		{
			name:      "URL with query params",
			url:       "https://github.com/owner/repo?tab=readme",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:    "invalid URL",
			url:     "not-a-github-url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := buildActionRefFromGitHubURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ref.Owner != tt.wantOwner {
				t.Errorf("Owner = %q, want %q", ref.Owner, tt.wantOwner)
			}
			if ref.Repo != tt.wantRepo {
				t.Errorf("Repo = %q, want %q", ref.Repo, tt.wantRepo)
			}
			if ref.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", ref.Path, tt.wantPath)
			}
		})
	}
}

func TestActionRefKey(t *testing.T) {
	tests := []struct {
		name string
		ref  ghaworkflow.ActionRef
		want string
	}{
		{
			name: "root action",
			ref:  ghaworkflow.ActionRef{Owner: "actions", Repo: "checkout"},
			want: "actions/checkout",
		},
		{
			name: "subdirectory action",
			ref:  ghaworkflow.ActionRef{Owner: "actions", Repo: "cache", Path: "save"},
			want: "actions/cache/save",
		},
		{
			name: "deep subdirectory",
			ref:  ghaworkflow.ActionRef{Owner: "owner", Repo: "repo", Path: "path/to/action"},
			want: "owner/repo/path/to/action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := actionRefKey(tt.ref)
			if got != tt.want {
				t.Errorf("actionRefKey() = %q, want %q", got, tt.want)
			}
		})
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
	svc, err := NewDiscoveryService(client, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	directURLs, transitiveURLs, errs, err := svc.DiscoverActions(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(directURLs) != 0 {
		t.Errorf("expected 0 direct URLs, got %d", len(directURLs))
	}
	if len(transitiveURLs) != 0 {
		t.Errorf("expected 0 transitive URLs, got %d", len(transitiveURLs))
	}
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d", len(errs))
	}
}
