package hex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestFetch_APIKeyHeaderAndRetryAfter(t *testing.T) {
	t.Setenv("HEX_API_KEY", "secret")
	var calls int32
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"meta":{"links":{"GitHub":"https://github.com/a/b"}}}`))
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	got, err := c.GetRepoURL(context.Background(), "pkg")
	if err != nil || got == "" {
		t.Fatalf("GetRepoURL = %q, %v", got, err)
	}
	if gotAuth != "secret" {
		t.Errorf("Authorization = %q, want secret", gotAuth)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}
