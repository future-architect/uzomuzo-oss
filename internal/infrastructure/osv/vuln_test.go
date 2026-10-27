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
			_, _ = w.Write([]byte(`{"id":"GHSA-jp4x-w63m-7wgm","published":"2018-04-26T17:21:53Z","summary":"hoek prototype pollution"}`))
		case "/v1/vulns/GHSA-hp3w-g68c-fv3c":
			_, _ = w.Write([]byte(`{"id":"GHSA-hp3w-g68c-fv3c","published":"2026-09-24T00:00:00Z","withdrawn":"2026-10-22T19:21:04Z"}`))
		case "/v1/vulns/GHSA-other-answer":
			_, _ = w.Write([]byte(`{"id":"GHSA-not-what-was-asked","published":"2020-01-01T00:00:00Z"}`))
		case "/v1/vulns/GHSA-server-error":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)
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

	rec, found, err = c.GetVuln(ctx, "GHSA-hp3w-g68c-fv3c")
	if err != nil || !found || !rec.Withdrawn {
		t.Fatalf("withdrawn record: found=%v withdrawn=%v err=%v", found, rec.Withdrawn, err)
	}

	if _, found, err = c.GetVuln(ctx, "GHSA-xxxx-xxxx-xxxx"); err != nil || found {
		t.Fatalf("404: found=%v err=%v, want not found and no error", found, err)
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
	// Not cached: an error is retried on the next call.
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
