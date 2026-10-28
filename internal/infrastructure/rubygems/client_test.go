package rubygems

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetReverseDependencyCount_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/gems/rails/reverse_dependencies.json" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`["activerecord","actionpack","railties","devise","kaminari"]`))
	}))
	defer srv.Close()

	client := NewClient()
	client.baseURL = srv.URL

	count, err := client.GetReverseDependencyCount(context.Background(), "rails")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count == nil || *count != 5 {
		t.Errorf("count = %v, want 5", count)
	}
}

func TestGetReverseDependencyCount_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient()
	client.baseURL = srv.URL

	count, err := client.GetReverseDependencyCount(context.Background(), "nonexistent-gem")
	if err != nil {
		t.Fatalf("unexpected error on 404: %v", err)
	}
	if count != nil {
		t.Errorf("expected nil for not found gem, got %v", count)
	}
}

func TestGetReverseDependencyCount_EmptyName(t *testing.T) {
	client := NewClient()
	count, err := client.GetReverseDependencyCount(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != nil {
		t.Errorf("expected nil for empty name, got %v", count)
	}
}

func TestGetReverseDependencyCount_MeasuredZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`[]`)) }))
	defer srv.Close()
	client := NewClient()
	client.baseURL = srv.URL
	count, err := client.GetReverseDependencyCount(context.Background(), "tiny")
	if err != nil || count == nil || *count != 0 {
		t.Fatalf("measured zero: count=%v error=%v", count, err)
	}
}
