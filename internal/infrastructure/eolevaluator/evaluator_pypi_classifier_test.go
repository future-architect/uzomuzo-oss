package eolevaluator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "github.com/future-architect/uzomuzo/internal/domain/analysis"
	"github.com/future-architect/uzomuzo/internal/infrastructure/pypi"
)

func TestEvaluator_PyPI_InactiveClassifier(t *testing.T) {
	// Mock minimal PyPI JSON response with inactive classifier only (no text phrases)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pypi/inactivepkg/json" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(`{"info":{"name":"inactivepkg","summary":"","description":"","classifiers":["Development Status :: 7 - Inactive"]}}`))
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

	analysis := &domain.Analysis{Package: &domain.Package{PURL: "pkg:pypi/inactivepkg@1.0.0", Ecosystem: "pypi"}}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
	if err != nil {
		panic(err)
	}
	st := out["k"]
	if st.State != domain.EOLEndOfLife {
		t.Fatalf("expected EOLEndOfLife from inactive classifier, got %v", st.State)
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
