package npmjs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetDeprecation_Deprecated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"versions": {
				"1.2.3": { "deprecated": "Use @scope/newpkg instead" }
			},
			"time": {}
		}`)) // test helper
	}))
	defer server.Close()
	c := NewClient()
	c.baseURL = server.URL
	info, found, err := c.GetDeprecation(context.Background(), "@scope", "oldpkg", "1.2.3")
	if err != nil || !found || info == nil {
		t.Fatalf("unexpected error or not found: %v", err)
	}
	if !info.Deprecated {
		t.Errorf("expected deprecated true")
	}
	if info.Successor != "@scope/newpkg" {
		t.Errorf("expected successor '@scope/newpkg', got '%s'", info.Successor)
	}
}

func TestGetDeprecation_SelfSuccessorSuppressed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"versions": {
				"1.0.0": { "deprecated": "Use @scope/oldpkg instead" }
			},
			"time": {}
		}`)) // test helper
	}))
	defer server.Close()
	c := NewClient()
	c.baseURL = server.URL
	info, found, err := c.GetDeprecation(context.Background(), "@scope", "oldpkg", "1.0.0")
	if err != nil || !found || info == nil {
		t.Fatalf("unexpected error or not found: %v", err)
	}
	if !info.Deprecated {
		t.Fatalf("expected deprecated true")
	}
	if info.Successor != "" { // should be suppressed
		t.Errorf("expected successor suppressed (empty), got %q", info.Successor)
	}
}

func TestGetDeprecation_Unpublished(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"versions": {
				"2.0.0": {}
			},
			"time": { "unpublished": { "name": "oldpkg" } }
		}`)) // test helper
	}))
	defer server.Close()
	c := NewClient()
	c.baseURL = server.URL
	info, found, err := c.GetDeprecation(context.Background(), "", "oldpkg", "2.0.0")
	if err != nil || !found || info == nil {
		t.Fatalf("unexpected error or not found: %v", err)
	}
	if !info.Unpublished {
		t.Errorf("expected unpublished true")
	}
}

func TestGetDeprecation_NotDeprecated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"versions": {
				"3.0.0": {}
			},
			"time": {}
		}`)) // test helper
	}))
	defer server.Close()
	c := NewClient()
	c.baseURL = server.URL
	info, found, err := c.GetDeprecation(context.Background(), "", "oldpkg", "3.0.0")
	if err != nil || !found || info == nil {
		t.Fatalf("unexpected error or not found: %v", err)
	}
	if info.Deprecated {
		t.Errorf("expected deprecated false")
	}
	if info.Unpublished {
		t.Errorf("expected unpublished false")
	}
}
