package goproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// helper to build a test client pointed at an httptest.Server
func newTestClient(srv *httptest.Server) *Client {
	c := NewClient()
	c.base = srv.URL // override base to test server
	return c
}

func TestLatestVersion_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/github.com/acme/repo/@latest" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Version": "v1.2.3",
				"Time":    time.Date(2025, 8, 21, 12, 0, 0, 0, time.UTC),
			})
			return
		}
		w.WriteHeader(404)
	}))
	defer ts.Close()
	c := newTestClient(ts)
	ver, ref, err := c.LatestVersion(context.Background(), "github.com/acme/repo")
	if err != nil {
		t.Fatalf("LatestVersion unexpected error: %v", err)
	}
	if ver != "v1.2.3" {
		t.Fatalf("version = %s want v1.2.3", ver)
	}
	if !strings.Contains(ref, "/@latest") {
		t.Fatalf("reference missing @latest: %s", ref)
	}
}

func TestLatestVersion_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()
	c := newTestClient(ts)
	_, _, err := c.LatestVersion(context.Background(), "github.com/acme/repo")
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestGoMod_Success(t *testing.T) {
	const module = "github.com/acme/repo"
	const version = "v0.9.0"
	gomod := "module github.com/acme/repo\n\ngo 1.22\n"
	path := "/github.com/acme/repo/@v/" + version + ".mod"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(gomod))
			return
		}
		w.WriteHeader(404)
	}))
	defer ts.Close()
	c := newTestClient(ts)
	b, ref, err := c.GoMod(context.Background(), module, version)
	if err != nil {
		te := string(b)
		_ = te
		t.Fatalf("GoMod unexpected error: %v", err)
	}
	if string(b) != gomod {
		t.Fatalf("gomod mismatch: got %q want %q", string(b), gomod)
	}
	if !strings.Contains(ref, "/@v/"+version+".mod") {
		t.Fatalf("reference mismatch: %s", ref)
	}
}

func TestGoMod_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()
	c := newTestClient(ts)
	_, _, err := c.GoMod(context.Background(), "github.com/acme/repo", "v1.0.0")
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestResolveModuleRoot_FindsRoot(t *testing.T) {
	// Simulate attempts: full path & trimmed segments 404 until root returns latest
	calls := make([]string, 0)
	root := "/github.com/acme/repo/@latest"
	// candidates in order
	cand1 := "/github.com/acme/repo/pkg/sub/@latest"
	cand2 := "/github.com/acme/repo/pkg/@latest"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case root:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"Version": "v9.9.9", "Time": time.Now()})
		default:
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()
	c := newTestClient(ts)
	mod, ver, err := c.ResolveModuleRoot(context.Background(), "github.com/acme/repo/pkg/sub")
	if err != nil {
		t.Fatalf("ResolveModuleRoot error: %v", err)
	}
	if mod != "github.com/acme/repo" || ver != "v9.9.9" {
		t.Fatalf("unexpected result mod=%s ver=%s", mod, ver)
	}
	expectedOrder := []string{cand1, cand2, root}
	if !reflect.DeepEqual(calls, expectedOrder) {
		t.Fatalf("call order mismatch\n got : %v\n want: %v", calls, expectedOrder)
	}
}

func TestResolveModuleRoot_NoModule(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()
	c := newTestClient(ts)
	_, _, err := c.ResolveModuleRoot(context.Background(), "github.com/acme/repo/pkg/sub")
	if err == nil || !strings.Contains(err.Error(), "no module") {
		t.Fatalf("expected no module error, got %v", err)
	}
}

func TestPathEscapeModule(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"simple/module", "simple/module"},
		{"example.com/with space/mod", "example.com/with%20space/mod"},
		{"github.com/Upper/Case", "github.com/Upper/Case"},
	}
	for _, cse := range cases {
		got := pathEscapeModule(cse.in)
		if got != cse.want {
			t.Fatalf("pathEscapeModule(%q)=%q want %q", cse.in, got, cse.want)
		}
	}
}

func TestNilClientMethods(t *testing.T) {
	var c *Client
	if v, _, err := c.LatestVersion(context.Background(), "github.com/acme/repo"); err == nil || v != "" {
		t.Fatalf("nil LatestVersion should error")
	}
	if b, _, err := c.GoMod(context.Background(), "github.com/acme/repo", "v1.0.0"); err == nil || b != nil {
		t.Fatalf("nil GoMod should error")
	}
	if m, _, err := c.ResolveModuleRoot(context.Background(), "github.com/acme/repo/pkg"); err == nil || m != "" {
		t.Fatalf("nil ResolveModuleRoot should error")
	}
}
