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
		DormantRelease: &analysis.DormantRelease{Kind: analysis.DormantReleaseFresh, Burst: &analysis.DormantBurst{Version: "5.0.4"}},
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
	// A marker without its burst is not a marker, as in every output.
	noBurst := audit.AuditEntry{Analysis: &analysis.Analysis{
		DormantRelease: &analysis.DormantRelease{Kind: analysis.DormantReleaseFresh},
	}}
	tests := []struct {
		name    string
		raw     string
		entries []audit.AuditEntry
		want    bool
	}{
		{"named, marked", "dormant-release", []audit.AuditEntry{marked}, true},
		{"named with a label, marked", "review-needed,dormant-release", []audit.AuditEntry{marked}, true},
		{"named, unmarked", "dormant-release", []audit.AuditEntry{unmarked}, false},
		{"named, marker without a burst", "dormant-release", []audit.AuditEntry{noBurst}, false},
		{"review-needed does not imply it", "review-needed", []audit.AuditEntry{marked}, false},
		{"malicious does not imply it", "malicious", []audit.AuditEntry{marked}, false},
		{"label still applies", "stalled", []audit.AuditEntry{marked}, true},
		{"dormant-release alone fails on malicious", "dormant-release", []audit.AuditEntry{malicious}, true},
		{"review-needed alone fails on malicious", "review-needed", []audit.AuditEntry{malicious}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParseFailPolicy(tt.raw)
			if err != nil {
				t.Fatalf("ParseFailPolicy(%q): %v", tt.raw, err)
			}
			if got := p.Evaluate(tt.entries); got != tt.want {
				t.Errorf("policy %q: Evaluate = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
	p, err := ParseFailPolicy("dormant-release")
	if err != nil {
		t.Fatal(err)
	}
	if p.IsEmpty() {
		t.Error("dormant-release alone parsed as an empty policy")
	}
}
