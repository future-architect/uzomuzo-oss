package application

import (
	"context"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// recordingEOLEvaluator captures the identity fields the evaluator actually sees.
type recordingEOLEvaluator struct {
	seen map[string]string // map key -> OriginalPURL observed
}

func (r *recordingEOLEvaluator) EvaluateBatch(_ context.Context, analyses map[string]*domain.Analysis) (map[string]domain.EOLStatus, error) {
	r.seen = make(map[string]string, len(analyses))
	for k, a := range analyses {
		if a != nil {
			r.seen[k] = a.OriginalPURL
		}
	}
	return map[string]domain.EOLStatus{}, nil
}

// TestProcessBatchPURLs_RepairsMissingOriginalPURL pins the AnalysisSource contract
// repair. Version-specific EOL rules read OriginalPURL and do nothing when it is
// empty, so a source that returns only Package.PURL would silently lose yank
// detection. ProcessBatchPURLs fills the field from the map key, which is the
// requested PURL by construction. See ADR-0021.
func TestProcessBatchPURLs_RepairsMissingOriginalPURL(t *testing.T) {
	tests := []struct {
		name             string
		analysis         *domain.Analysis
		wantOriginalPURL string
	}{
		{
			name: "source omitted OriginalPURL — repaired from the map key",
			analysis: &domain.Analysis{
				Package: &domain.Package{PURL: "pkg:cargo/sha-1@0.10.1", Ecosystem: "cargo"},
			},
			wantOriginalPURL: "pkg:cargo/sha-1@0.10.1",
		},
		{
			name: "source supplied OriginalPURL — left alone",
			analysis: &domain.Analysis{
				OriginalPURL:  "pkg:cargo/sha-1",
				EffectivePURL: "pkg:cargo/sha-1@0.10.1",
				Package:       &domain.Package{PURL: "pkg:cargo/sha-1@0.10.1", Ecosystem: "cargo"},
			},
			wantOriginalPURL: "pkg:cargo/sha-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			const key = "pkg:cargo/sha-1@0.10.1"
			rec := &recordingEOLEvaluator{}
			svc := NewAnalysisService(&fakeAnalysisSource{
				analyses: map[string]*domain.Analysis{key: tt.analysis},
			})
			svc.newEOLEvaluator = func() eolBatchEvaluator { return rec }

			if _, err := svc.ProcessBatchPURLs(context.Background(), []string{key}); err != nil {
				t.Fatalf("ProcessBatchPURLs failed: %v", err)
			}

			if got := rec.seen[key]; got != tt.wantOriginalPURL {
				t.Errorf("evaluator saw OriginalPURL = %q, want %q", got, tt.wantOriginalPURL)
			}
		})
	}
}
