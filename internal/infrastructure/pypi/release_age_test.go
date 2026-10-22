package pypi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestExistingPyPIResponsesCarryEarliestUploadWithoutAnotherRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/pypi/sample/json":
			_, _ = w.Write([]byte(`{"info":{"name":"sample"},"releases":{"1.0.0":[{"upload_time_iso_8601":"2026-10-21T12:00:00Z"},{"upload_time_iso_8601":"2026-10-20T12:00:00Z"}],"2.0.0":[]}}`))
		case "/pypi/sample/1.0.0/json":
			_, _ = w.Write([]byte(`{"info":{"name":"sample","version":"1.0.0"},"urls":[{"upload_time_iso_8601":"2026-10-21T12:00:00Z"},{"upload_time_iso_8601":"2026-10-20T12:00:00Z"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := NewClient()
	c.SetBaseURL(srv.URL)
	project, found, err := c.GetProject(context.Background(), "sample")
	if err != nil || !found {
		t.Fatalf("project: found=%v err=%v", found, err)
	}
	want := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	if got := project.ReleasePublishedAt["1.0.0"]; !got.Equal(want) {
		t.Fatalf("project time=%v, want %v", got, want)
	}
	if calls.Load() != 1 {
		t.Fatalf("project calls=%d, want 1", calls.Load())
	}
	version, found, err := c.GetVersion(context.Background(), "sample", "1.0.0")
	if err != nil || !found {
		t.Fatalf("version: found=%v err=%v", found, err)
	}
	if !version.PublishedAt.Equal(want) {
		t.Fatalf("version time=%v, want %v", version.PublishedAt, want)
	}
	if calls.Load() != 2 {
		t.Fatalf("total calls=%d, want 2", calls.Load())
	}
}
