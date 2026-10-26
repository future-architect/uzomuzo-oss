package scan

import (
	"errors"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

// TestArchivedDisabledFailPolicy pins that archived and disabled fail a scan on
// the repository state alone, whatever the lifecycle label is, and only when
// named explicitly. See ADR-0029.
func TestArchivedDisabledFailPolicy(t *testing.T) {
	withLabel := func(rs *analysis.RepoState, label analysis.MaintenanceStatus) audit.AuditEntry {
		return audit.AuditEntry{Analysis: &analysis.Analysis{
			RepoState: rs,
			AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
				analysis.LifecycleAxis: {Label: string(label)},
			},
		}}
	}
	// Archived and Stalled, like tibdex/github-app-token.
	archivedStalled := withLabel(&analysis.RepoState{IsArchived: true}, analysis.LabelStalled)
	archivedEOL := withLabel(&analysis.RepoState{IsArchived: true}, analysis.LabelEOLConfirmed)
	// Stalled but not archived, like actions/github-script.
	quietStalled := withLabel(&analysis.RepoState{}, analysis.LabelStalled)
	disabledOnly := withLabel(&analysis.RepoState{IsDisabled: true}, analysis.LabelStalled)
	// An analysis error leaves no lifecycle result, but the repository state
	// can still be known.
	archivedErrored := audit.AuditEntry{Analysis: &analysis.Analysis{
		RepoState: &analysis.RepoState{IsArchived: true},
		Error:     errors.New("deps.dev lookup failed"),
	}}
	archivedMalicious := audit.AuditEntry{Analysis: &analysis.Analysis{
		RepoState:      &analysis.RepoState{IsArchived: true},
		MaliciousState: &analysis.MaliciousState{Status: analysis.MaliciousStatusFlagged, AdvisoryID: "MAL-1"},
	}}
	maliciousOnly := audit.AuditEntry{Analysis: &analysis.Analysis{
		MaliciousState: &analysis.MaliciousState{Status: analysis.MaliciousStatusFlagged, AdvisoryID: "MAL-1"},
	}}
	archivedDormant := audit.AuditEntry{Analysis: &analysis.Analysis{
		RepoState:      &analysis.RepoState{IsArchived: true},
		DormantRelease: &analysis.DormantRelease{Kind: analysis.DormantReleaseFresh, Burst: &analysis.DormantBurst{Version: "2.0.1"}},
	}}
	nilAnalysis := audit.AuditEntry{}
	noRepoState := audit.AuditEntry{Analysis: &analysis.Analysis{}}

	tests := []struct {
		name    string
		raw     string
		entries []audit.AuditEntry
		want    bool
	}{
		{"archived, Stalled", "archived", []audit.AuditEntry{archivedStalled}, true},
		{"archived, EOL-Confirmed", "archived", []audit.AuditEntry{archivedEOL}, true},
		{"archived, analysis error", "archived", []audit.AuditEntry{archivedErrored}, true},
		{"archived, not archived", "archived", []audit.AuditEntry{quietStalled}, false},
		{"archived, disabled only", "archived", []audit.AuditEntry{disabledOnly}, false},
		{"disabled, disabled only", "disabled", []audit.AuditEntry{disabledOnly}, true},
		{"disabled, archived only", "disabled", []audit.AuditEntry{archivedStalled}, false},
		{"archived, archived and malicious", "archived", []audit.AuditEntry{archivedMalicious}, true},
		{"malicious, archived and malicious", "malicious", []audit.AuditEntry{archivedMalicious}, true},
		{"archived alone fails on malicious", "archived", []audit.AuditEntry{maliciousOnly}, true},
		{"disabled alone fails on malicious", "disabled", []audit.AuditEntry{maliciousOnly}, true},
		{"archived, archived and dormant", "archived", []audit.AuditEntry{archivedDormant}, true},
		{"dormant-release does not imply archived", "dormant-release", []audit.AuditEntry{archivedStalled}, false},
		{"malicious does not imply archived", "malicious", []audit.AuditEntry{archivedStalled}, false},
		{"a label does not imply disabled", "eol-confirmed", []audit.AuditEntry{disabledOnly}, false},
		{"archived does not imply disabled", "archived", []audit.AuditEntry{disabledOnly}, false},
		{"label still applies", "stalled", []audit.AuditEntry{quietStalled}, true},
		{"one archived entry among others", "archived", []audit.AuditEntry{quietStalled, archivedStalled}, true},
		{"nil analysis", "archived,disabled", []audit.AuditEntry{nilAnalysis}, false},
		{"no repository state", "archived,disabled", []audit.AuditEntry{noRepoState}, false},
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
	for _, raw := range []string{"archived", "disabled", " Archived "} {
		p, err := ParseFailPolicy(raw)
		if err != nil {
			t.Fatalf("ParseFailPolicy(%q): %v", raw, err)
		}
		if p.IsEmpty() {
			t.Errorf("%q alone parsed as an empty policy", raw)
		}
	}
}

// TestNeedsRepositoryState pins which selections need repository state read
// with a GitHub token.
func TestNeedsRepositoryState(t *testing.T) {
	for raw, want := range map[string]bool{
		"archived": true, "disabled": true, "stalled,archived": true,
		"": false, "stalled": false, "malicious": false, "dormant-release": false,
	} {
		p, err := ParseFailPolicy(raw)
		if err != nil {
			t.Fatalf("ParseFailPolicy(%q): %v", raw, err)
		}
		if got := p.NeedsRepositoryState(); got != want {
			t.Errorf("%q: NeedsRepositoryState = %v, want %v", raw, got, want)
		}
	}
}
