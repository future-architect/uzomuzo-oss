package hex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/packages/cowlib" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"meta":{"links":{"Docs":"https://ninenines.eu/docs","Github":"https://github.com/ninenines/cowlib"}},
			"retirements":{"2.7.1":{"reason":"invalid","message":"bad .app"}}}`))
	}))
	defer srv.Close()
	c := NewClient()
	c.SetBaseURL(srv.URL)
	ctx := context.Background()

	if got, err := c.GetRepoURL(ctx, "CowLib"); err != nil || got != "https://github.com/ninenines/cowlib" {
		t.Fatalf("GetRepoURL = %q, %v", got, err)
	}
	if r, err := c.GetRetirement(ctx, "cowlib", "2.7.1"); err != nil || r == nil || r.Reason != "invalid" {
		t.Fatalf("retired release: %+v, %v", r, err)
	}
	if r, err := c.GetRetirement(ctx, "cowlib", "2.21.0"); err != nil || r != nil {
		t.Fatalf("live release: %+v, %v", r, err)
	}
	if got, err := c.GetRepoURL(ctx, "missing"); err != nil || got != "" {
		t.Fatalf("404: %q, %v", got, err)
	}
}
