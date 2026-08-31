package crates

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestGetCrate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		status     int
		body       string
		crate      string
		wantFound  bool
		wantYanked bool
		wantErr    bool
	}{
		{
			name:       "all versions yanked",
			status:     http.StatusOK,
			body:       `{"crate":{"name":"normal","yanked":true},"versions":[]}`,
			crate:      "normal",
			wantFound:  true,
			wantYanked: true,
		},
		{
			name:       "some versions yanked",
			status:     http.StatusOK,
			body:       `{"crate":{"name":"serde","yanked":false},"versions":[]}`,
			crate:      "serde",
			wantFound:  true,
			wantYanked: false,
		},
		{
			name:      "unknown crate",
			status:    http.StatusNotFound,
			body:      `{"errors":[{"detail":"Not Found"}]}`,
			crate:     "no-such-crate",
			wantFound: false,
		},
		{
			name:    "rate limited is an error, not a negative answer",
			status:  http.StatusTooManyRequests,
			body:    `{}`,
			crate:   "serde",
			wantErr: true,
		},
		{
			name:    "malformed json",
			status:  http.StatusOK,
			body:    `{"crate":{"name":`,
			crate:   "serde",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = fmt.Fprintln(w, tt.body)
			}))
			defer srv.Close()

			c := NewClient()
			c.SetBaseURL(srv.URL)
			c.SetCacheTTL(0)

			info, found, err := c.GetCrate(context.Background(), tt.crate)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got info=%v found=%v", info, found)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetCrate failed: %v", err)
			}
			if found != tt.wantFound {
				t.Fatalf("found: got %v, want %v", found, tt.wantFound)
			}
			if !tt.wantFound {
				return
			}
			if info == nil {
				t.Fatalf("expected non-nil info")
			}
			if info.Yanked != tt.wantYanked {
				t.Errorf("Yanked: got %v, want %v", info.Yanked, tt.wantYanked)
			}
		})
	}
}

// TestGetCrate_RequestShape pins the crates.io-specific request: the crate
// endpoint plus an empty include parameter, which suppresses the versions array.
func TestGetCrate_RequestShape(t *testing.T) {
	t.Parallel()
	var capturedPath, capturedQuery atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath.Store(r.URL.Path)
		capturedQuery.Store(r.URL.RawQuery)
		_, _ = fmt.Fprintln(w, `{"crate":{"name":"normal","yanked":true}}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(0)

	if _, _, err := c.GetCrate(context.Background(), "normal"); err != nil {
		t.Fatalf("GetCrate failed: %v", err)
	}
	if got := capturedPath.Load(); got != "/api/v1/crates/normal" {
		t.Errorf("path: got %q, want /api/v1/crates/normal", got)
	}
	if got := capturedQuery.Load(); got != "include=" {
		t.Errorf("query: got %q, want include=", got)
	}
}

func TestGetCrate_CachesByName(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = fmt.Fprintln(w, `{"crate":{"name":"normal","yanked":true}}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)

	for _, name := range []string{"normal", "Normal"} {
		info, found, err := c.GetCrate(context.Background(), name)
		if err != nil || !found || info == nil || !info.Yanked {
			t.Fatalf("GetCrate(%q): info=%v found=%v err=%v", name, info, found, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("http calls: got %d, want 1 (case-insensitive cache key)", got)
	}
}

func TestGetCrate_EmptyName(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no HTTP request expected for an empty crate name")
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	info, found, err := c.GetCrate(context.Background(), "  ")
	if err != nil || found || info != nil {
		t.Fatalf("got info=%v found=%v err=%v, want nil/false/nil", info, found, err)
	}
}
