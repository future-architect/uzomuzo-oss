package rdap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// bootstrapServer serves an IANA-style registry at /dns.json and RDAP domain
// records under /rdap/. It counts registry requests and records the last
// If-None-Match header.
type bootstrapServer struct {
	*httptest.Server
	status      atomic.Int32
	body        atomic.Value
	requests    atomic.Int32
	ifNoneMatch atomic.Value
}

func newBootstrapServer(t *testing.T) *bootstrapServer {
	t.Helper()
	s := &bootstrapServer{}
	s.status.Store(http.StatusOK)
	s.ifNoneMatch.Store("")
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/rdap/") {
			_, _ = w.Write([]byte(`{"events":[{"eventAction":"registration","eventDate":"2020-01-01T00:00:00Z"}]}`))
			return
		}
		s.requests.Add(1)
		s.ifNoneMatch.Store(r.Header.Get("If-None-Match"))
		status := int(s.status.Load())
		w.Header().Set("Cache-Control", "max-age=86400")
		w.Header().Set("ETag", `"v2"`)
		w.WriteHeader(status)
		if status == http.StatusOK {
			body, _ := s.body.Load().(string)
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(s.Close)
	s.body.Store(s.registry("newtld"))
	return s
}

// registry returns a registry whose only service maps tld to this server.
func (s *bootstrapServer) registry(tld string) string {
	return fmt.Sprintf(`{"publication":"2026-10-20T00:00:00Z","services":[[[%q],[%q]]]}`, tld, s.URL+"/rdap/")
}

func (s *bootstrapServer) client(t *testing.T, cachePath string, now time.Time) *Client {
	t.Helper()
	c := NewClient()
	c.SetHTTPClient(s.Client())
	c.SetBootstrapURL(s.URL + "/dns.json")
	c.SetBootstrapCache(cachePath)
	c.SetRequestInterval(0)
	c.SetClock(func() time.Time { return now })
	return c
}

// TestRefreshBootstrapLive checks that a fetched registry makes a TLD absent
// from the embedded copy resolvable and is cached with its expiry and ETag.
func TestRefreshBootstrapLive(t *testing.T) {
	t.Parallel()
	if _, ok := embeddedBootstrap().bases["newtld"]; ok {
		t.Fatal("test TLD must be absent from the embedded registry")
	}
	srv := newBootstrapServer(t)
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "dns.json")
	now := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	c := srv.client(t, cachePath, now)

	info := c.RefreshBootstrap(context.Background())
	if info.Source != analysis.RDAPBootstrapLive || info.Publication != "2026-10-20T00:00:00Z" {
		t.Fatalf("info=%+v", info)
	}
	rec, err := c.Lookup(context.Background(), "example.newtld")
	if err != nil || rec.Status != analysis.DomainFound {
		t.Fatalf("lookup on fetched TLD: record=%+v err=%v", rec, err)
	}
	if got := srv.ifNoneMatch.Load(); got != "" {
		t.Errorf("first fetch sent If-None-Match %q", got)
	}
	entry, err := (&Client{bootstrapCache: cachePath}).readBootstrapCache()
	if err != nil {
		t.Fatal(err)
	}
	if entry.ETag != `"v2"` || !entry.Expires.Equal(now.Add(24*time.Hour)) {
		t.Errorf("cache entry etag=%q expires=%s", entry.ETag, entry.Expires)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != "dns.json" {
		t.Errorf("cache dir holds %v, want only dns.json (no domain records, no temp files)", files)
	}
	info2, err := os.Stat(cachePath)
	if err != nil || info2.Mode().Perm() != 0o600 {
		t.Errorf("cache file mode=%v err=%v", info2.Mode().Perm(), err)
	}
}

// TestRefreshBootstrapFallsBackToEmbedded checks every rejected fetch.
func TestRefreshBootstrapFallsBackToEmbedded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int32
		body   func(*bootstrapServer) string
	}{
		{"server error", http.StatusInternalServerError, nil},
		{"not found", http.StatusNotFound, nil},
		{"malformed", http.StatusOK, func(*bootstrapServer) string { return `{` }},
		{"empty services", http.StatusOK, func(*bootstrapServer) string { return `{"publication":"x","services":[]}` }},
		{"plain http only", http.StatusOK, func(*bootstrapServer) string {
			return `{"publication":"x","services":[[["newtld"],["http://rdap.example/"]]]}`
		}},
		{"oversize", http.StatusOK, func(s *bootstrapServer) string {
			return s.registry("newtld")[:10] + strings.Repeat(" ", rdapResponseLimit) + s.registry("newtld")[10:]
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newBootstrapServer(t)
			srv.status.Store(tt.status)
			if tt.body != nil {
				srv.body.Store(tt.body(srv))
			}
			cachePath := filepath.Join(t.TempDir(), "dns.json")
			c := srv.client(t, cachePath, time.Now())
			info := c.RefreshBootstrap(context.Background())
			if info.Source != analysis.RDAPBootstrapEmbedded || info.Publication != embeddedBootstrap().publication {
				t.Errorf("info=%+v, want embedded", info)
			}
			if _, ok := c.base["newtld"]; ok {
				t.Errorf("rejected registry was adopted")
			}
			if !strings.HasPrefix(c.base["com"], "https://") {
				t.Errorf("embedded registry not in use: com=%q", c.base["com"])
			}
			if srv.requests.Load() != 1 {
				t.Errorf("registry requests=%d, want 1 (no retry)", srv.requests.Load())
			}
			if _, err := os.Stat(cachePath); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("rejected registry was cached: %v", err)
			}
		})
	}
}

