package npmjs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestGetPublishHistory covers a removed version, malformed time entries, a
// scoped name and a missing package.
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
	var (
		mu      sync.Mutex
		gotPath string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.EscapedPath()
		mu.Unlock()
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

	_, found, err = c.GetPublishHistory(context.Background(), "@solana/web3.js")
	mu.Lock()
	path := gotPath
	mu.Unlock()
	if err != nil || !found {
		t.Errorf("scoped: found=%v err=%v path=%s", found, err, path)
	}
	if path != "/@solana%2Fweb3.js" {
		t.Errorf("scoped path = %s, want the slash escaped", path)
	}
	if h, found, err := c.GetPublishHistory(context.Background(), "missing"); err != nil || found || h != nil {
		t.Errorf("missing: h=%v found=%v err=%v", h, found, err)
	}
	if _, found, err := c.GetPublishHistory(context.Background(), "  "); err != nil || found {
		t.Errorf("blank: found=%v err=%v", found, err)
	}
}

// TestParsePublishHistory_Versions pins that a missing or null "versions"
// leaves Installable nil (unknown), while "{}" is a known-empty set.
func TestParsePublishHistory_Versions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		wantNil bool
		wantLen int
	}{
		{name: "missing", body: `{"time":{"1.0.0":"2020-01-01T00:00:00Z"}}`, wantNil: true},
		{name: "null", body: `{"time":{"1.0.0":"2020-01-01T00:00:00Z"},"versions":null}`, wantNil: true},
		{name: "empty object", body: `{"time":{"1.0.0":"2020-01-01T00:00:00Z"},"versions":{}}`, wantLen: 0},
		{name: "one version", body: `{"time":{"1.0.0":"2020-01-01T00:00:00Z"},"versions":{"1.0.0":{}}}`, wantLen: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, err := parsePublishHistory(strings.NewReader(tt.body))
			if err != nil {
				t.Fatalf("parsePublishHistory: %v", err)
			}
			if got := h.Installable == nil; got != tt.wantNil {
				t.Fatalf("Installable nil = %v, want %v", got, tt.wantNil)
			}
			if !tt.wantNil && len(h.Installable) != tt.wantLen {
				t.Errorf("len(Installable) = %d, want %d", len(h.Installable), tt.wantLen)
			}
		})
	}
}

// TestGetPublishHistory_ServerError pins that a non-404 failure is an error.
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

// TestGetPublishHistory_LookupTimeoutSpansRetries pins that lookupTimeout bounds
// the whole lookup: a registry that never answers is retried, but the call
// returns once the single deadline passes.
func TestGetPublishHistory_LookupTimeoutSpansRetries(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.lookupTimeout = 200 * time.Millisecond
	start := time.Now()
	_, found, err := c.GetPublishHistory(context.Background(), "hang")
	if err == nil || found {
		t.Fatalf("want an error, got found=%v err=%v", found, err)
	}
	// NewClient's per-attempt limit is 3 s, so without the lookup deadline the
	// call would take at least that long before its first retry.
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("lookup took %v, want it bounded by lookupTimeout", elapsed)
	}
}

// TestGetPublishHistory_LiveProbe checks against registry.npmjs.org that a
// scoped name is accepted in its escaped form and that an unpublished version
// keeps its "time" entry, which ADR-0026 relies on.
//
// Opt-in only: set UZOMUZO_LIVE_PROBE=1 to run, so `go test ./...` stays
// hermetic.
func TestGetPublishHistory_LiveProbe(t *testing.T) {
	if os.Getenv("UZOMUZO_LIVE_PROBE") == "" {
		t.Skip("network probe — set UZOMUZO_LIVE_PROBE=1 to enable")
	}
	t.Parallel()
	c := NewPackumentClient()

	h, found, err := c.GetPublishHistory(context.Background(), "node-ipc")
	if err != nil || !found {
		t.Fatalf("node-ipc: found=%v err=%v", found, err)
	}
	if _, ok := h.PublishedAt["12.0.1"]; !ok {
		t.Error("node-ipc 12.0.1 lost its time entry")
	}
	if _, ok := h.Installable["12.0.1"]; ok {
		t.Error("node-ipc 12.0.1 is installable again")
	}

	h, found, err = c.GetPublishHistory(context.Background(), "@solana/web3.js")
	if err != nil || !found {
		t.Fatalf("@solana/web3.js: found=%v err=%v", found, err)
	}
	if _, ok := h.PublishedAt["1.95.6"]; !ok {
		t.Error("@solana/web3.js 1.95.6 has no time entry")
	}
}
