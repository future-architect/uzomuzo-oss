package depsdev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

func TestFetchDependentCount_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"dependentCount":1234,"directDependentCount":1000,"indirectDependentCount":234}`))
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5e9, // 5s
		MaxRetries: 0,
		BatchSize:  100,
	})

	resp, err := client.FetchDependentCount(context.Background(), "pkg:npm/lodash@4.17.21")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.DependentCount != 1234 {
		t.Errorf("DependentCount = %d, want 1234", resp.DependentCount)
	}
	if resp.DirectDependentCount != 1000 {
		t.Errorf("DirectDependentCount = %d, want 1000", resp.DirectDependentCount)
	}
	if resp.IndirectDependentCount != 234 {
		t.Errorf("IndirectDependentCount = %d, want 234", resp.IndirectDependentCount)
	}
}

func TestFetchDependentCount_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5e9,
		MaxRetries: 0,
		BatchSize:  100,
	})

	resp, err := client.FetchDependentCount(context.Background(), "pkg:npm/nonexistent@1.0.0")
	if err != nil {
		t.Fatalf("unexpected error on 404: %v", err)
	}
	if resp != nil {
		t.Errorf("expected nil response for 404, got %+v", resp)
	}
}

func TestFetchDependentCount_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer srv.Close()
	client := NewDepsDevClient(&config.DepsDevConfig{BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0})
	resp, err := client.FetchDependentCount(context.Background(), "pkg:npm/express@4.18.2")
	if resp != nil || err == nil {
		t.Fatalf("response=%v error=%v, want nil and error", resp, err)
	}
}

func TestFetchDependentCountBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"dependentCount":42,"directDependentCount":30,"indirectDependentCount":12}`))
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5e9,
		MaxRetries: 0,
		BatchSize:  100,
	})

	purls := []string{"pkg:npm/express@4.18.2", "pkg:maven/org.slf4j/slf4j-api@2.0.16"}
	results := client.FetchDependentCountBatch(context.Background(), purls)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, key := range purls {
		resp := results[key]
		if resp == nil {
			t.Fatalf("missing result for exact PURL %s", key)
		}
		if resp.DependentCount != 42 {
			t.Errorf("key=%s: DependentCount = %d, want 42", key, resp.DependentCount)
		}
	}
}

func TestFetchDependentCountBatch_DistinctVersions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/versions/4.18.2:dependents"):
			_, _ = w.Write([]byte(`{"dependentCount":10,"directDependentCount":6,"indirectDependentCount":4}`))
		case strings.Contains(r.URL.Path, "/versions/5.0.0:dependents"):
			_, _ = w.Write([]byte(`{"dependentCount":20,"directDependentCount":12,"indirectDependentCount":8}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	client := NewDepsDevClient(&config.DepsDevConfig{BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0, BatchSize: 100})

	results := client.FetchDependentCountBatch(context.Background(), []string{"pkg:npm/express@4.18.2", "pkg:npm/express@5.0.0"})
	want := map[string]int{"pkg:npm/express@4.18.2": 10, "pkg:npm/express@5.0.0": 20}
	if len(results) != len(want) {
		t.Fatalf("got %d results, want %d", len(results), len(want))
	}
	for key, count := range want {
		resp := results[key]
		if resp == nil {
			t.Fatalf("missing result for %s", key)
		}
		if resp.DependentCount != count {
			t.Errorf("key=%s: DependentCount = %d, want %d", key, resp.DependentCount, count)
		}
	}
}

func TestFetchDependentCount_GoSkipsRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	client := NewDepsDevClient(&config.DepsDevConfig{BaseURL: srv.URL, Timeout: 5e9})
	resp, err := client.FetchDependentCount(context.Background(), "pkg:golang/github.com/pkg/errors@v0.9.1")
	if err != nil || resp != nil || calls.Load() != 0 {
		t.Fatalf("Go dependents: response=%v error=%v calls=%d, want nil, nil, zero", resp, err, calls.Load())
	}
}

func TestFetchDependentCountBatch_Empty(t *testing.T) {
	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    "http://unused",
		Timeout:    5e9,
		MaxRetries: 0,
		BatchSize:  100,
	})

	results := client.FetchDependentCountBatch(context.Background(), nil)
	if len(results) != 0 {
		t.Errorf("expected empty map, got %d entries", len(results))
	}
}
