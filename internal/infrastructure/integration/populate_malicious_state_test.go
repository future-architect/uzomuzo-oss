package integration

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
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

func TestEnrichMaliciousStateLookupFailed(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer srv.Close()
	a := analysisFor("pkg:npm/chalk@5.6.1", "npm")
	newAdvisoryDBService(t, srv.URL).enrichMaliciousState(context.Background(), map[string]*domain.Analysis{"chalk": a})
	if a.MaliciousState == nil || a.MaliciousState.Status != domain.MaliciousStatusLookupFailed {
		t.Fatalf("state: %+v", a.MaliciousState)
	}
	if got := strings.Count(logs.String(), "malicious check incomplete"); got != 1 {
		t.Fatalf("warning count=%d, logs=%s", got, logs.String())
	}
}
