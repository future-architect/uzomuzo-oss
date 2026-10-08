package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
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
	}
	s.enrichReleaseHistory(context.Background(), analyses)

	for _, k := range []string{"a", "b", "c"} {
		h := analyses[k].ReleaseHistory
		if h == nil || h.Registry != domain.RegistryNpm || len(h.PublishedAt) != 2 {
			t.Errorf("%s: ReleaseHistory = %+v", k, h)
		}
	}
	for _, k := range []string{"d", "e", "f"} {
		if analyses[k].ReleaseHistory != nil {
			t.Errorf("%s: want nil ReleaseHistory", k)
		}
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
