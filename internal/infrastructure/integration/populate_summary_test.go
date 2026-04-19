package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/pypi"
)

// TestEnrichPyPISummary_OverridesForPyPIEcosystem verifies the PyPI Summary fetch
// replaces the deps.dev/GitHub-derived Repository.Summary for ecosystem=pypi
// while leaving non-pypi analyses untouched.
func TestEnrichPyPISummary_OverridesForPyPIEcosystem(t *testing.T) {
	const wantSummary = "Lightweight HTTP client built on aiohttp."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// PyPI JSON API path: /pypi/<name>/json
		if !strings.HasPrefix(r.URL.Path, "/pypi/") {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprintf(w, `{"info":{"name":"requests","summary":%q,"description":"# Long readme...\nWith many lines"}}`, wantSummary)
	}))
	defer srv.Close()

	pyClient := pypi.NewClient()
	pyClient.SetBaseURL(srv.URL)
	pyClient.SetCacheTTL(0) // disable cache for deterministic test
	svc := &IntegrationService{pypiClient: pyClient}

	pyAnalysis := &domain.Analysis{
		OriginalPURL:  "pkg:pypi/requests@2.31.0",
		EffectivePURL: "pkg:pypi/requests@2.31.0",
		Package:       &domain.Package{PURL: "pkg:pypi/requests@2.31.0", Ecosystem: "pypi"},
		Repository:    &domain.Repository{Summary: "from-deps.dev (should be overridden)"},
	}
	npmAnalysis := &domain.Analysis{
		OriginalPURL:  "pkg:npm/requests@1.0.0",
		EffectivePURL: "pkg:npm/requests@1.0.0",
		Package:       &domain.Package{PURL: "pkg:npm/requests@1.0.0", Ecosystem: "npm"},
		Repository:    &domain.Repository{Summary: "from-deps.dev (kept)"},
	}
	analyses := map[string]*domain.Analysis{
		pyAnalysis.OriginalPURL:  pyAnalysis,
		npmAnalysis.OriginalPURL: npmAnalysis,
	}

	svc.enrichPyPISummary(context.Background(), analyses)

	if pyAnalysis.Repository.Summary != wantSummary {
		t.Errorf("PyPI Summary = %q, want %q", pyAnalysis.Repository.Summary, wantSummary)
	}
	if npmAnalysis.Repository.Summary != "from-deps.dev (kept)" {
		t.Errorf("non-pypi Summary mutated unexpectedly: got %q", npmAnalysis.Repository.Summary)
	}
}

// TestEnrichPyPISummary_NoClient_NoOp ensures the enrichment is a no-op when the
// PyPI client is unset (the WithPyPIClient option was not applied). This guards
// the documented "best-effort" contract — the absence of the option must never
// crash or mutate state.
func TestEnrichPyPISummary_NoClient_NoOp(t *testing.T) {
	svc := &IntegrationService{}
	a := &domain.Analysis{
		Package:    &domain.Package{PURL: "pkg:pypi/x@1", Ecosystem: "pypi"},
		Repository: &domain.Repository{Summary: "untouched"},
	}
	svc.enrichPyPISummary(context.Background(), map[string]*domain.Analysis{"x": a})
	if a.Repository.Summary != "untouched" {
		t.Errorf("Summary mutated when pypiClient was nil: got %q", a.Repository.Summary)
	}
}

// TestEnrichPyPISummary_DedupesByPackageName ensures multiple analyses sharing
// a PyPI package name issue exactly one PyPI fetch and all receive the summary.
func TestEnrichPyPISummary_DedupesByPackageName(t *testing.T) {
	const wantSummary = "Async-friendly HTTP."
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = fmt.Fprintf(w, `{"info":{"name":"requests","summary":%q,"description":"long readme"}}`, wantSummary)
	}))
	defer srv.Close()

	pyClient := pypi.NewClient()
	pyClient.SetBaseURL(srv.URL)
	pyClient.SetCacheTTL(0)
	svc := &IntegrationService{pypiClient: pyClient}

	a1 := &domain.Analysis{
		Package:    &domain.Package{PURL: "pkg:pypi/requests@2.31.0", Ecosystem: "pypi"},
		Repository: &domain.Repository{Summary: ""},
	}
	a2 := &domain.Analysis{
		// Different version, same package name — should share one fetch.
		Package:    &domain.Package{PURL: "pkg:pypi/requests@2.30.0", Ecosystem: "pypi"},
		Repository: &domain.Repository{Summary: ""},
	}
	svc.enrichPyPISummary(context.Background(), map[string]*domain.Analysis{
		"a1": a1, "a2": a2,
	})

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected 1 PyPI fetch (deduped by name), got %d", got)
	}
	if a1.Repository.Summary != wantSummary || a2.Repository.Summary != wantSummary {
		t.Errorf("both analyses should receive summary; a1=%q a2=%q", a1.Repository.Summary, a2.Repository.Summary)
	}
}

// TestEnrichPyPISummary_EmptyServerSummary_KeepsExisting ensures a PyPI response
// with an empty "summary" field does NOT clobber the existing Repository.Summary.
func TestEnrichPyPISummary_EmptyServerSummary_KeepsExisting(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = fmt.Fprintln(w, `{"info":{"name":"empty","summary":"   ","description":"long readme"}}`)
	}))
	defer srv.Close()

	pyClient := pypi.NewClient()
	pyClient.SetBaseURL(srv.URL)
	pyClient.SetCacheTTL(0)
	svc := &IntegrationService{pypiClient: pyClient}

	a := &domain.Analysis{
		Package:    &domain.Package{PURL: "pkg:pypi/empty@1.0.0", Ecosystem: "pypi"},
		Repository: &domain.Repository{Summary: "kept-fallback"},
	}
	svc.enrichPyPISummary(context.Background(), map[string]*domain.Analysis{"k": a})

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected 1 PyPI fetch, got %d", got)
	}
	if a.Repository.Summary != "kept-fallback" {
		t.Errorf("Summary clobbered by empty PyPI summary: got %q", a.Repository.Summary)
	}
}
