package npmjs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetPublishHistory(t *testing.T) {
	t.Parallel()
	// Shape of https://registry.npmjs.org/node-ipc after the 2026-05 incident:
	// 12.0.1 keeps its "time" entry but is gone from "versions".
	const body = `{
	  "name": "node-ipc",
	  "time": {
	    "created": "2014-01-01T00:00:00.000Z",
	    "modified": "2026-08-24T22:17:15.000Z",
	    "12.0.0": "2024-08-12T16:28:35.811Z",
	    "12.0.1": "2026-05-14T14:25:30.311Z",
	    "bogus": 42,
	    "broken": "not-a-time"
	  },
	  "versions": {"12.0.0": {"name": "node-ipc"}}
	}`
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		switch r.URL.EscapedPath() {
		case "/node-ipc", "/@solana%2Fweb3.js":
			_, _ = w.Write([]byte(body))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := NewClient()
	c.SetBaseURL(srv.URL)

	h, found, err := c.GetPublishHistory(context.Background(), "node-ipc")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if len(h.PublishedAt) != 2 {
		t.Fatalf("PublishedAt = %v, want only the two version entries", h.PublishedAt)
	}
	want := time.Date(2026, 5, 14, 14, 25, 30, 311000000, time.UTC)
	if !h.PublishedAt["12.0.1"].Equal(want) {
		t.Errorf("12.0.1 = %v, want %v", h.PublishedAt["12.0.1"], want)
	}
	if _, ok := h.Installable["12.0.1"]; ok {
		t.Error("12.0.1 must not be installable")
	}
	if _, ok := h.Installable["12.0.0"]; !ok {
		t.Error("12.0.0 must be installable")
	}

	if _, found, err := c.GetPublishHistory(context.Background(), "@solana/web3.js"); err != nil || !found {
		t.Errorf("scoped: found=%v err=%v path=%s", found, err, gotPath)
	}
	if gotPath != "/@solana%2Fweb3.js" {
		t.Errorf("scoped path = %s, want the slash escaped", gotPath)
	}
	if h, found, err := c.GetPublishHistory(context.Background(), "missing"); err != nil || found || h != nil {
		t.Errorf("missing: h=%v found=%v err=%v", h, found, err)
	}
	if _, found, err := c.GetPublishHistory(context.Background(), "  "); err != nil || found {
		t.Errorf("blank: found=%v err=%v", found, err)
	}
}

func TestGetPublishHistory_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	c := NewClient()
	c.SetBaseURL(srv.URL)
	if _, found, err := c.GetPublishHistory(context.Background(), "x"); err == nil || found {
		t.Errorf("want error, got found=%v err=%v", found, err)
	}
}

func FuzzParsePublishHistory(f *testing.F) {
	f.Add(`{"time":{"created":"2014-01-01T00:00:00Z","1.0.0":"2020-01-01T00:00:00Z","x":1},"versions":{"1.0.0":{"name":"a"}}}`)
	f.Add(`{"time":{"unpublished":{"time":"2020-01-01T00:00:00Z"}}}`)
	f.Add(`{"versions":{"1.0.0":"not an object"}}`)
	f.Add(`[]`)
	f.Fuzz(func(t *testing.T, body string) {
		h, err := parsePublishHistory(strings.NewReader(body))
		if err != nil {
			return
		}
		for v, at := range h.PublishedAt {
			if at.IsZero() || v == "created" || v == "modified" || v == "unpublished" {
				t.Fatalf("unexpected entry %q=%v", v, at)
			}
		}
	})
}
