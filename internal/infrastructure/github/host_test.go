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

func TestFetchRepositoryStatesFiltersByGitHubHost(t *testing.T) {
	tests := []struct {
		name      string
		repoURL   string
		wantFetch bool
	}{
		{"https", "https://github.com/o/r", true},
		{"git plus https", "git+https://github.com/o/r.git", true},
		{"scp", "git@github.com:o/r.git", true},
		{"ssh", "ssh://git@github.com/o/r", true},
		{"port", "https://github.com:443/o/r", true},
		{"www", "https://www.github.com/o/r", true},
		{"scheme-less github", "github.com/o/r", true},
		{"scheme-less www", "www.github.com/o/r", true},
		{"scheme-less mixed case", "GitHub.COM/o/r", true},
		{"gitlab", "gitlab.com/o/r", false},
		{"golang vanity", "golang.org/x/net", false},
		{"bare owner repo", "o/r", false},
		{"https gitlab", "https://gitlab.com/o/r", false},
		{"decoy prefix", "https://notgithub.com/o/r", false},
		{"decoy suffix", "https://github.com.evil.example/o/r", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{GitHub: config.GetDefaultGitHub()}
			cfg.GitHub.Token = "test-token"
			transport := &countingTransport{}
			c := NewClient(cfg)
			c.httpClient = httpclient.NewClient(&http.Client{Transport: transport}, httpclient.DefaultRetryConfig())
			_ = c.FetchRepositoryStates(context.Background(), map[string]*domain.Analysis{
				"repo": {RepoURL: tc.repoURL},
			})
			if fetched := transport.calls > 0; fetched != tc.wantFetch {
				t.Fatalf("FetchRepositoryStates(%q) made %d requests, want fetch %t", tc.repoURL, transport.calls, tc.wantFetch)
			}
		})
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
