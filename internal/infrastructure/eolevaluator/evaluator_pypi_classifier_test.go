package eolevaluator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/pypi"
)

func TestEvaluator_PyPI_InactiveClassifier(t *testing.T) {
	// Mock minimal PyPI JSON response with inactive classifier only (no text phrases)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/pypi/inactivepkg/json" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"info":{"name":"inactivepkg","summary":"","description":"","classifiers":["Development Status :: 7 - Inactive"]},"releases":{"1.0.0":[{"upload_time_iso_8601":"2026-10-21T00:00:00Z"}]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	pc := pypi.NewClient()
	pc.SetBaseURL(srv.URL)
	pc.SetCacheTTL(0)

	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetPyPIClient(pc)

	analysis := &domain.Analysis{OriginalPURL: "pkg:pypi/inactivepkg@1.0.0", Package: &domain.Package{PURL: "pkg:pypi/inactivepkg@1.0.0", Ecosystem: "pypi", Version: "1.0.0"}}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
	if err != nil {
		panic(err)
	}
	st := out["k"]
	if st.State != domain.EOLEndOfLife {
		t.Fatalf("expected EOLEndOfLife from inactive classifier, got %v", st.State)
	}
	if want := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC); !analysis.PyPIPublishedAt.Equal(want) {
		t.Fatalf("project response publish time = %v, want %v", analysis.PyPIPublishedAt, want)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("registry requests = %d, want only the existing project request", got)
	}
	found := false
	for _, evd := range st.Evidences {
		if evd.Source == "PyPI" && evd.Summary == "Classifier: Development Status :: 7 - Inactive" && evd.Confidence == 1.0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected inactive classifier evidence, evidences=%#v", st.Evidences)
	}
}

// TestEvaluator_PyPI_VersionResponseRecordsEarliestUpload covers the yanked
// rule's write: the project response has no time for the version, so the
// earliest file upload in the version response is recorded.
func TestEvaluator_PyPI_VersionResponseRecordsEarliestUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/pypi/activepkg/json":
			_, _ = w.Write([]byte(`{"info":{"name":"activepkg","summary":"","description":"","classifiers":["Development Status :: 5 - Production/Stable"]},"releases":{}}`))
		case "/pypi/activepkg/1.0.0/json":
			_, _ = w.Write([]byte(`{"info":{"name":"activepkg","version":"1.0.0","yanked":false},"urls":[{"yanked":false,"upload_time_iso_8601":"2026-10-21T05:00:00Z"},{"yanked":false,"upload_time_iso_8601":"2026-10-21T03:00:00Z"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	pc := pypi.NewClient()
	pc.SetBaseURL(srv.URL)
	pc.SetCacheTTL(0)

	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetPyPIClient(pc)

	analysis := &domain.Analysis{OriginalPURL: "pkg:pypi/activepkg@1.0.0", Package: &domain.Package{PURL: "pkg:pypi/activepkg@1.0.0", Ecosystem: "pypi", Version: "1.0.0"}}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
	if err != nil {
		t.Fatalf("EvaluateBatch failed: %v", err)
	}
	if st := out["k"]; st.State == domain.EOLEndOfLife {
		t.Fatalf("unexpected EOL: %+v", st)
	}
	if want := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC); !analysis.PyPIPublishedAt.Equal(want) {
		t.Fatalf("version response publish time = %v, want %v", analysis.PyPIPublishedAt, want)
	}
}
