package depsdev

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchLatestRelease_MavenPrereleaseDefaultUsesPURLSystem(t *testing.T) {
	const path = "/v3alpha/systems/maven/packages/org.example:demo"
	seen := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"versions":[{"versionKey":{"version":"2.0.0-M1"},"publishedAt":"2027-01-01T00:00:00Z","isDefault":true},{"versionKey":{"version":"1.0.0"},"publishedAt":"2025-01-01T00:00:00Z"}]}`)
	}))
	t.Cleanup(srv.Close)

	info, err := newDepsDevClient(srv).fetchLatestRelease(context.Background(), "pkg:maven/org.example/demo")
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if got := <-seen; got != path {
		t.Errorf("request path = %q, want %q", got, path)
	}
	if got := info.StableVersion.VersionKey.Version; got != "1.0.0" {
		t.Errorf("Stable = %q, want 1.0.0", got)
	}
	if got := info.PreReleaseVersion.VersionKey.Version; got != "2.0.0-M1" {
		t.Errorf("Dev = %q, want 2.0.0-M1", got)
	}
}
