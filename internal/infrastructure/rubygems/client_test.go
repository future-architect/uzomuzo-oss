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
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
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
	if count != 0 {
		t.Errorf("expected 0 for not found gem, got %d", count)
	}
}

func TestGetReverseDependencyCount_EmptyName(t *testing.T) {
	client := NewClient()
	count, err := client.GetReverseDependencyCount(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for empty name, got %d", count)
	}
}

func TestGetReverseDependencyCount_EmptyArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := NewClient()
	client.baseURL = srv.URL

	count, err := client.GetReverseDependencyCount(context.Background(), "tiny-gem")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for empty array, got %d", count)
	}
}
