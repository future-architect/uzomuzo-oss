package osv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_GetVuln(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/v1/vulns/GHSA-jp4x-w63m-7wgm":
			_, _ = w.Write([]byte(`{"id":"GHSA-jp4x-w63m-7wgm","published":"2018-04-26T17:21:53Z","summary":"hoek prototype pollution","database_specific":{"severity":"moderate"}}`))
		case "/v1/vulns/GHSA-hp3w-g68c-fv3c":
			_, _ = w.Write([]byte(`{"id":"GHSA-hp3w-g68c-fv3c","published":"2026-09-24T00:00:00Z","withdrawn":"2026-10-22T19:21:04Z"}`))
		case "/v1/vulns/GHSA-other-answer":
			_, _ = w.Write([]byte(`{"id":"GHSA-not-what-was-asked","published":"2020-01-01T00:00:00Z"}`))
		case "/v1/vulns/GHSA-odd-severity":
			_, _ = w.Write([]byte(`{"id":"GHSA-odd-severity","published":"2020-01-01T00:00:00Z","database_specific":{"severity":{"level":"high"}}}`))
		case "/v1/vulns/GHSA-server-error":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)
	c.SetCacheTTL(time.Minute)
	ctx := context.Background()

	rec, found, err := c.GetVuln(ctx, "GHSA-jp4x-w63m-7wgm")
	if err != nil || !found {
		t.Fatalf("GetVuln: found=%v err=%v", found, err)
	}
	if want := time.Date(2018, 4, 26, 17, 21, 53, 0, time.UTC); !rec.Published.Equal(want) {
		t.Errorf("Published: got %v, want %v", rec.Published, want)
	}
	if rec.Withdrawn {
		t.Errorf("Withdrawn: got true, want false")
	}
	if rec.Severity != "MODERATE" {
		t.Errorf("Severity: got %q, want MODERATE", rec.Severity)
	}

	rec, found, err = c.GetVuln(ctx, "GHSA-hp3w-g68c-fv3c")
	if err != nil || !found || !rec.Withdrawn {
		t.Fatalf("withdrawn record: found=%v withdrawn=%v err=%v", found, rec.Withdrawn, err)
	}

	if _, found, err = c.GetVuln(ctx, "GHSA-xxxx-xxxx-xxxx"); err != nil || found {
		t.Fatalf("404: found=%v err=%v, want not found and no error", found, err)
	}
	// A severity that is not a string reads as no rating, not as a failed lookup.
	if rec, found, err = c.GetVuln(ctx, "GHSA-odd-severity"); err != nil || !found || rec.Severity != "" {
		t.Fatalf("non-string severity: found=%v severity=%q err=%v, want found, empty, no error", found, rec.Severity, err)
	}
	if _, _, err = c.GetVuln(ctx, "GHSA-other-answer"); err == nil {
		t.Errorf("an answer naming another ID must be an error")
	}

	// Cached: the second lookup of a found and a 404 ID sends no request.
	before := calls.Load()
	_, _, _ = c.GetVuln(ctx, "GHSA-jp4x-w63m-7wgm")
	_, _, _ = c.GetVuln(ctx, "GHSA-xxxx-xxxx-xxxx")
	if got := calls.Load(); got != before {
		t.Errorf("cached lookups sent %d requests, want 0", got-before)
	}
	// SetCacheTTL drops the cached answers, so a later withdrawal or a record
	// published after a 404 is seen.
	c.SetCacheTTL(time.Minute)
	before = calls.Load()
	_, _, _ = c.GetVuln(ctx, "GHSA-jp4x-w63m-7wgm")
	_, _, _ = c.GetVuln(ctx, "GHSA-xxxx-xxxx-xxxx")
	if got := calls.Load() - before; got != 2 {
		t.Errorf("after SetCacheTTL: %d requests, want 2", got)
	}
	// A disabled cache asks every time.
	c.SetCacheTTL(0)
	before = calls.Load()
	_, _, _ = c.GetVuln(ctx, "GHSA-jp4x-w63m-7wgm")
	_, _, _ = c.GetVuln(ctx, "GHSA-jp4x-w63m-7wgm")
	if got := calls.Load() - before; got != 2 {
		t.Errorf("with caching off: %d requests, want 2", got)
	}
	// Not cached, even with caching on: an error is retried on the next call.
	c.SetCacheTTL(time.Minute)
	_, _, err = c.GetVuln(ctx, "GHSA-server-error")
	if err == nil {
		t.Fatalf("500: want error")
	}
	n := calls.Load()
	_, _, _ = c.GetVuln(ctx, "GHSA-server-error")
	if calls.Load() == n {
		t.Errorf("an error answer must not be cached")
	}
}

// TestClient_GetVuln_ResetDuringRequest checks that an answer to a request
// sent before SetCacheTTL is not stored after it: the reset must not be undone
// by a slow response, for a found record and for a 404 alike.
func TestClient_GetVuln_ResetDuringRequest(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, id, body string }{
		{"found", "GHSA-slow-found", `{"id":"GHSA-slow-found","published":"2018-04-26T17:21:53Z"}`},
		{"not found", "GHSA-slow-missing", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			arrived := make(chan struct{}, 4)
			release := make(chan struct{})
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					arrived <- struct{}{}
					<-release
				}
				if tc.body == "" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			c := newTestClient(t, srv.URL)
			c.SetCacheTTL(time.Minute)

			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _, _ = c.GetVuln(context.Background(), tc.id)
			}()
			<-arrived
			c.SetCacheTTL(time.Minute)
			close(release)
			<-done

			_, _, _ = c.GetVuln(context.Background(), tc.id)
			if got := calls.Load(); got != 2 {
				t.Errorf("requests: got %d, want 2 (the answer to the request sent before the reset must not be cached)", got)
			}
		})
	}
}
