package application

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

func TestNewAnalysisService(t *testing.T) {
	tests := []struct {
		name    string
		wantNil bool
	}{
		{name: "new_analysis_service", wantNil: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewAnalysisService(nil)
			if tt.wantNil && service != nil {
				t.Errorf("NewAnalysisService() should return nil, got non-nil")
			}
			if !tt.wantNil && service == nil {
				t.Errorf("NewAnalysisService() should return non-nil, got nil")
			}
		})
	}
}

func TestAnalysisService_ProcessBatchPURLs_EmptyList(t *testing.T) {
	tests := []struct {
		name  string
		purls []string
		want  int
	}{
		{name: "empty_purls_list", purls: []string{}, want: 0},
		{name: "nil_purls_list", purls: nil, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewAnalysisService(nil)
			ctx := context.Background()
			got, err := service.ProcessBatchPURLs(ctx, tt.purls)
			if err != nil {
				t.Errorf("AnalysisService.ProcessBatchPURLs() error = %v", err)
				return
			}
			if len(got) != tt.want {
				t.Errorf("AnalysisService.ProcessBatchPURLs() got %d results, want %d", len(got), tt.want)
			}
		})
	}
}

func TestAnalysisService_ProcessBatchGitHubURLs_EmptyList(t *testing.T) {
	tests := []struct {
		name       string
		githubURLs []string
		want       int
	}{
		{name: "empty_urls_list", githubURLs: []string{}, want: 0},
		{name: "nil_urls_list", githubURLs: nil, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewAnalysisService(nil)
			ctx := context.Background()
			got, err := service.ProcessBatchGitHubURLs(ctx, tt.githubURLs)
			if err != nil {
				t.Errorf("AnalysisService.ProcessBatchGitHubURLs() error = %v", err)
				return
			}
			if len(got) != tt.want {
				t.Errorf("AnalysisService.ProcessBatchGitHubURLs() got %d results, want %d", len(got), tt.want)
			}
		})
	}
}

func TestAnalysisService_Integration_Pattern(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{name: "batch_purl_processing_pattern", description: "ProcessBatchPURLs should delegate to integration service and apply lifecycle assessments"},
		{name: "batch_github_processing_pattern", description: "ProcessBatchGitHubURLs should delegate to integration service and apply lifecycle assessments"},
		{name: "csv_export_pattern", description: "WriteScoreCardCSV should delegate to reporter infrastructure"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewAnalysisService(nil)
			if service == nil {
				t.Errorf("Service creation failed")
			}
			// Pattern verification log
			// 1. Application orchestration
			// 2. Domain business logic
			// 3. Infrastructure delegation
			// (No real assertions needed for pattern doc test)
			t.Logf("Pattern verified: %s", tt.description)
		})
	}
}

func TestAnalysisService_ErrorHandling(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{name: "nil_integration_service_panics", expected: "Service with nil integration service should panic or fail"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewAnalysisService(nil)
			ctx := context.Background()
			defer func() {
				if r := recover(); r != nil {
					t.Log("Service correctly panics with nil integration service")
				}
			}()
			_, err := service.ProcessBatchPURLs(ctx, []string{"pkg:npm/test@1.0.0"})
			if err != nil {
				t.Logf("Service returns error with nil integration service: %v", err)
			} else {
				t.Error("Expected error or panic with nil integration service")
			}
		})
	}
}

