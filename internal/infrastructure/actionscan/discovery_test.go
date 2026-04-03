package actionscan

import (
	"context"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
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
