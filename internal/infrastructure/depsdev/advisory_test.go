package depsdev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

func TestFetchAdvisory_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3alpha/advisories/GHSA-wm7h-9275-46v2" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := AdvisoryDetail{
			AdvisoryKey: AdvisoryKey{ID: "GHSA-wm7h-9275-46v2"},
			URL:         "https://github.com/advisories/GHSA-wm7h-9275-46v2",
			Title:       "Crash in HeaderParser in dicer",
			CVSS3Score:  7.5,
			CVSS3Vector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H",
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5e9,
		MaxRetries: 0,
	})

	detail, err := client.FetchAdvisory(context.Background(), "GHSA-wm7h-9275-46v2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail == nil {
		t.Fatal("expected non-nil detail")
	}
	if detail.Title != "Crash in HeaderParser in dicer" {
		t.Errorf("title = %q, want %q", detail.Title, "Crash in HeaderParser in dicer")
	}
	if detail.CVSS3Score != 7.5 {
		t.Errorf("cvss3Score = %f, want 7.5", detail.CVSS3Score)
	}
}

func TestFetchAdvisory_404ReturnsNilNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5e9,
		MaxRetries: 0,
	})

	detail, err := client.FetchAdvisory(context.Background(), "GHSA-nonexistent")
	if err != nil {
		t.Fatalf("expected no error for 404, got: %v", err)
	}
	if detail != nil {
		t.Error("expected nil detail for 404")
	}
}

func TestFetchAdvisory_NegativeCaching(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5e9,
		MaxRetries: 0,
	})

	ctx := context.Background()

	// First call should hit the server.
	detail, err := client.FetchAdvisory(ctx, "GHSA-gone")
	if err != nil || detail != nil {
		t.Fatalf("first call: err=%v, detail=%v", err, detail)
	}

	// Second call should return from cache without hitting the server.
	detail, err = client.FetchAdvisory(ctx, "GHSA-gone")
	if err != nil || detail != nil {
		t.Fatalf("second call: err=%v, detail=%v", err, detail)
	}

	if got := callCount.Load(); got != 1 {
		t.Errorf("server hit count = %d, want 1 (negative cache miss)", got)
	}
}

func TestFetchAdvisory_PositiveCaching(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		resp := AdvisoryDetail{
			AdvisoryKey: AdvisoryKey{ID: "GHSA-cached"},
			Title:       "Cached Advisory",
			CVSS3Score:  5.0,
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5e9,
		MaxRetries: 0,
	})

	ctx := context.Background()

	// First call hits server.
	d1, err := client.FetchAdvisory(ctx, "GHSA-cached")
	if err != nil || d1 == nil {
		t.Fatalf("first call: err=%v, detail=%v", err, d1)
	}

	// Second call should use cache.
	d2, err := client.FetchAdvisory(ctx, "GHSA-cached")
	if err != nil || d2 == nil {
		t.Fatalf("second call: err=%v, detail=%v", err, d2)
	}

	if got := callCount.Load(); got != 1 {
		t.Errorf("server hit count = %d, want 1 (positive cache miss)", got)
	}
}

func TestFetchAdvisory_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5e9,
		MaxRetries: 0,
	})

	_, err := client.FetchAdvisory(context.Background(), "GHSA-err")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestFetchAdvisoriesBatch_EmptyInput(t *testing.T) {
	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    "http://unused",
		Timeout:    5e9,
		MaxRetries: 0,
	})

	results := client.FetchAdvisoriesBatch(context.Background(), nil)
	if results == nil {
		t.Fatal("expected non-nil map")
	}
	if len(results) != 0 {
		t.Errorf("expected empty map, got %d entries", len(results))
	}
}

func TestFetchAdvisoriesBatch_DeduplicatesIDs(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		resp := AdvisoryDetail{
			AdvisoryKey: AdvisoryKey{ID: "GHSA-dup"},
			Title:       "Duplicate Test",
			CVSS3Score:  3.0,
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5e9,
		MaxRetries: 0,
	})

	// Pass the same ID three times.
	results := client.FetchAdvisoriesBatch(context.Background(), []string{
		"GHSA-dup", "GHSA-dup", "GHSA-dup",
	})

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	// The server should only be called once due to dedup.
	if got := callCount.Load(); got != 1 {
		t.Errorf("server hit count = %d, want 1 (dedup should prevent extra calls)", got)
	}
}

func TestFetchAdvisoriesBatch_MixedResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3alpha/advisories/GHSA-ok":
			w.Header().Set("Content-Type", "application/json")
			resp := AdvisoryDetail{
				AdvisoryKey: AdvisoryKey{ID: "GHSA-ok"},
				Title:       "Valid Advisory",
				CVSS3Score:  9.8,
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Fatalf("failed to encode response: %v", err)
			}
		case "/v3alpha/advisories/GHSA-gone":
			http.Error(w, "not found", http.StatusNotFound)
		case "/v3alpha/advisories/GHSA-err":
			http.Error(w, "server error", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5e9,
		MaxRetries: 0,
	})

	results := client.FetchAdvisoriesBatch(context.Background(), []string{
		"GHSA-ok", "GHSA-gone", "GHSA-err",
	})

	// Only the successful one should be in results.
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if d, ok := results["GHSA-ok"]; !ok || d.CVSS3Score != 9.8 {
		t.Errorf("expected GHSA-ok with score 9.8, got %v", d)
	}
}
