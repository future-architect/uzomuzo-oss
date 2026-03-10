package depsdev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/future-architect/uzomuzo/internal/domain/config"
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
	for key, resp := range results {
		if resp.DependentCount != 42 {
			t.Errorf("key=%s: DependentCount = %d, want 42", key, resp.DependentCount)
		}
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
