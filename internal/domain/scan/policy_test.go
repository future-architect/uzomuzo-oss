package scan

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

func TestParseFailPolicy(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
		isEmpty bool
	}{
		{name: "empty string", raw: "", isEmpty: true},
		{name: "single label", raw: "eol-confirmed"},
		{name: "multiple labels", raw: "eol-confirmed,stalled,legacy-safe"},
		{name: "with spaces", raw: " eol-confirmed , stalled "},
		{name: "invalid label", raw: "eol-confirmed,bogus", wantErr: true},
		{name: "case insensitive", raw: "EOL-Confirmed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParseFailPolicy(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.IsEmpty() != tt.isEmpty {
				t.Errorf("IsEmpty() = %v, want %v", p.IsEmpty(), tt.isEmpty)
			}
		})
	}
}

func TestFailPolicy_Evaluate(t *testing.T) {
	makeEntry := func(label analysis.MaintenanceStatus) domainaudit.AuditEntry {
		return domainaudit.AuditEntry{
			PURL: "pkg:npm/test@1.0.0",
			Analysis: &analysis.Analysis{
				AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
					analysis.LifecycleAxis: {Label: string(label)},
				},
			},
			Verdict: domainaudit.VerdictOK,
		}
	}

	tests := []struct {
		name    string
		failOn  string
		entries []domainaudit.AuditEntry
		want    bool
	}{
		{
			name:    "empty policy never fails",
			failOn:  "",
			entries: []domainaudit.AuditEntry{makeEntry(analysis.LabelEOLConfirmed)},
			want:    false,
		},
		{
			name:    "matching label triggers failure",
			failOn:  "eol-confirmed",
			entries: []domainaudit.AuditEntry{makeEntry(analysis.LabelEOLConfirmed)},
			want:    true,
		},
		{
			name:    "non-matching label does not trigger",
			failOn:  "stalled",
			entries: []domainaudit.AuditEntry{makeEntry(analysis.LabelActive)},
			want:    false,
		},
		{
			name:   "mixed entries one match",
			failOn: "eol-effective",
			entries: []domainaudit.AuditEntry{
				makeEntry(analysis.LabelActive),
				makeEntry(analysis.LabelEOLEffective),
			},
			want: true,
		},
		{
			name:   "nil analysis skipped",
			failOn: "eol-confirmed",
			entries: []domainaudit.AuditEntry{
				{PURL: "pkg:npm/test@1.0.0", Analysis: nil},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParseFailPolicy(tt.failOn)
			if err != nil {
				t.Fatalf("ParseFailPolicy: %v", err)
			}
			got := p.Evaluate(tt.entries)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}