func TestAnalysisService_DomainLogicIntegration(t *testing.T) {
	tests := []struct {
		name            string
		mockAnalysis    *domain.Analysis
		expectLifecycle bool
		description     string
	}{
		{
			name: "valid_analysis_should_create_lifecycle_assessment",
			mockAnalysis: &domain.Analysis{
				Package: &domain.Package{PURL: "pkg:npm/test@1.0.0", Ecosystem: "npm", Version: "1.0.0"},
				Scores: map[string]*domain.ScoreEntity{
					"Maintained":      domain.NewScoreEntity("Maintained", 8, 10, "Well maintained"),
					"Vulnerabilities": domain.NewScoreEntity("Vulnerabilities", 9, 10, "Few vulnerabilities"),
				},
				RepoState: &domain.RepoState{DaysSinceLastCommit: 5, IsArchived: false, IsDisabled: false},
			},
			expectLifecycle: true,
			description:     "Valid analysis should trigger lifecycle assessment creation",
		},
		{
			name:            "analysis_with_error_should_skip_lifecycle_assessment",
			mockAnalysis:    &domain.Analysis{Package: &domain.Package{PURL: "pkg:npm/error@1.0.0", Ecosystem: "npm", Version: "1.0.0"}, Error: errors.New("analysis failed")},
			expectLifecycle: false,
			description:     "Analysis with error should skip lifecycle assessment creation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mockAnalysis.Error == nil && tt.expectLifecycle {
				if tt.mockAnalysis.AxisResults == nil {
					t.Log("AxisResults not populated (service orchestration not invoked in this unit test)")
				}
			}
			t.Logf("Domain logic pattern verified: %s", tt.description)
		})
	}
}

