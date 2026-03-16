package pypi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// TestGetProject_Cache verifies that a second call within TTL hits the cache and avoids an extra HTTP request.
func TestGetProject_Cache(t *testing.T) {
	t.Parallel()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = fmt.Fprintln(w, `{"info":{"name":"sample","summary":"s","description":"d","classifiers":["Development Status :: 5 - Production/Stable"]}}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(5 * time.Minute)

	ctx := context.Background()
	_, found, err := c.GetProject(ctx, "sample")
	if err != nil {
		t.Fatalf("first GetProject failed: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("expected 1 network hit after first call, got %d", hits)
	}

	// Second call should use cache.
	_, found, err = c.GetProject(ctx, "sample")
	if err != nil {
		t.Fatalf("second GetProject failed: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true on cached fetch")
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("expected cache hit (no new network request). hits=%d", hits)
	}
}

// TestGetProject_CacheExpiry ensures that after TTL expiry, a new request is made.
func TestGetProject_CacheExpiry(t *testing.T) {
	t.Parallel()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = fmt.Fprintln(w, `{"info":{"name":"pkg","summary":"s","description":"d","classifiers":["Development Status :: 7 - Inactive"]}}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(50 * time.Millisecond)
	ctx := context.Background()

	_, found, err := c.GetProject(ctx, "pkg")
	if err != nil {
		t.Fatalf("first GetProject failed: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", hits)
	}

	// Within TTL
	_, _, err = c.GetProject(ctx, "pkg")
	if err != nil {
		t.Fatalf("second GetProject failed: %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("expected cache usage. hits=%d", hits)
	}

	// After expiry
	time.Sleep(60 * time.Millisecond)
	_, _, err = c.GetProject(ctx, "pkg")
	if err != nil {
		t.Fatalf("third GetProject failed: %v", err)
	}
	if atomic.LoadInt32(&hits) != 2 {
		t.Fatalf("expected cache expiry and second network hit. hits=%d", hits)
	}
}

// TestGetProject_Retry ensures transient 5xx errors are retried via underlying httpclient retry logic.
func TestGetProject_Retry(t *testing.T) {
	t.Parallel()
	var hits int32
	// First two attempts 500, third 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt32(&hits, 1)
		if c < 3 { // fail first two
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("error"))
			return
		}
		_, _ = fmt.Fprintln(w, `{"info":{"name":"retry","summary":"s","description":"d","classifiers":["Development Status :: 4 - Beta"]}}`)
	}))
	defer srv.Close()

	// Configure small backoff to keep test fast.
	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(0) // disable cache for clarity
	c.SetRetryConfig(httpclient.RetryConfig{MaxRetries: 2, BaseBackoff: 5 * time.Millisecond, MaxBackoff: 10 * time.Millisecond, RetryOn5xx: true, RetryOnNetworkErr: true})

	ctx := context.Background()
	proj, found, err := c.GetProject(ctx, "retry")
	if err != nil {
		t.Fatalf("GetProject should succeed after retries: %v", err)
	}
	if !found || proj == nil || proj.Name != "retry" {
		t.Fatalf("unexpected project result: found=%v proj=%v", found, proj)
	}
	if atomic.LoadInt32(&hits) != 3 {
		t.Fatalf("expected 3 attempts (2 failures + success). hits=%d", hits)
	}
}

// TestGetProject_ClassifiersParsed ensures classifiers slice is populated.
func TestGetProject_ClassifiersParsed(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, `{"info":{"name":"cls","summary":"s","description":"d","classifiers":["Development Status :: 7 - Inactive","Programming Language :: Python" ]}}`)
	}))
	defer srv.Close()
	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(0)
	proj, found, err := c.GetProject(context.Background(), "cls")
	if err != nil || !found || proj == nil {
		t.Fatalf("unexpected fetch error=%v found=%v proj=%v", err, found, proj)
	}
	if len(proj.Classifiers) != 2 {
		t.Fatalf("expected 2 classifiers, got %d: %v", len(proj.Classifiers), proj.Classifiers)
	}
	if proj.Classifiers[0] != "Development Status :: 7 - Inactive" {
		t.Fatalf("unexpected first classifier: %v", proj.Classifiers[0])
	}
}

func TestGetRepoURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		json     string
		wantURL  string
		wantFind bool
	}{
		{
			name:     "project_urls_repository",
			json:     `{"info":{"name":"flask","summary":"s","description":"d","classifiers":[],"project_urls":{"Repository":"https://github.com/pallets/flask"},"home_page":""}}`,
			wantURL:  "https://github.com/pallets/flask",
			wantFind: true,
		},
		{
			name:     "project_urls_source",
			json:     `{"info":{"name":"requests","summary":"s","description":"d","classifiers":[],"project_urls":{"Source":"https://github.com/psf/requests"},"home_page":""}}`,
			wantURL:  "https://github.com/psf/requests",
			wantFind: true,
		},
		{
			name:     "project_urls_homepage_github",
			json:     `{"info":{"name":"pkg","summary":"s","description":"d","classifiers":[],"project_urls":{"Homepage":"https://github.com/owner/repo"},"home_page":""}}`,
			wantURL:  "https://github.com/owner/repo",
			wantFind: true,
		},
		{
			name:     "home_page_fallback",
			json:     `{"info":{"name":"old","summary":"s","description":"d","classifiers":[],"project_urls":{},"home_page":"https://github.com/owner/old-repo"}}`,
			wantURL:  "https://github.com/owner/old-repo",
			wantFind: true,
		},
		{
			name:     "no_github_url",
			json:     `{"info":{"name":"nope","summary":"s","description":"d","classifiers":[],"project_urls":{"Homepage":"https://example.com"},"home_page":"https://example.com"}}`,
			wantURL:  "",
			wantFind: false,
		},
		{
			name:     "deep_path_trimmed",
			json:     `{"info":{"name":"sub","summary":"s","description":"d","classifiers":[],"project_urls":{"Source":"https://github.com/owner/mono/tree/main/packages/sub"},"home_page":""}}`,
			wantURL:  "https://github.com/owner/mono",
			wantFind: true,
		},
		{
			name:     "git_plus_prefix",
			json:     `{"info":{"name":"g","summary":"s","description":"d","classifiers":[],"project_urls":{"Repository":"git+https://github.com/owner/repo.git"},"home_page":""}}`,
			wantURL:  "https://github.com/owner/repo",
			wantFind: true,
		},
		{
			name:     "null_project_urls",
			json:     `{"info":{"name":"bare","summary":"s","description":"d","classifiers":[],"project_urls":null,"home_page":"https://github.com/owner/bare"}}`,
			wantURL:  "https://github.com/owner/bare",
			wantFind: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintln(w, tt.json)
			}))
			defer srv.Close()
			c := NewClient()
			c.SetBaseURL(srv.URL)
			c.SetCacheTTL(0)
			got, err := c.GetRepoURL(context.Background(), "pkg")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantFind && got != tt.wantURL {
				t.Errorf("want %q, got %q", tt.wantURL, got)
			}
			if !tt.wantFind && got != "" {
				t.Errorf("want empty, got %q", got)
			}
		})
	}
}

func TestExtractRepoURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		info *ProjectInfo
		want string
	}{
		{
			name: "nil_info",
			info: nil,
			want: "",
		},
		{
			name: "prefers_repository_over_homepage",
			info: &ProjectInfo{
				ProjectURLs: map[string]string{
					"Homepage":   "https://github.com/owner/home",
					"Repository": "https://github.com/owner/repo",
				},
			},
			want: "https://github.com/owner/repo",
		},
		{
			name: "case_insensitive_key",
			info: &ProjectInfo{
				ProjectURLs: map[string]string{
					"SOURCE CODE": "https://github.com/owner/repo",
				},
			},
			want: "https://github.com/owner/repo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractRepoURL(tt.info)
			if got != tt.want {
				t.Errorf("want %q, got %q", tt.want, got)
			}
		})
	}
}
