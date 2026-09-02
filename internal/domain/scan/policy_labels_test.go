package scan

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// wantFailLabels is an independent statement of the --fail-on vocabulary and its
// display order. It is written out by hand on purpose: deriving it from
// failLabels would only compare the production table with itself, and a wrong
// MaintenanceStatus on either side would pass.
var wantFailLabels = []struct {
	label  string
	status analysis.MaintenanceStatus
}{
	{"eol-confirmed", analysis.LabelEOLConfirmed},
	{"eol-effective", analysis.LabelEOLEffective},
	{"eol-scheduled", analysis.LabelEOLScheduled},
	{"stalled", analysis.LabelStalled},
	{"legacy-safe", analysis.LabelLegacySafe},
	{"review-needed", analysis.LabelReviewNeeded},
}

// Test_ValidFailLabels_ContentAndOrder pins what --fail-on accepts and the order
// users see it in. Order is part of the contract: ValidFailLabels feeds the CLI
// help text and the parse error message, so a reordering is user-visible.
func Test_ValidFailLabels_ContentAndOrder(t *testing.T) {
	t.Parallel()

	got := ValidFailLabels()
	if len(got) != len(wantFailLabels) {
		t.Fatalf("label count: got %d %v, want %d", len(got), got, len(wantFailLabels))
	}
	for i, want := range wantFailLabels {
		if got[i] != want.label {
			t.Errorf("label %d: got %q, want %q (full: %v)", i, got[i], want.label, got)
		}
	}
}

// Test_ParseFailPolicy_EachLabelTriggersItsOwnStatus pins the label-to-status
// mapping against hand-written expectations rather than against the production
// table, so a label wired to the wrong MaintenanceStatus is caught. Each case
// also asserts the policy does not trigger on some other status, so a policy
// that matched everything would fail.
func Test_ParseFailPolicy_EachLabelTriggersItsOwnStatus(t *testing.T) {
	t.Parallel()

	for _, tt := range wantFailLabels {
		t.Run(tt.label, func(t *testing.T) {
			t.Parallel()

			p, err := ParseFailPolicy(tt.label)
			if err != nil {
				t.Fatalf("ParseFailPolicy(%q) failed: %v", tt.label, err)
			}
			if !p.IsTriggered(tt.status) {
				t.Errorf("%q does not trigger on %v", tt.label, tt.status)
			}
			for _, other := range wantFailLabels {
				if other.status == tt.status {
					continue
				}
				if p.IsTriggered(other.status) {
					t.Errorf("%q also triggers on %v; a single label must select a single status",
						tt.label, other.status)
				}
			}
			if p.IsTriggered(analysis.LabelActive) {
				t.Errorf("%q triggers on Active; no fail label maps to a healthy package", tt.label)
			}
		})
	}
}

// Test_ParseFailPolicy_ReviewNeeded is the regression pin for #498: ADR-0022
// made Review Needed reachable for a package whose every release is yanked, but
// --fail-on could not gate on it, so CI had no way to stop.
func Test_ParseFailPolicy_ReviewNeeded(t *testing.T) {
	t.Parallel()

	p, err := ParseFailPolicy("review-needed")
	if err != nil {
		t.Fatalf("ParseFailPolicy failed: %v", err)
	}
	if !p.IsTriggered(analysis.LabelReviewNeeded) {
		t.Error("review-needed does not trigger on LabelReviewNeeded")
	}
}
