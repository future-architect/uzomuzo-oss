package licenses

import "testing"

func TestNormalizeExpression(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Empty / sentinel
		{name: "empty", in: "", want: ""},
		{name: "whitespace_only", in: "   \t\n", want: ""},
		{name: "noassertion_upper", in: "NOASSERTION", want: "NOASSERTION"},
		{name: "noassertion_lower", in: "noassertion", want: "NOASSERTION"},
		{name: "noassertion_padded", in: "  NoAssertion  ", want: "NOASSERTION"},
		{name: "none_alias_to_noassertion", in: "NONE", want: "NOASSERTION"},

		// Atomic SPDX
		{name: "canonical_atomic", in: "Apache-2.0", want: "Apache-2.0"},
		{name: "alias_resolves", in: "Apache License 2.0", want: "Apache-2.0"},
		{name: "casefold_resolves", in: "apache-2.0", want: "Apache-2.0"},

		// Compound SPDX
		{name: "or_compound", in: "MIT OR Apache-2.0", want: "MIT OR Apache-2.0"},
		{name: "and_compound", in: "MIT AND BSD-3-Clause", want: "MIT AND BSD-3-Clause"},
		{name: "with_exception", in: "GPL-2.0-only WITH Classpath-exception-2.0", want: "GPL-2.0-only WITH Classpath-exception-2.0"},
		{name: "or_later", in: "Apache-2.0+", want: "Apache-2.0+"},
		{name: "alias_inside_compound", in: "Apache License 2.0 OR MIT", want: "Apache-2.0 OR MIT"},

		// Mixed-recognized rejects whole expression
		{name: "compound_with_nonstd_leaf_rejected", in: "MIT OR ProprietaryFoo", want: ""},
		{name: "single_nonstd_rejected", in: "ProprietaryFoo", want: ""},
		{name: "free_text_no_alias_rejected", in: "Custom Vendor License", want: ""},

		// NOASSERTION inside compound is recognized (per SPDX 2.3 — NOASSERTION
		// is a valid simple-expression sentinel), so a compound mixing it with
		// a canonical leaf renders as-is.
		{name: "compound_with_noassertion_kept", in: "MIT OR NOASSERTION", want: "MIT OR NOASSERTION"},

		// Idempotence sanity (normalize twice = once)
		{name: "idempotent_alias_compound", in: "Apache License 2.0 OR MIT OR BSD-3-Clause", want: "Apache-2.0 OR MIT OR BSD-3-Clause"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeExpression(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeExpression(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestNormalizeExpression_Idempotent locks in that re-running the normalizer
// on its own output yields the same string. SBOM consumers, cache layers, and
// diff-comparison tooling depend on this property.
func TestNormalizeExpression_Idempotent(t *testing.T) {
	corpus := []string{
		"",
		"NOASSERTION",
		"Apache-2.0",
		"Apache License 2.0",
		"MIT OR Apache-2.0",
		"(MIT AND BSD-3-Clause)",
		"GPL-2.0-only WITH Classpath-exception-2.0",
		"Apache-2.0+",
		"Apache-2.0+ WITH Classpath-exception-2.0",
		"ProprietaryFoo",
		"MIT OR NOASSERTION",
	}
	for _, raw := range corpus {
		t.Run(raw, func(t *testing.T) {
			once := NormalizeExpression(raw)
			twice := NormalizeExpression(once)
			if once != twice {
				t.Errorf("non-idempotent: NormalizeExpression(%q) = %q, NormalizeExpression(%q) = %q", raw, once, once, twice)
			}
		})
	}
}

func TestJoinExpressions(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{name: "nil_slice", in: nil, want: ""},
		{name: "empty_slice", in: []string{}, want: ""},
		{name: "all_empty_entries", in: []string{"", "  "}, want: ""},

		// Single-element shortcuts (no re-parse round-trip needed)
		{name: "single_atomic", in: []string{"MIT"}, want: "MIT"},
		{name: "single_alias", in: []string{"Apache License 2.0"}, want: "Apache-2.0"},
		{name: "single_compound", in: []string{"MIT OR Apache-2.0"}, want: "MIT OR Apache-2.0"},
		{name: "single_noassertion", in: []string{"NOASSERTION"}, want: "NOASSERTION"},

		// Multi-element OR-join
		{name: "two_atomics", in: []string{"MIT", "Apache-2.0"}, want: "MIT OR Apache-2.0"},
		{name: "three_atomics", in: []string{"MIT", "Apache-2.0", "BSD-3-Clause"}, want: "MIT OR Apache-2.0 OR BSD-3-Clause"},
		{name: "alias_then_canonical", in: []string{"Apache License 2.0", "MIT"}, want: "Apache-2.0 OR MIT"},
		{name: "compound_plus_atomic_flatten", in: []string{"MIT OR Apache-2.0", "BSD-3-Clause"}, want: "MIT OR Apache-2.0 OR BSD-3-Clause"},

		// Deduplication preserves first-seen order
		{name: "dedup_first_seen_wins", in: []string{"MIT", "Apache-2.0", "MIT"}, want: "MIT OR Apache-2.0"},
		{name: "dedup_alias_collapses_to_canonical", in: []string{"Apache License 2.0", "Apache-2.0"}, want: "Apache-2.0"},

		// NOASSERTION rules
		{name: "all_noassertion_preserved", in: []string{"NOASSERTION"}, want: "NOASSERTION"},
		{name: "all_noassertion_multi_dedupe", in: []string{"NOASSERTION", "noassertion"}, want: "NOASSERTION"},
		{name: "noassertion_dropped_from_mixed", in: []string{"NOASSERTION", "MIT"}, want: "MIT"},
		{name: "noassertion_dropped_keeps_or_join", in: []string{"NOASSERTION", "MIT", "Apache-2.0"}, want: "MIT OR Apache-2.0"},

		// Non-SPDX entries drop entry-wise (asymmetry vs NormalizeExpression)
		{name: "nonstd_dropped_keeps_recognized", in: []string{"MIT", "ProprietaryFoo"}, want: "MIT"},
		{name: "all_nonstd_returns_empty", in: []string{"ProprietaryFoo", "AnotherCustom"}, want: ""},

		// Compound entry with mixed leaves is rejected as a whole; siblings keep going
		{name: "rejected_compound_does_not_break_siblings", in: []string{"MIT OR ProprietaryFoo", "Apache-2.0"}, want: "Apache-2.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JoinExpressions(tt.in)
			if got != tt.want {
				t.Errorf("JoinExpressions(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestJoinExpressions_OrderPreservedThroughReparse verifies that
// re-parsing-and-rendering the OR-joined survivor list does not reorder
// operands. Maven POM publishers list the primary license first by
// convention; reordering would change the legal posture downstream.
func TestJoinExpressions_OrderPreservedThroughReparse(t *testing.T) {
	got := JoinExpressions([]string{"BSD-3-Clause", "Apache-2.0", "MIT"})
	want := "BSD-3-Clause OR Apache-2.0 OR MIT"
	if got != want {
		t.Errorf("JoinExpressions order drift: got %q, want %q", got, want)
	}
}
