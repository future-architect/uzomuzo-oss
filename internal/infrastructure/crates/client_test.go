package crates

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetVersion_Yanked(t *testing.T) {
	t.Parallel()
	var capturedPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath.Store(r.URL.Path)
		_, _ = fmt.Fprintln(w, `{"version":{"crate":"openssl","num":"0.10.45","yanked":true}}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(0)

	info, found, err := c.GetVersion(context.Background(), "openssl", "0.10.45")
	if err != nil {
		t.Fatalf("GetVersion failed: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if info == nil {
		t.Fatalf("expected non-nil info")
	}
	if !info.Yanked {
		t.Errorf("expected Yanked=true, got false")
	}
	if got := capturedPath.Load(); got != "/api/v1/crates/openssl/0.10.45" {
		t.Errorf("unexpected request path: got %q, want /api/v1/crates/openssl/0.10.45", got)
	}
}

func TestGetVersion_NotYanked(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, `{"version":{"crate":"serde","num":"1.0.197","yanked":false}}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(0)

	info, found, err := c.GetVersion(context.Background(), "serde", "1.0.197")
	if err != nil {
		t.Fatalf("GetVersion failed: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if info.Yanked {
		t.Errorf("expected Yanked=false, got true")
	}
}

func TestGetVersion_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(0)

	info, found, err := c.GetVersion(context.Background(), "nonexistent", "0.0.0")
	if err != nil {
		t.Fatalf("expected nil error on 404, got %v", err)
	}
	if found {
		t.Errorf("expected found=false on 404")
	}
	if info != nil {
		t.Errorf("expected nil info on 404, got %+v", info)
	}
}

func TestGetVersion_EmptyArgs(t *testing.T) {
	t.Parallel()
	c := NewClient()
	c.SetBaseURL("http://invalid.example.invalid") // would fail if hit

	tests := []struct {
		name, ver string
	}{
		{"", "1.0.0"},
		{"foo", ""},
		{"", ""},
		{"   ", "1.0.0"},
	}
	for _, tc := range tests {
		info, found, err := c.GetVersion(context.Background(), tc.name, tc.ver)
		if err != nil || found || info != nil {
			t.Errorf("expected (nil,false,nil) for empty args (%q,%q); got (%+v,%v,%v)", tc.name, tc.ver, info, found, err)
		}
	}
}

func TestGetVersion_Cache(t *testing.T) {
	t.Parallel()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = fmt.Fprintln(w, `{"version":{"crate":"tokio","num":"1.0.0","yanked":false}}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(5 * time.Minute)

	ctx := context.Background()
	if _, _, err := c.GetVersion(ctx, "tokio", "1.0.0"); err != nil {
		t.Fatalf("first GetVersion failed: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected 1 hit, got %d", got)
	}

	// Second call within TTL must hit cache.
	if _, _, err := c.GetVersion(ctx, "tokio", "1.0.0"); err != nil {
		t.Fatalf("second GetVersion failed: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected cache hit (still 1 network call), got %d", got)
	}

	// Different version must miss cache.
	if _, _, err := c.GetVersion(ctx, "tokio", "1.1.0"); err != nil {
		t.Fatalf("third GetVersion failed: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("expected cache miss for new version (2 calls), got %d", got)
	}
}

func TestGetVersion_UserAgent(t *testing.T) {
	t.Parallel()
	var captured atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.Store(r.Header.Get("User-Agent"))
		_, _ = fmt.Fprintln(w, `{"version":{"crate":"x","num":"1.0.0","yanked":false}}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(0)

	if _, _, err := c.GetVersion(context.Background(), "x", "1.0.0"); err != nil {
		t.Fatalf("GetVersion failed: %v", err)
	}
	got, _ := captured.Load().(string)
	if got != cratesUserAgent {
		t.Errorf("unexpected User-Agent: got %q, want %q", got, cratesUserAgent)
	}
}

func TestGetVersion_HTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(0)

	_, found, err := c.GetVersion(context.Background(), "any", "1.0.0")
	if err == nil {
		t.Errorf("expected error on persistent 502, got nil")
	}
	if found {
		t.Errorf("expected found=false on error")
	}
}
