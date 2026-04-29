package eolevaluator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/crates"
)

func TestEvaluator_Cargo_Yanked(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":{"crate":"openssl","num":"0.10.45","yanked":true}}`))
	}))
	defer srv.Close()

	cc := crates.NewClient()
	cc.SetBaseURL(srv.URL)
	cc.SetCacheTTL(0)

	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetCratesClient(cc)

	analysis := &domain.Analysis{Package: &domain.Package{PURL: "pkg:cargo/openssl@0.10.45", Ecosystem: "cargo"}}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
	if err != nil {
		t.Fatalf("EvaluateBatch failed: %v", err)
	}
	st := out["k"]
	if st.State != domain.EOLEndOfLife {
		t.Fatalf("expected EOLEndOfLife on yanked crate, got %v", st.State)
	}
	if capturedPath != "/api/v1/crates/openssl/0.10.45" {
		t.Errorf("unexpected request path: got %q, want /api/v1/crates/openssl/0.10.45", capturedPath)
	}
	found := false
	for _, evd := range st.Evidences {
		if evd.Source == "crates.io" && evd.Confidence == 1.0 && evd.Reference == "https://crates.io/crates/openssl/0.10.45" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected crates.io yanked evidence, got %#v", st.Evidences)
	}
}

func TestEvaluator_Cargo_NotYanked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":{"crate":"serde","num":"1.0.197","yanked":false}}`))
	}))
	defer srv.Close()

	cc := crates.NewClient()
	cc.SetBaseURL(srv.URL)
	cc.SetCacheTTL(0)

	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetCratesClient(cc)

	analysis := &domain.Analysis{Package: &domain.Package{PURL: "pkg:cargo/serde@1.0.197", Ecosystem: "cargo"}}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
	if err != nil {
		t.Fatalf("EvaluateBatch failed: %v", err)
	}
	if out["k"].State == domain.EOLEndOfLife {
		t.Fatalf("expected non-EOL on healthy crate, got EOL")
	}
}

func TestEvaluator_Cargo_NonCargoPURL_NoFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("crates server should not be hit for non-cargo PURL")
		w.WriteHeader(500)
	}))
	defer srv.Close()

	cc := crates.NewClient()
	cc.SetBaseURL(srv.URL)
	cc.SetCacheTTL(0)

	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetCratesClient(cc)

	analysis := &domain.Analysis{Package: &domain.Package{PURL: "pkg:npm/foo@1.0.0", Ecosystem: "npm"}}
	if _, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis}); err != nil {
		t.Fatalf("EvaluateBatch failed: %v", err)
	}
}