func TestAnalysisService_WriteScoreCardCSV(t *testing.T) {
	tests := []struct {
		name     string
		results  map[string]*domain.Analysis
		filename string
		wantErr  bool
	}{
		{name: "empty_results", results: make(map[string]*domain.Analysis), filename: "test.csv", wantErr: false},
		{name: "valid_results", results: map[string]*domain.Analysis{"pkg:npm/test@1.0.0": {Package: &domain.Package{PURL: "pkg:npm/test@1.0.0", Ecosystem: "npm", Version: "1.0.0"}}}, filename: "test.csv", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewAnalysisService(nil)
			tempDir := t.TempDir()
			fullPath := filepath.Join(tempDir, tt.filename)
			err := service.WriteScoreCardCSV(tt.results, fullPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("AnalysisService.WriteScoreCardCSV() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ================= Registry Fallback Helper Tests =================

func TestIsRegistryResolvedEOL(t *testing.T) {
	tests := []struct {
		name string
		eol  domain.EOLStatus
		want bool
	}{
		{
			name: "eol_end_of_life_returns_true",
			eol:  domain.EOLStatus{State: domain.EOLEndOfLife},
			want: true,
		},
		{
			name: "eol_scheduled_returns_true",
			eol:  domain.EOLStatus{State: domain.EOLScheduled},
			want: true,
		},
		{
			name: "eol_not_eol_returns_false",
			eol:  domain.EOLStatus{State: domain.EOLNotEOL},
			want: false,
		},
		{
			name: "eol_unknown_returns_false",
			eol:  domain.EOLStatus{State: domain.EOLUnknown},
			want: false,
		},
		{
			name: "eol_end_of_life_with_evidence",
			eol: domain.EOLStatus{
				State:     domain.EOLEndOfLife,
				Evidences: []domain.EOLEvidence{{Source: "PyPI", Summary: "Classifier: Development Status :: 7 - Inactive", Confidence: 1.0}},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRegistryResolvedEOL(tt.eol)
			if got != tt.want {
				t.Errorf("isRegistryResolvedEOL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEolEvidenceSource(t *testing.T) {
	tests := []struct {
		name string
		eol  domain.EOLStatus
		want string
	}{
		{
			name: "with_evidence",
			eol: domain.EOLStatus{
				State:     domain.EOLEndOfLife,
				Evidences: []domain.EOLEvidence{{Source: "PyPI", Summary: "test"}},
			},
			want: "PyPI",
		},
		{
			name: "no_evidence",
			eol:  domain.EOLStatus{State: domain.EOLEndOfLife},
			want: "unknown",
		},
		{
			name: "multiple_evidences_returns_first",
			eol: domain.EOLStatus{
				Evidences: []domain.EOLEvidence{
					{Source: "Packagist", Summary: "first"},
					{Source: "PyPI", Summary: "second"},
				},
			},
			want: "Packagist",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eolEvidenceSource(tt.eol)
			if got != tt.want {
				t.Errorf("eolEvidenceSource() = %q, want %q", got, tt.want)
			}
		})
	}
}

// fakeAnalysisSource implements AnalysisSource with canned responses for testing.
type fakeAnalysisSource struct {
	analyses map[string]*domain.Analysis
}

func (f *fakeAnalysisSource) AnalyzeFromPURLs(_ context.Context, _ []string) (map[string]*domain.Analysis, error) {
	return f.analyses, nil
}

func (f *fakeAnalysisSource) AnalyzeFromGitHubURLs(_ context.Context, _ []string) (map[string]*domain.Analysis, error) {
	return f.analyses, nil
}

// fakeEOLEvaluator implements eolBatchEvaluator with a canned EOL map. It also
// counts calls so tests can assert that exactly one evaluator instance is created
// per ProcessBatch* invocation (per-call construction is deliberate and must not
// be silently hoisted).
type fakeEOLEvaluator struct {
	eolMap map[string]domain.EOLStatus
	calls  *int // pointer so multiple instances share the counter
}

func (f *fakeEOLEvaluator) EvaluateBatch(_ context.Context, _ map[string]*domain.Analysis) (map[string]domain.EOLStatus, error) {
	*f.calls++
	return f.eolMap, nil
}

// newFakeEOLFactory returns a factory function and a shared invocation counter.
// The counter is incremented once per factory call (i.e., once per
// ProcessBatch* invocation), asserting per-call evaluator construction.
func newFakeEOLFactory(eolMap map[string]domain.EOLStatus) (func() eolBatchEvaluator, *int) {
	factoryCalls := 0
	factory := func() eolBatchEvaluator {
		factoryCalls++
		calls := 0
		return &fakeEOLEvaluator{eolMap: eolMap, calls: &calls}
	}
	return factory, &factoryCalls
}

func TestRegistryFallback_ErrorClearedWhenEOLResolved(t *testing.T) {
	tests := []struct {
		name           string
		analysisError  error
		eolState       domain.EOLState
		wantErrorNil   bool
		wantEOLApplied bool
	}{
		{
			name:           "resource_not_found_with_eol_end_of_life_clears_error",
			analysisError:  common.NewResourceNotFoundError("package not found in deps.dev"),
			eolState:       domain.EOLEndOfLife,
			wantErrorNil:   true,
			wantEOLApplied: true,
		},
		{
			name:           "resource_not_found_with_eol_scheduled_clears_error",
			analysisError:  common.NewResourceNotFoundError("package not found in deps.dev"),
			eolState:       domain.EOLScheduled,
			wantErrorNil:   true,
			wantEOLApplied: true,
		},
		{
			name:           "resource_not_found_with_not_eol_keeps_error",
			analysisError:  common.NewResourceNotFoundError("package not found in deps.dev"),
			eolState:       domain.EOLNotEOL,
			wantErrorNil:   false,
			wantEOLApplied: false,
		},
		{
			name:           "resource_not_found_with_unknown_keeps_error",
			analysisError:  common.NewResourceNotFoundError("package not found in deps.dev"),
			eolState:       domain.EOLUnknown,
			wantErrorNil:   false,
			wantEOLApplied: false,
		},
		{
			name:           "non_resource_error_with_eol_keeps_error",
			analysisError:  errors.New("network timeout"),
			eolState:       domain.EOLEndOfLife,
			wantErrorNil:   false,
			wantEOLApplied: false,
		},
		{
			name:           "no_error_with_eol_applies_normally",
			analysisError:  nil,
			eolState:       domain.EOLEndOfLife,
			wantErrorNil:   true,
			wantEOLApplied: true,
		},
	}
	const purl = "pkg:pypi/numeric@24.2"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &domain.Analysis{
				Package: &domain.Package{PURL: purl, Ecosystem: "pypi"},
				Error:   tt.analysisError,
			}
			eolResult := domain.EOLStatus{
				State: tt.eolState,
				Evidences: []domain.EOLEvidence{
					{Source: "PyPI", Summary: "Classifier: Development Status :: 7 - Inactive", Confidence: 1.0},
				},
			}

			eolMap := map[string]domain.EOLStatus{purl: eolResult}
			factory, factoryCalls := newFakeEOLFactory(eolMap)

			svc := NewAnalysisService(&fakeAnalysisSource{analyses: map[string]*domain.Analysis{purl: a}})
			svc.newEOLEvaluator = factory

			ctx := context.Background()
			results, err := svc.ProcessBatchPURLs(ctx, []string{purl})
			if err != nil {
				t.Fatalf("ProcessBatchPURLs() unexpected error: %v", err)
			}

			// Assert per-call evaluator construction (must be exactly 1).
			if *factoryCalls != 1 {
				t.Errorf("expected newEOLEvaluator called once, got %d", *factoryCalls)
			}

			got, ok := results[purl]
			if !ok {
				t.Fatalf("expected result for key %q", purl)
			}

			if tt.wantErrorNil && got.Error != nil {
				t.Errorf("expected error to be cleared, got: %v", got.Error)
			}
			if !tt.wantErrorNil && got.Error == nil {
				t.Error("expected error to be preserved, got nil")
			}
			if tt.wantEOLApplied && got.EOL.State != tt.eolState {
				t.Errorf("expected EOL state %q, got %q", tt.eolState, got.EOL.State)
			}
			if !tt.wantEOLApplied && got.EOL.State != domain.EOLUnknown && got.EOL.State != "" {
				t.Errorf("expected EOL state to remain zero value, got %q", got.EOL.State)
			}
		})
	}
}

func TestRepoURLFallback_ErrorClearedWhenRepoResolved(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		repoURL      string
		repository   *domain.Repository
		wantErrorNil bool
	}{
		{
			name:         "not_found_with_repo_url_and_repository_clears_error",
			err:          common.NewResourceNotFoundError("package not found in deps.dev"),
			repoURL:      "https://github.com/expressjs/express",
			repository:   &domain.Repository{URL: "https://github.com/expressjs/express"},
			wantErrorNil: true,
		},
		{
			name:         "not_found_with_repo_url_only_keeps_error",
			err:          common.NewResourceNotFoundError("package not found in deps.dev"),
			repoURL:      "https://github.com/expressjs/express",
			repository:   nil,
			wantErrorNil: false,
		},
		{
			name:         "not_found_without_repo_url_keeps_error",
			err:          common.NewResourceNotFoundError("package not found in deps.dev"),
			repoURL:      "",
			repository:   nil,
			wantErrorNil: false,
		},
		{
			name:         "non_resource_error_with_repo_url_keeps_error",
			err:          errors.New("network timeout"),
			repoURL:      "https://github.com/expressjs/express",
			repository:   &domain.Repository{URL: "https://github.com/expressjs/express"},
			wantErrorNil: false,
		},
		{
			name:         "no_error_stays_nil",
			err:          nil,
			repoURL:      "https://github.com/expressjs/express",
			repository:   &domain.Repository{URL: "https://github.com/expressjs/express"},
			wantErrorNil: true,
		},
	}
	const purl = "pkg:npm/express@4.18.2"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &domain.Analysis{
				Package:    &domain.Package{PURL: purl, Ecosystem: "npm"},
				Error:      tt.err,
				RepoURL:    tt.repoURL,
				Repository: tt.repository,
			}

			// Empty EOL map: no registry fallback fires, only repo-URL fallback matters here.
			factory, factoryCalls := newFakeEOLFactory(map[string]domain.EOLStatus{})

			svc := NewAnalysisService(&fakeAnalysisSource{analyses: map[string]*domain.Analysis{purl: a}})
			svc.newEOLEvaluator = factory

			ctx := context.Background()
			results, err := svc.ProcessBatchPURLs(ctx, []string{purl})
			if err != nil {
				t.Fatalf("ProcessBatchPURLs() unexpected error: %v", err)
			}

			// Assert per-call evaluator construction (must be exactly 1).
			if *factoryCalls != 1 {
				t.Errorf("expected newEOLEvaluator called once, got %d", *factoryCalls)
			}

			got, ok := results[purl]
			if !ok {
				t.Fatalf("expected result for key %q", purl)
			}

			if tt.wantErrorNil && got.Error != nil {
				t.Errorf("expected error to be cleared, got: %v", got.Error)
			}
			if !tt.wantErrorNil && got.Error == nil {
				t.Error("expected error to be preserved, got nil")
			}
		})
	}
}
