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
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/crates"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/pypi"
)

// newRegistryStateService wires an IntegrationService whose PyPI and crates.io
// clients both point at srv.
func newRegistryStateService(t *testing.T, srvURL string) *IntegrationService {
	t.Helper()
	pc := pypi.NewClient()
	pc.SetBaseURL(srvURL)
	pc.SetCacheTTL(0)
	cc := crates.NewClient()
	cc.SetBaseURL(srvURL)
	cc.SetCacheTTL(0)
	return &IntegrationService{pypiClient: pc, cratesClient: cc}
}

func analysisFor(purl, ecosystem string) *domain.Analysis {
	return &domain.Analysis{Package: &domain.Package{PURL: purl, Ecosystem: ecosystem}}
}

// registryHandler answers both registries from one server.
func registryHandler(pypiBody, cratesBody string, calls *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/crates/"):
			if cratesBody == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = fmt.Fprintln(w, cratesBody)
		case strings.HasPrefix(r.URL.Path, "/pypi/"):
			if pypiBody == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = fmt.Fprintln(w, pypiBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestEnrichRegistryState(t *testing.T) {
	t.Parallel()
	const allYankedPyPI = `{"info":{"name":"python-apt","yanked":true,"yanked_reason":"Unmaintained"}}`
	const partialPyPI = `{"info":{"name":"requests","yanked":false}}`
	const allYankedCrate = `{"crate":{"name":"normal","yanked":true}}`
	const healthyCrate = `{"crate":{"name":"serde","yanked":false}}`

	tests := []struct {
		name       string
		purl       string
		ecosystem  string
		pypiBody   string
		cratesBody string
		wantState  bool // RegistryState non-nil
		wantYanked bool
		wantReason string
		wantReg    string
	}{
		{
			name: "pypi every release yanked", purl: "pkg:pypi/python-apt", ecosystem: "pypi",
			pypiBody:  allYankedPyPI,
			wantState: true, wantYanked: true, wantReason: "Unmaintained", wantReg: "PyPI",
		},
		{
			name: "pypi with a non-yanked release still records the fact", purl: "pkg:pypi/requests", ecosystem: "pypi",
			pypiBody:  partialPyPI,
			wantState: true, wantYanked: false, wantReg: "PyPI",
		},
		{
			name: "cargo unversioned every release yanked", purl: "pkg:cargo/normal", ecosystem: "cargo",
			cratesBody: allYankedCrate,
			wantState:  true, wantYanked: true, wantReg: "crates.io",
		},
		{
			name: "cargo versioned is fetched too", purl: "pkg:cargo/normal@0.0.0", ecosystem: "cargo",
			cratesBody: allYankedCrate,
			wantState:  true, wantYanked: true, wantReg: "crates.io",
		},
		{
			name: "cargo healthy", purl: "pkg:cargo/serde@1.0.197", ecosystem: "cargo",
			cratesBody: healthyCrate,
			wantState:  true, wantYanked: false, wantReg: "crates.io",
		},
		{
			name: "package not found leaves the state unfetched", purl: "pkg:pypi/ghost", ecosystem: "pypi",
			wantState: false,
		},
		{
			name: "unsupported ecosystem is skipped", purl: "pkg:npm/left-pad", ecosystem: "npm",
			wantState: false,
		},
		{
			name: "namespaced purl is skipped", purl: "pkg:pypi/acme/python-apt", ecosystem: "pypi",
			pypiBody:  allYankedPyPI,
			wantState: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(registryHandler(tt.pypiBody, tt.cratesBody, nil))
			defer srv.Close()

			s := newRegistryStateService(t, srv.URL)
			a := analysisFor(tt.purl, tt.ecosystem)
			s.enrichRegistryState(context.Background(), map[string]*domain.Analysis{"k": a})

			if !tt.wantState {
				if a.RegistryState != nil {
					t.Fatalf("expected RegistryState to stay nil, got %+v", a.RegistryState)
				}
				return
			}
			if a.RegistryState == nil {
				t.Fatalf("expected RegistryState to be populated")
			}
			if got := a.AllReleasesYanked(); got != tt.wantYanked {
				t.Errorf("AllReleasesYanked: got %v, want %v", got, tt.wantYanked)
			}
			if a.RegistryState.Registry != tt.wantReg {
				t.Errorf("Registry: got %q, want %q", a.RegistryState.Registry, tt.wantReg)
			}
			if a.RegistryState.Reason != tt.wantReason {
				t.Errorf("Reason: got %q, want %q", a.RegistryState.Reason, tt.wantReason)
			}
			if a.RegistryState.Reference == "" {
				t.Error("Reference: got empty, want a registry URL")
			}
		})
	}
}

// TestEnrichRegistryState_NoRepositoryRequired guards the difference from
// enrichPyPISummary: a registry fact does not depend on a resolved Repository.
func TestEnrichRegistryState_NoRepositoryRequired(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(registryHandler(`{"info":{"name":"conda","yanked":true,"yanked_reason":"use miniconda"}}`, "", nil))
	defer srv.Close()

	s := newRegistryStateService(t, srv.URL)
	a := analysisFor("pkg:pypi/conda", "pypi")
	a.Repository = nil
	s.enrichRegistryState(context.Background(), map[string]*domain.Analysis{"k": a})

	if !a.AllReleasesYanked() {
		t.Fatalf("expected the fact to be recorded without a Repository, got %+v", a.RegistryState)
	}
}

// TestEnrichRegistryState_DeduplicatesByName pins that duplicate PURLs for one
// package collapse into a single request while every analysis is updated.
func TestEnrichRegistryState_DeduplicatesByName(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(registryHandler("", `{"crate":{"name":"normal","yanked":true}}`, &calls))
	defer srv.Close()

	s := newRegistryStateService(t, srv.URL)
	analyses := map[string]*domain.Analysis{
		"a": analysisFor("pkg:cargo/normal", "cargo"),
		"b": analysisFor("pkg:cargo/normal@0.0.0", "cargo"),
	}
	s.enrichRegistryState(context.Background(), analyses)

	for k, a := range analyses {
		if !a.AllReleasesYanked() {
			t.Errorf("%s: expected AllReleasesYanked=true, got %+v", k, a.RegistryState)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("http calls: got %d, want 1", got)
	}
	if analyses["a"].RegistryState == analyses["b"].RegistryState {
		t.Error("expected each analysis to own its RegistryState copy")
	}
}

func TestEnrichRegistryState_FetchFailureLeavesStateNil(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	s := newRegistryStateService(t, srv.URL)
	a := analysisFor("pkg:cargo/normal", "cargo")
	s.enrichRegistryState(context.Background(), map[string]*domain.Analysis{"k": a})

	if a.RegistryState != nil {
		t.Fatalf("expected nil RegistryState after a failed fetch, got %+v", a.RegistryState)
	}
}

func TestEnrichRegistryState_UnwiredClients(t *testing.T) {
	t.Parallel()
	s := &IntegrationService{}
	a := analysisFor("pkg:cargo/normal", "cargo")
	s.enrichRegistryState(context.Background(), map[string]*domain.Analysis{"k": a})
	if a.RegistryState != nil {
		t.Fatalf("expected nil RegistryState without clients, got %+v", a.RegistryState)
	}
}
