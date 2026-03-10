package packagist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetDependentCount_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/packages/monolog/monolog.json" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"package":{"name":"monolog/monolog","repository":"https://github.com/Seldaek/monolog","dependents":24851}}`))
	}))
	defer srv.Close()

	client := NewClient()
	client.baseURL = srv.URL

	count, err := client.GetDependentCount(context.Background(), "monolog", "monolog")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 24851 {
		t.Errorf("count = %d, want 24851", count)
	}
}

func TestGetDependentCount_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient()
	client.baseURL = srv.URL

	count, err := client.GetDependentCount(context.Background(), "nonexistent", "pkg")
	if err != nil {
		t.Fatalf("unexpected error on 404: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for not found, got %d", count)
	}
}

func TestGetDependentCount_EmptyVendor(t *testing.T) {
	client := NewClient()
	count, err := client.GetDependentCount(context.Background(), "", "monolog")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for empty vendor, got %d", count)
	}
}

func TestGetDependentCount_EmptyName(t *testing.T) {
	client := NewClient()
	count, err := client.GetDependentCount(context.Background(), "monolog", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for empty name, got %d", count)
	}
}

func TestGetDependentCount_ZeroDependents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"package":{"name":"tiny/pkg","dependents":0}}`))
	}))
	defer srv.Close()

	client := NewClient()
	client.baseURL = srv.URL

	count, err := client.GetDependentCount(context.Background(), "tiny", "pkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestGetDependentCount_CacheHit(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"package":{"name":"vendor/pkg","dependents":42}}`))
	}))
	defer srv.Close()

	client := NewClient()
	client.baseURL = srv.URL

	// First call
	count1, err := client.GetDependentCount(context.Background(), "vendor", "pkg")
	if err != nil {
		t.Fatalf("first call unexpected error: %v", err)
	}
	if count1 != 42 {
		t.Errorf("first call count = %d, want 42", count1)
	}

	// Second call should use cache
	count2, err := client.GetDependentCount(context.Background(), "vendor", "pkg")
	if err != nil {
		t.Fatalf("second call unexpected error: %v", err)
	}
	if count2 != 42 {
		t.Errorf("second call count = %d, want 42", count2)
	}

	if calls != 1 {
		t.Errorf("expected 1 HTTP call (cache hit), got %d", calls)
	}
}

func TestGetAbandoned_WithDependents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"package":{"name":"fzaninotto/faker","abandoned":"fakerphp/faker","dependents":5432}}`))
	}))
	defer srv.Close()

	client := NewClient()
	client.baseURL = srv.URL

	// GetAbandoned should still work
	abd, succ, err := client.GetAbandoned(context.Background(), "fzaninotto", "faker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !abd {
		t.Error("expected abandoned=true")
	}
	if succ != "fakerphp/faker" {
		t.Errorf("successor = %q, want fakerphp/faker", succ)
	}

	// GetDependentCount should use same cache entry
	count, err := client.GetDependentCount(context.Background(), "fzaninotto", "faker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 5432 {
		t.Errorf("count = %d, want 5432", count)
	}
}
