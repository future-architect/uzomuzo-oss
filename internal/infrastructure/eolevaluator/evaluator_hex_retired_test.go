package eolevaluator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/hex"
)

func TestEvaluator_HexRetired(t *testing.T) {
	tests := []struct {
		name     string
		purl     string
		wantEOL  bool
		wantHits int
	}{
		{"retired release", "pkg:hex/cowlib@2.7.1", true, 1},
		{"live release", "pkg:hex/cowlib@2.21.0", false, 1},
		{"unversioned PURL", "pkg:hex/cowlib", false, 0},
		{"other ecosystem", "pkg:cargo/cowlib@2.7.1", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				if r.URL.Path != "/api/packages/cowlib" {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write([]byte(`{"retirements":{"2.7.1":{"reason":"invalid","message":"bad .app"}}}`))
			}))
			defer srv.Close()
			hc := hex.NewClient()
			hc.SetBaseURL(srv.URL)

			ev := NewEvaluator(nil)
			ev.SetMaxWorkers(1)
			ev.SetHexClient(hc)
			ev.SetCratesClient(nil)
			a := &domain.Analysis{OriginalPURL: tt.purl, Package: &domain.Package{PURL: tt.purl}}
			out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": a})
			if err != nil {
				t.Fatalf("EvaluateBatch failed: %v", err)
			}
			st := out["k"]
			if got := st.State == domain.EOLEndOfLife; got != tt.wantEOL {
				t.Fatalf("EOL = %v, want %v (%#v)", got, tt.wantEOL, st)
			}
			if hits != tt.wantHits {
				t.Errorf("hex.pm requests = %d, want %d", hits, tt.wantHits)
			}
			if !tt.wantEOL {
				return
			}
			if len(st.Evidences) != 1 {
				t.Fatalf("evidences = %#v, want one", st.Evidences)
			}
			e := st.Evidences[0]
			if e.Source != domain.RegistryHex || e.Confidence != 1.0 ||
				e.Summary != "Release retired on hex.pm (invalid): bad .app" ||
				e.Reference != "https://hex.pm/packages/cowlib/2.7.1" {
				t.Errorf("unexpected evidence %#v", e)
			}
		})
	}
}