// TestRefreshBootstrapCache checks that a fresh cache needs no request and an
// expired one is revalidated with If-None-Match, reusing the body on 304.
func TestRefreshBootstrapCache(t *testing.T) {
	t.Parallel()
	srv := newBootstrapServer(t)
	cachePath := filepath.Join(t.TempDir(), "dns.json")
	start := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	if info := srv.client(t, cachePath, start).RefreshBootstrap(context.Background()); info.Source != analysis.RDAPBootstrapLive {
		t.Fatalf("seed info=%+v", info)
	}

	fresh := srv.client(t, cachePath, start.Add(time.Hour))
	if info := fresh.RefreshBootstrap(context.Background()); info.Source != analysis.RDAPBootstrapCache || srv.requests.Load() != 1 {
		t.Fatalf("fresh cache: info=%+v requests=%d, want cache and no new request", info, srv.requests.Load())
	}
	if _, ok := fresh.base["newtld"]; !ok {
		t.Errorf("cached registry not in use")
	}

	srv.status.Store(http.StatusNotModified)
	later := start.Add(48 * time.Hour)
	expired := srv.client(t, cachePath, later)
	info := expired.RefreshBootstrap(context.Background())
	if info.Source != analysis.RDAPBootstrapCache || info.Publication != "2026-10-20T00:00:00Z" || srv.requests.Load() != 2 {
		t.Fatalf("304: info=%+v requests=%d", info, srv.requests.Load())
	}
	if got := srv.ifNoneMatch.Load(); got != `"v2"` {
		t.Errorf("If-None-Match=%q, want the cached ETag", got)
	}
	if _, ok := expired.base["newtld"]; !ok {
		t.Errorf("cached registry not in use after 304")
	}
	entry, err := (&Client{bootstrapCache: cachePath}).readBootstrapCache()
	if err != nil || !entry.Expires.Equal(later.Add(24*time.Hour)) {
		t.Errorf("expiry after 304=%s err=%v, want refreshed", entry.Expires, err)
	}
}

// TestBootstrapExpiry checks Cache-Control and Expires handling.
func TestBootstrapExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	c := &Client{now: func() time.Time { return now }}
	tests := []struct {
		name   string
		header http.Header
		want   time.Time
	}{
		{"max-age", http.Header{"Cache-Control": {"public, max-age=86400"}}, now.Add(24 * time.Hour)},
		{"max-age wins over expires", http.Header{"Cache-Control": {"max-age=60"}, "Expires": {"Wed, 21 Oct 2026 10:00:00 GMT"}}, now.Add(time.Minute)},
		{"expires", http.Header{"Expires": {"Wed, 21 Oct 2026 10:00:00 GMT"}}, time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)},
		{"no-cache", http.Header{"Cache-Control": {"no-cache, max-age=86400"}}, now},
		{"bad max-age", http.Header{"Cache-Control": {"max-age=abc"}}, now},
		{"none", http.Header{}, now},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := c.expiry(tt.header); !got.Equal(tt.want) {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

// TestRefreshBootstrapIgnoresBrokenCache checks that an unreadable cache file
// leads to a fresh fetch rather than a failure.
func TestRefreshBootstrapIgnoresBrokenCache(t *testing.T) {
	t.Parallel()
	srv := newBootstrapServer(t)
	cachePath := filepath.Join(t.TempDir(), "dns.json")
	for _, content := range []string{`{`, `{"schema":99}`} {
		if err := os.WriteFile(cachePath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if info := srv.client(t, cachePath, time.Now()).RefreshBootstrap(context.Background()); info.Source != analysis.RDAPBootstrapLive {
			t.Errorf("cache %q: info=%+v, want live", content, info)
		}
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var entry bootstrapCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil || entry.Schema != bootstrapCacheSchema {
		t.Errorf("cache not rewritten: %s", data)
	}
}
