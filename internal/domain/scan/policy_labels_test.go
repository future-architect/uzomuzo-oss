package scan

import (
	"sort"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// Test_ValidFailLabels_MatchesLabelMap pins the two halves of the --fail-on
// vocabulary together. ValidFailLabels feeds the CLI help text and the parse
// error message; labelMap decides what actually parses. A label in one but not
// the other is either a label users are told about but cannot use, or one that
// works but is never advertised.
//
// This is the drift that left review-needed un-gatable after ADR-0022 made it
// reachable (#498).
func Test_ValidFailLabels_MatchesLabelMap(t *testing.T) {
	t.Parallel()

	listed := append([]string(nil), ValidFailLabels()...)
	mapped := make([]string, 0, len(labelMap))
	for k := range labelMap {
		mapped = append(mapped, k)
	}
	sort.Strings(listed)
	sort.Strings(mapped)

	if len(listed) != len(mapped) {
		t.Fatalf("label count: ValidFailLabels has %d, labelMap has %d\n  listed=%v\n  mapped=%v",
			len(listed), len(mapped), listed, mapped)
	}
	for i := range listed {
		if listed[i] != mapped[i] {
			t.Errorf("label mismatch at %d: ValidFailLabels=%q, labelMap=%q\n  listed=%v\n  mapped=%v",
				i, listed[i], mapped[i], listed, mapped)
		}
	}
}

// Test_ValidFailLabels_AllParse asserts every advertised label round-trips
// through ParseFailPolicy and lands on the MaintenanceStatus it names, so a
// label cannot be advertised while mapping to the wrong status.
func Test_ValidFailLabels_AllParse(t *testing.T) {
	t.Parallel()

	for _, label := range ValidFailLabels() {
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			p, err := ParseFailPolicy(label)
			if err != nil {
				t.Fatalf("ParseFailPolicy(%q) failed: %v", label, err)
			}
			want, ok := labelMap[label]
			if !ok {
				t.Fatalf("labelMap has no entry for advertised label %q", label)
			}
			if !p.IsTriggered(want) {
				t.Errorf("ParseFailPolicy(%q) does not trigger on %v", label, want)
			}
		})
	}
}

// Test_ParseFailPolicy_ReviewNeeded pins the label added for ADR-0022: a package
// whose every release is yanked is assessed Review Needed, and CI must be able
// to stop on it. Before #498 the label was reachable but not gatable.
func Test_ParseFailPolicy_ReviewNeeded(t *testing.T) {
	t.Parallel()

	p, err := ParseFailPolicy("review-needed")
	if err != nil {
		t.Fatalf("ParseFailPolicy failed: %v", err)
	}
	if !p.IsTriggered(analysis.LabelReviewNeeded) {
		t.Error("review-needed does not trigger on LabelReviewNeeded")
	}
	if p.IsTriggered(analysis.LabelActive) {
		t.Error("review-needed must not trigger on Active")
	}
}
