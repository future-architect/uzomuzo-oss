package audit_test

import (
	"fmt"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

func makeAnalysisWithLabel(label analysis.MaintenanceStatus) *analysis.Analysis {
	return &analysis.Analysis{
		AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
			analysis.LifecycleAxis: {
				Axis:  analysis.LifecycleAxis,
				Label: string(label),
			},
		},
	}
}

func TestDeriveVerdict(t *testing.T) {
	tests := []struct {
		name string
		a    *analysis.Analysis
		want audit.Verdict
	}{
		{name: "nil_analysis", a: nil, want: audit.VerdictReview},
		{name: "analysis_with_error", a: &analysis.Analysis{Error: fmt.Errorf("network error")}, want: audit.VerdictReview},
		{name: "archived_repo", a: &analysis.Analysis{RepoState: &analysis.RepoState{IsArchived: true}}, want: audit.VerdictReplace},
		{name: "eol_confirmed_via_status", a: &analysis.Analysis{EOL: analysis.EOLStatus{State: analysis.EOLEndOfLife}}, want: audit.VerdictReplace},
		{name: "eol_scheduled", a: &analysis.Analysis{EOL: analysis.EOLStatus{State: analysis.EOLScheduled}}, want: audit.VerdictCaution},
		{name: "active", a: makeAnalysisWithLabel(analysis.LabelActive), want: audit.VerdictOK},
		{name: "legacy_safe", a: makeAnalysisWithLabel(analysis.LabelLegacySafe), want: audit.VerdictOK},
		{name: "stalled", a: makeAnalysisWithLabel(analysis.LabelStalled), want: audit.VerdictCaution},
		{name: "eol_confirmed_label", a: makeAnalysisWithLabel(analysis.LabelEOLConfirmed), want: audit.VerdictReplace},
		{name: "eol_effective_label", a: makeAnalysisWithLabel(analysis.LabelEOLEffective), want: audit.VerdictReplace},
		{name: "eol_scheduled_label", a: makeAnalysisWithLabel(analysis.LabelEOLScheduled), want: audit.VerdictCaution},
		{name: "review_needed", a: makeAnalysisWithLabel(analysis.LabelReviewNeeded), want: audit.VerdictReview},
		{name: "no_lifecycle_result", a: &analysis.Analysis{}, want: audit.VerdictReview},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := audit.DeriveVerdict(tt.a)
			if got != tt.want {
				t.Errorf("DeriveVerdict() = %q, want %q", got, tt.want)
			}
		})
	}
}

func makeAnalysisWithBuild(lifecycleLabel analysis.MaintenanceStatus, buildLabel analysis.BuildIntegrityLabel) *analysis.Analysis {
	return &analysis.Analysis{
		AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
			analysis.LifecycleAxis: {
				Axis:  analysis.LifecycleAxis,
				Label: string(lifecycleLabel),
			},
			analysis.BuildHealthAxis: {
				Axis:  analysis.BuildHealthAxis,
				Label: string(buildLabel),
				Meta:  map[string]string{"score": "5.0"},
			},
		},
	}
}

func TestDeriveVerdict_IgnoresBuildIntegrity(t *testing.T) {
	tests := []struct {
		name string
		a    *analysis.Analysis
		want audit.Verdict
	}{
		{
			name: "active_hardened_ok",
			a:    makeAnalysisWithBuild(analysis.LabelActive, analysis.BuildLabelHardened),
			want: audit.VerdictOK,
		},
		{
			name: "active_moderate_ok",
			a:    makeAnalysisWithBuild(analysis.LabelActive, analysis.BuildLabelModerate),
			want: audit.VerdictOK,
		},
		{
			name: "active_weak_ok",
			a:    makeAnalysisWithBuild(analysis.LabelActive, analysis.BuildLabelWeak),
			want: audit.VerdictOK,
		},
		{
			name: "active_ungraded_ok",
			a:    makeAnalysisWithBuild(analysis.LabelActive, analysis.BuildLabelUngraded),
			want: audit.VerdictOK,
		},
		{
			name: "stalled_hardened_caution",
			a:    makeAnalysisWithBuild(analysis.LabelStalled, analysis.BuildLabelHardened),
			want: audit.VerdictCaution,
		},
		{
			name: "stalled_ungraded_caution",
			a:    makeAnalysisWithBuild(analysis.LabelStalled, analysis.BuildLabelUngraded),
			want: audit.VerdictCaution,
		},
		{
			name: "eol_hardened_replace",
			a:    makeAnalysisWithBuild(analysis.LabelEOLConfirmed, analysis.BuildLabelHardened),
			want: audit.VerdictReplace,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := audit.DeriveVerdict(tt.a)
			if got != tt.want {
				t.Errorf("DeriveVerdict() = %q, want %q", got, tt.want)
			}
		})
	}
}
