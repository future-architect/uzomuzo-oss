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

// TestEnrichAndAssess_RepairsMissingOriginalPURL pins the AnalysisSource contract
// repair. Version-specific EOL rules read OriginalPURL and do nothing when it is
// empty, so a source that returns only Package.PURL would silently lose yank
// detection. Both entry paths repair the field from the map key, which is the
// caller's requested coordinate by construction. See ADR-0021.
func TestEnrichAndAssess_RepairsMissingOriginalPURL(t *testing.T) {
	const (
		purlKey = "pkg:cargo/sha-1@0.10.1"
		urlKey  = "https://github.com/pydantic/pydantic-extra-types"
	)

	tests := []struct {
		name string
		// gitHubPath selects ProcessBatchGitHubURLs over ProcessBatchPURLs.
		gitHubPath       bool
		key              string
		analysis         *domain.Analysis
		wantOriginalPURL string
	}{
		{
			name: "PURL path: source omitted OriginalPURL — repaired from the map key",
			key:  purlKey,
			analysis: &domain.Analysis{
				Package: &domain.Package{PURL: purlKey, Ecosystem: "cargo"},
			},
			wantOriginalPURL: purlKey,
		},
		{
			name: "PURL path: source supplied OriginalPURL — left alone",
			key:  purlKey,
			analysis: &domain.Analysis{
				OriginalPURL:  "pkg:cargo/sha-1",
				EffectivePURL: purlKey,
				Package:       &domain.Package{PURL: purlKey, Ecosystem: "cargo"},
			},
			wantOriginalPURL: "pkg:cargo/sha-1",
		},
		{
			name:       "GitHub URL path: source omitted OriginalPURL — repaired from the map key",
			gitHubPath: true,
			key:        urlKey,
			analysis: &domain.Analysis{
				EffectivePURL: "pkg:pypi/pydantic-extra-types@2.11.2",
				Package:       &domain.Package{PURL: "pkg:pypi/pydantic-extra-types@2.11.2", Ecosystem: "pypi"},
			},
			// A GitHub URL is the established OriginalPURL value for an analysis
			// with no caller-supplied PURL — see buildGitHubOnlyAnalysis. It does
			// not parse as a PURL, so version-specific rules stay a no-op.
			wantOriginalPURL: urlKey,
		},
		{
			name:       "GitHub URL path: source supplied OriginalPURL — left alone",
			gitHubPath: true,
			key:        urlKey,
			analysis: &domain.Analysis{
				OriginalPURL:  "pkg:pypi/pydantic-extra-types",
				EffectivePURL: "pkg:pypi/pydantic-extra-types@2.11.2",
				Package:       &domain.Package{PURL: "pkg:pypi/pydantic-extra-types@2.11.2", Ecosystem: "pypi"},
			},
			wantOriginalPURL: "pkg:pypi/pydantic-extra-types",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := &recordingEOLEvaluator{}
			svc := NewAnalysisService(&fakeAnalysisSource{
				analyses: map[string]*domain.Analysis{tt.key: tt.analysis},
			})
			svc.newEOLEvaluator = func() eolBatchEvaluator { return rec }

			var err error
			if tt.gitHubPath {
				_, err = svc.ProcessBatchGitHubURLs(context.Background(), []string{tt.key})
			} else {
				_, err = svc.ProcessBatchPURLs(context.Background(), []string{tt.key})
			}
			if err != nil {
				t.Fatalf("process failed: %v", err)
			}

			if got := rec.seen[tt.key]; got != tt.wantOriginalPURL {
				t.Errorf("evaluator saw OriginalPURL = %q, want %q", got, tt.wantOriginalPURL)
			}
		})
	}
}
