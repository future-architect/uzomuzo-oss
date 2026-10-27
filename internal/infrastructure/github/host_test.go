package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

type countingTransport struct{ calls int }

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Request: req, Header: make(http.Header)}, nil
}

func TestFetchRepositoryStatesFiltersByExtractableHost(t *testing.T) {
	cfg := &config.Config{GitHub: config.GetDefaultGitHub()}
	cfg.GitHub.Token = "test-token"
	transport := &countingTransport{}
	c := NewClient(cfg)
	c.httpClient = httpclient.NewClient(&http.Client{Transport: transport}, httpclient.DefaultRetryConfig())
	analyses := map[string]*domain.Analysis{
		"decoy": {RepoURL: "https://notgithub.com/o/r"},
		"scp":   {RepoURL: "git@github.com:o/r.git"},
	}
	_ = c.FetchRepositoryStates(context.Background(), analyses)
	if transport.calls == 0 {
		t.Fatal("scp GitHub URL was dropped before fetching")
	}
	before := transport.calls
	_ = c.FetchRepositoryStates(context.Background(), map[string]*domain.Analysis{
		"decoy": {RepoURL: "https://notgithub.com/o/r"},
	})
	if transport.calls != before {
		t.Fatalf("decoy host triggered %d requests", transport.calls-before)
	}
}

func TestNormalizeRepoURLRequiresGitHubHost(t *testing.T) {
	transport := &countingTransport{}
	c := &Client{httpClient: httpclient.NewClient(&http.Client{Transport: transport}, httpclient.DefaultRetryConfig())}
	tests := []struct {
		input     string
		wantCalls int
	}{
		{"https://notgithub.com/o/r", 0},
		{"https://github.com.evil.example/o/r", 0},
		{"https://github.com/o/r", 1},
	}
	for _, tc := range tests {
		before := transport.calls
		got := c.normalizeRepoURL(context.Background(), tc.input)
		if calls := transport.calls - before; calls != tc.wantCalls {
			t.Errorf("normalizeRepoURL(%q) made %d requests, want %d", tc.input, calls, tc.wantCalls)
		}
		if tc.wantCalls == 0 && got != "" {
			t.Errorf("normalizeRepoURL(%q) = %q, want empty", tc.input, got)
		}
	}
}
