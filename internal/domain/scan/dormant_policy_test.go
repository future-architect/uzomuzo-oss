package scan

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

// TestDormantReleaseFailPolicy pins that dormant-release fails a scan only when
// named explicitly, whatever the lifecycle label is. See ADR-0028.
func TestDormantReleaseFailPolicy(t *testing.T) {
	marked := audit.AuditEntry{Analysis: &analysis.Analysis{
		DormantRelease: &analysis.DormantRelease{Kind: analysis.DormantReleaseFresh},
		AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
			analysis.LifecycleAxis: {Label: string(analysis.LabelStalled)},
		},
	}}
	unmarked := audit.AuditEntry{Analysis: &analysis.Analysis{}}
	// A malicious version without the marker, like chalk 5.6.1: any non-empty
	// selection fails on it (ADR-0027), dormant-release alone included.
	malicious := audit.AuditEntry{Analysis: &analysis.Analysis{
		MaliciousState: &analysis.MaliciousState{Status: analysis.MaliciousStatusFlagged, AdvisoryID: "MAL-1"},
	}}
	tests := []struct {
		raw     string
		entries []audit.AuditEntry
		want    bool
	}{
		{"dormant-release", []audit.AuditEntry{marked}, true},
		{"review-needed,dormant-release", []audit.AuditEntry{marked}, true},
		{"dormant-release", []audit.AuditEntry{unmarked}, false},
		{"review-needed", []audit.AuditEntry{marked}, false},
		{"malicious", []audit.AuditEntry{marked}, false},
		{"stalled", []audit.AuditEntry{marked}, true},
		{"dormant-release", []audit.AuditEntry{malicious}, true},
		{"review-needed", []audit.AuditEntry{malicious}, true},
	}
	for _, tt := range tests {
		p, err := ParseFailPolicy(tt.raw)
		if err != nil {
			t.Fatalf("ParseFailPolicy(%q): %v", tt.raw, err)
		}
		if got := p.Evaluate(tt.entries); got != tt.want {
			t.Errorf("policy %q: Evaluate = %v, want %v", tt.raw, got, tt.want)
		}
	}
	p, err := ParseFailPolicy("dormant-release")
	if err != nil {
		t.Fatal(err)
	}
	if p.IsEmpty() {
		t.Error("dormant-release alone parsed as an empty policy")
	}
}
