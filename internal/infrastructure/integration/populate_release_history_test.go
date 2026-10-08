package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/npmjs"
)

// TestEnrichReleaseHistory covers scoped names, one request per package,
// and the analyses that must be skipped.
func TestEnrichReleaseHistory(t *testing.T) {
	t.Parallel()
	const body = `{"time":{"1.0.0":"2020-01-01T00:00:00Z","1.0.1":"2023-01-01T00:00:00Z"},"versions":{"1.0.0":{}}}`
	var mu sync.Mutex
	paths := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths[r.URL.EscapedPath()]++
		mu.Unlock()
		switch r.URL.EscapedPath() {
		case "/left-pad", "/@scope%2Fpkg":
			_, _ = w.Write([]byte(body))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	nc := npmjs.NewClient()
	nc.SetBaseURL(srv.URL)
	s := &IntegrationService{npmClient: nc}

	analyses := map[string]*domain.Analysis{
		"a": analysisFor("pkg:npm/left-pad@1.0.0", "npm"),
		"b": analysisFor("pkg:npm/left-pad@1.0.1", "npm"),
		"c": analysisFor("pkg:npm/%40scope/pkg@1.0.1", "npm"),
		"d": analysisFor("pkg:npm/missing@1.0.0", "npm"),
		"e": analysisFor("pkg:pypi/requests@2.0.0", "pypi"),
		"f": analysisFor("not a purl", "npm"),
		"g": analysisFor("pkg:npm/unversioned", "npm"),
	}
	s.enrichReleaseHistory(context.Background(), analyses)

	for _, k := range []string{"a", "b", "c"} {
		h := analyses[k].ReleaseHistory
		if h == nil || h.Registry != domain.RegistryNpm || len(h.PublishedAt) != 2 {
			t.Errorf("%s: ReleaseHistory = %+v", k, h)
		}
	}
	for _, k := range []string{"d", "e", "f", "g"} {
		if analyses[k].ReleaseHistory != nil {
			t.Errorf("%s: want nil ReleaseHistory", k)
		}
	}
	// Two versions of one package must not share maps: an edit to one copy
	// would change the other's burst result.
	analyses["a"].ReleaseHistory.PublishedAt["9.9.9"] = time.Time{}
	delete(analyses["a"].ReleaseHistory.Installable, "1.0.0")
	if _, leaked := analyses["b"].ReleaseHistory.PublishedAt["9.9.9"]; leaked {
		t.Error("two analyses share one PublishedAt map")
	}
	if _, ok := analyses["b"].ReleaseHistory.Installable["1.0.0"]; !ok {
		t.Error("two analyses share one Installable map")
	}
	if paths["/left-pad"] != 1 {
		t.Errorf("left-pad fetched %d times, want once for two versions", paths["/left-pad"])
	}
	if paths["/@scope%2Fpkg"] != 1 {
		t.Errorf("scoped package not fetched under its full name: %v", paths)
	}
	if len(paths) != 3 {
		t.Errorf("unexpected requests: %v", paths)
	}
}

// TestEnrichReleaseHistory_NoClient pins the no-op without an npm client.
func TestEnrichReleaseHistory_NoClient(t *testing.T) {
	t.Parallel()
	a := analysisFor("pkg:npm/left-pad@1.0.0", "npm")
	(&IntegrationService{}).enrichReleaseHistory(context.Background(), map[string]*domain.Analysis{"a": a})
	if a.ReleaseHistory != nil {
		t.Error("want nil without a client")
	}
}

// TestAnalyzeFromPURLs_PopulatesReleaseHistory drives the production path so
// that deleting the enrichReleaseHistory call in purl_batch.go fails a test.
func TestAnalyzeFromPURLs_PopulatesReleaseHistory(t *testing.T) {
	t.Parallel()
	const body = `{"time":{"1.0.0":"2020-01-01T00:00:00Z","1.0.1":"2023-01-01T00:00:00Z"},"versions":{"1.0.0":{}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/left-pad" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	nc := npmjs.NewClient()
	nc.SetBaseURL(srv.URL)
	// A tokenless GitHub client short-circuits the repository-state fetch without
	// any network call; a nil one would panic inside enhanceAnalysesWithGitHubBatch.
	svc := NewIntegrationService(github.NewClient(&config.Config{}), &stubDepsDevClient{}, WithNpmClient(nc))

	const p = "pkg:npm/left-pad@1.0.0"
	analyses, err := svc.AnalyzeFromPURLs(context.Background(), []string{p})
	if err != nil {
		t.Fatalf("AnalyzeFromPURLs failed: %v", err)
	}
	a := analyses[p]
	if a == nil {
		t.Fatalf("expected an analysis for %s, got %v", p, analyses)
	}
	h := a.ReleaseHistory
	if h == nil {
		t.Fatal("expected ReleaseHistory to be populated through the production path")
	}
	want := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	if len(h.PublishedAt) != 2 || !h.PublishedAt["1.0.1"].Equal(want) {
		t.Errorf("PublishedAt = %v, want 1.0.0 and 1.0.1 (%v)", h.PublishedAt, want)
	}
	if _, ok := h.Installable["1.0.0"]; !ok || len(h.Installable) != 1 {
		t.Errorf("Installable = %v, want only 1.0.0", h.Installable)
	}
}
