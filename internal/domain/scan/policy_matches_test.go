package scan

import (
	"reflect"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

func TestFailPolicyMatches(t *testing.T) {
	labelEntry := func(label analysis.MaintenanceStatus) *analysis.Analysis {
		return &analysis.Analysis{AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{analysis.LifecycleAxis: {Label: string(label)}}}
	}
	malicious := &analysis.Analysis{MaliciousState: &analysis.MaliciousState{Status: analysis.MaliciousStatusFlagged}}
	tests := []struct {
		name, policy string
		a            *analysis.Analysis
		want         []Trigger
	}{
		{"confirmed", "eol-confirmed", labelEntry(analysis.LabelEOLConfirmed), []Trigger{FailEOLConfirmed}},
		{"effective", "eol-effective", labelEntry(analysis.LabelEOLEffective), []Trigger{FailEOLEffective}},
		{"scheduled", "eol-scheduled", labelEntry(analysis.LabelEOLScheduled), []Trigger{FailEOLScheduled}},
		{"stalled", "stalled", labelEntry(analysis.LabelStalled), []Trigger{FailStalled}},
		{"legacy", "legacy-safe", labelEntry(analysis.LabelLegacySafe), []Trigger{FailLegacySafe}},
		{"review", "review-needed", labelEntry(analysis.LabelReviewNeeded), []Trigger{FailReviewNeeded}},
		{"malicious", "malicious", malicious, []Trigger{FailMalicious}},
		{"implied malicious", "legacy-safe", malicious, []Trigger{FailMalicious}},
		{"dormant", "dormant-release", &analysis.Analysis{DormantRelease: &analysis.DormantRelease{Burst: &analysis.DormantBurst{}}}, []Trigger{FailDormantRelease}},
		{"archived", "archived", &analysis.Analysis{RepoState: &analysis.RepoState{IsArchived: true}}, []Trigger{FailArchived}},
		{"disabled", "disabled", &analysis.Analysis{RepoState: &analysis.RepoState{IsDisabled: true}}, []Trigger{FailDisabled}},
		{"combined", "archived,disabled,malicious", &analysis.Analysis{RepoState: &analysis.RepoState{IsArchived: true, IsDisabled: true}, MaliciousState: malicious.MaliciousState}, []Trigger{FailMalicious, FailArchived, FailDisabled}},
		{"nil analysis", "eol-confirmed", nil, nil},
		{"nil lifecycle", "eol-confirmed", &analysis.Analysis{}, nil},
		{"empty", "", malicious, nil},
		{"raw label only", "eol-confirmed", &analysis.Analysis{EOL: analysis.EOLStatus{State: analysis.EOLEndOfLife}}, nil},
		{"unselected", "eol-confirmed", labelEntry(analysis.LabelStalled), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParseFailPolicy(tt.policy)
			if err != nil {
				t.Fatal(err)
			}
			e := audit.AuditEntry{Analysis: tt.a}
			if got := p.Matches(&e); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Matches = %v, want %v", got, tt.want)
			}
			if got := p.Evaluate([]audit.AuditEntry{e}); got != (len(tt.want) > 0) {
				t.Fatalf("Evaluate = %v", got)
			}
		})
	}
}
