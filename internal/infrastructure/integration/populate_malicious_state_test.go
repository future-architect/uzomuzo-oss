package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/osv"
)

func TestEnrichMaliciousStateScopedNamesAndVersion(t *testing.T) {
	t.Parallel()
	rec := &osvRecorder{}
	srv := httptest.NewServer(rec.handler(func(name string) string {
		return fmt.Sprintf(`{"vulns":[{"id":"MAL-1","affected":[{"package":{"ecosystem":"npm","name":%q},"versions":["1.0.0"]}]}]}`, name)
	}))
	defer srv.Close()
	analyses := map[string]*domain.Analysis{
		"a1": analysisFor("pkg:npm/%40a/x@1.0.0", "npm"),
		"a2": analysisFor("pkg:npm/%40a/x@2.0.0", "npm"),
		"b":  analysisFor("pkg:npm/%40b/x@1.0.0", "npm"),
	}
	newAdvisoryDBService(t, srv.URL).enrichMaliciousState(context.Background(), analyses)
	if got := rec.requested(); len(got) != 2 || !slices.Contains(got, "npm/@a/x") || !slices.Contains(got, "npm/@b/x") {
		t.Fatalf("requests: %v", got)
	}
	if !analyses["a1"].Malicious() || analyses["a2"].Malicious() || !analyses["b"].Malicious() {
		t.Fatalf("states: %+v %+v %+v", analyses["a1"].MaliciousState, analyses["a2"].MaliciousState, analyses["b"].MaliciousState)
	}
}

// TestAnalyzeFromPURLs_PopulatesMaliciousState drives the production path so
// that deleting the enrichMaliciousState call in purl_batch.go fails a test.
func TestAnalyzeFromPURLs_PopulatesMaliciousState(t *testing.T) {
	t.Parallel()
	rec := &osvRecorder{}
	srv := httptest.NewServer(rec.handler(func(name string) string {
		return fmt.Sprintf(`{"vulns":[{"id":"MAL-1","affected":[{"package":{"ecosystem":"npm","name":%q},"versions":["5.6.1"]}]}]}`, name)
	}))
	defer srv.Close()

	oc := osv.NewClient()
	oc.SetBaseURL(srv.URL)
	oc.SetCacheTTL(0)
	// A tokenless GitHub client short-circuits the repository-state fetch without
	// any network call; a nil one would panic inside enhanceAnalysesWithGitHubBatch.
	svc := NewIntegrationService(github.NewClient(&config.Config{}), &stubDepsDevClient{}, WithOSVClient(oc))

	const key = "pkg:npm/chalk@5.6.1"
	analyses, err := svc.AnalyzeFromPURLs(context.Background(), []string{key})
	if err != nil {
		t.Fatalf("AnalyzeFromPURLs failed: %v", err)
	}
	a := analyses[key]
	if a == nil {
		t.Fatalf("expected an analysis for %s, got %v", key, analyses)
	}
	if !a.Malicious() {
		t.Fatalf("expected the malicious fact through the production path, got %+v", a.MaliciousState)
	}
	if a.MaliciousState.AdvisoryID != "MAL-1" {
		t.Errorf("AdvisoryID = %q, want MAL-1", a.MaliciousState.AdvisoryID)
	}
}

func TestEnrichMaliciousStateLookupFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer srv.Close()
	a := analysisFor("pkg:npm/chalk@5.6.1", "npm")
	newAdvisoryDBService(t, srv.URL).enrichMaliciousState(context.Background(), map[string]*domain.Analysis{"chalk": a})
	if a.MaliciousState == nil || a.MaliciousState.Status != domain.MaliciousStatusLookupFailed {
		t.Fatalf("state: %+v", a.MaliciousState)
	}
}

func TestCargoAnalysisSharesOSVFetch(t *testing.T) {
	t.Parallel()
	rec := &osvRecorder{}
	srv := httptest.NewServer(rec.handler(func(string) string { return `{"vulns":[]}` }))
	defer srv.Close()
	s := newAdvisoryDBService(t, srv.URL)
	s.osvClient.SetCacheTTL(10 * time.Minute)
	a := analysisFor("pkg:cargo/x@1.0.0", "cargo")
	analyses := map[string]*domain.Analysis{"pkg:cargo/x@1.0.0": a}
	s.enrichAdvisoryDBState(context.Background(), analyses)
	s.enrichMaliciousState(context.Background(), analyses)
	if got := rec.requested(); len(got) != 1 || got[0] != "crates.io/x" {
		t.Fatalf("OSV requests = %v, want one crates.io/x request", got)
	}
	if a.AdvisoryDBState == nil || a.MaliciousState == nil || a.MaliciousState.Status != domain.MaliciousStatusClean {
		t.Fatalf("states: advisory=%+v malicious=%+v", a.AdvisoryDBState, a.MaliciousState)
	}
}
