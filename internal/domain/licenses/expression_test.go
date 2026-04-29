package licenses

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseExpression(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantRaws    []string
		wantSPDXIDs []string // canonical SPDX ID per operand; empty string means non-SPDX
	}{
		{
			name:        "empty",
			input:       "",
			wantRaws:    nil,
			wantSPDXIDs: nil,
		},
		{
			name:        "whitespace_only",
			input:       "   \t\n",
			wantRaws:    nil,
			wantSPDXIDs: nil,
		},
		{
			name:        "plain_spdx",
			input:       "Apache-2.0",
			wantRaws:    []string{"Apache-2.0"},
			wantSPDXIDs: []string{"Apache-2.0"},
		},
		{
			name:        "plain_alias_spaced",
			input:       "Apache License 2.0",
			wantRaws:    []string{"Apache License 2.0"},
			wantSPDXIDs: []string{"Apache-2.0"},
		},
		{
			name:        "or_two_spdx",
			input:       "Apache-2.0 OR MIT",
			wantRaws:    []string{"Apache-2.0", "MIT"},
			wantSPDXIDs: []string{"Apache-2.0", "MIT"},
		},
		{
			name:        "and_two_spdx",
			input:       "Apache-2.0 AND MIT",
			wantRaws:    []string{"Apache-2.0", "MIT"},
			wantSPDXIDs: []string{"Apache-2.0", "MIT"},
		},
		{
			name:        "or_lowercase",
			input:       "MIT or Apache-2.0",
			wantRaws:    []string{"MIT", "Apache-2.0"},
			wantSPDXIDs: []string{"MIT", "Apache-2.0"},
		},
		{
			name:        "and_mixed_case",
			input:       "MIT And Apache-2.0",
			wantRaws:    []string{"MIT", "Apache-2.0"},
			wantSPDXIDs: []string{"MIT", "Apache-2.0"},
		},
		{
			name:        "and_lowercase",
			input:       "MIT and Apache-2.0",
			wantRaws:    []string{"MIT", "Apache-2.0"},
			wantSPDXIDs: []string{"MIT", "Apache-2.0"},
		},
		{
			name:        "multiple_spaces_around_operator",
			input:       "MIT   OR   Apache-2.0",
			wantRaws:    []string{"MIT", "Apache-2.0"},
			wantSPDXIDs: []string{"MIT", "Apache-2.0"},
		},
		{
			name:        "or_with_alias_operand",
			input:       "MIT OR Apache License 2.0",
			wantRaws:    []string{"MIT", "Apache License 2.0"},
			wantSPDXIDs: []string{"MIT", "Apache-2.0"},
		},
		{
			name:        "with_attached_single_operand",
			input:       "Apache-2.0 WITH Classpath-exception-2.0",
			wantRaws:    []string{"Apache-2.0 WITH Classpath-exception-2.0"},
			wantSPDXIDs: []string{""},
		},
		{
			name:        "or_with_in_second_operand",
			input:       "CDDL-1.1 OR GPL-2.0-only WITH Classpath-exception-2.0",
			wantRaws:    []string{"CDDL-1.1", "GPL-2.0-only WITH Classpath-exception-2.0"},
			wantSPDXIDs: []string{"CDDL-1.1", ""},
		},
		{
			name:        "nested_paren_or_flattens",
			input:       "EPL-1.0 OR (LGPL-2.1 OR LGPL-3.0)",
			wantRaws:    []string{"EPL-1.0", "LGPL-2.1", "LGPL-3.0"},
			wantSPDXIDs: []string{"EPL-1.0", "LGPL-2.1", "LGPL-3.0"},
		},
		{
			name:        "outer_paren_wrap",
			input:       "(MIT OR Apache-2.0)",
			wantRaws:    []string{"MIT", "Apache-2.0"},
			wantSPDXIDs: []string{"MIT", "Apache-2.0"},
		},
		{
			name:        "double_outer_paren_wrap",
			input:       "((MIT OR Apache-2.0))",
			wantRaws:    []string{"MIT", "Apache-2.0"},
			wantSPDXIDs: []string{"MIT", "Apache-2.0"},
		},
		{
			name:        "noassertion_is_single_non_spdx",
			input:       "NOASSERTION",
			wantRaws:    []string{"NOASSERTION"},
			wantSPDXIDs: []string{""},
		},
		{
			// Apache-2.0+ is a deprecated SPDX form ("or later"). The generated
			// SPDX table maps it back to Apache-2.0 as canonical.
			name:        "plus_suffix_resolves_to_base_spdx",
			input:       "Apache-2.0+",
			wantRaws:    []string{"Apache-2.0+"},
			wantSPDXIDs: []string{"Apache-2.0"},
		},
		{
			name:        "leading_operator_yields_single_operand",
			input:       " OR Apache-2.0",
			wantRaws:    []string{"Apache-2.0"},
			wantSPDXIDs: []string{"Apache-2.0"},
		},
		{
			name:        "trailing_operator_yields_single_operand",
			input:       "Apache-2.0 OR ",
			wantRaws:    []string{"Apache-2.0"},
			wantSPDXIDs: []string{"Apache-2.0"},
		},
		{
			// The first " OR " is the matched operator; the residual "OR MIT"
			// segment then has its leading "OR " stripped by trimEdgeOperators.
			name:        "operator_after_split_is_trimmed_as_edge",
			input:       "Apache-2.0 OR OR MIT",
			wantRaws:    []string{"Apache-2.0", "MIT"},
			wantSPDXIDs: []string{"Apache-2.0", "MIT"},
		},
		{
			name:        "tabs_as_operator_whitespace",
			input:       "Apache-2.0\tOR\tMIT",
			wantRaws:    []string{"Apache-2.0", "MIT"},
			wantSPDXIDs: []string{"Apache-2.0", "MIT"},
		},
		{
			name:        "non_outer_paren_pair_left_intact",
			input:       "(MIT) OR (Apache-2.0)",
			wantRaws:    []string{"MIT", "Apache-2.0"},
			wantSPDXIDs: []string{"MIT", "Apache-2.0"},
		},
		{
			name:        "empty_parens_yields_no_operands",
			input:       "()",
			wantRaws:    nil,
			wantSPDXIDs: nil,
		},
		{
			name:        "three_way_or_chain",
			input:       "MIT OR Apache-2.0 OR BSD-3-Clause",
			wantRaws:    []string{"MIT", "Apache-2.0", "BSD-3-Clause"},
			wantSPDXIDs: []string{"MIT", "Apache-2.0", "BSD-3-Clause"},
		},
		{
			// Asymmetric handling pinned: open-only paren keeps the prefix
			// attached because no top-level OR/AND can match at depth 0.
			// Acceptable for v1; reconsider if real-world data shows demand.
			name:        "unbalanced_open_paren_collapses_to_one_operand",
			input:       "(MIT OR Apache-2.0",
			wantRaws:    []string{"(MIT OR Apache-2.0"},
			wantSPDXIDs: []string{""},
		},
		{
			// Asymmetric handling pinned: close-only paren is treated as part
			// of the trailing operand because depth never goes positive.
			name:        "unbalanced_close_paren_kept_on_trailing_operand",
			input:       "MIT OR Apache-2.0)",
			wantRaws:    []string{"MIT", "Apache-2.0)"},
			wantSPDXIDs: []string{"MIT", ""},
		},
		{
			// Defensive: hyphenated identifiers starting with OR-/AND- must
			// not be stripped by trimEdgeOperators. SPDX has no current entry
			// like this, but a future custom alias could.
			name:        "or_prefixed_identifier_not_stripped",
			input:       "OR-tools",
			wantRaws:    []string{"OR-tools"},
			wantSPDXIDs: []string{""},
		},
		{
			name:        "and_prefixed_identifier_not_stripped",
			input:       "AND-license",
			wantRaws:    []string{"AND-license"},
			wantSPDXIDs: []string{""},
		},
		{
			// Pathological input over the length cap returns no operands so
			// recursion / regex passes cannot be amplified.
			name:        "oversized_input_yields_no_operands",
			input:       strings.Repeat("X", maxExpressionLength+1),
			wantRaws:    nil,
			wantSPDXIDs: nil,
		},
		{
			// Boundary: input of exactly maxExpressionLength must still parse.
			// Pads "MIT" to the cap so the parser produces a single operand
			// (heuristic, non-SPDX) rather than tripping the guard. Locks in
			// the strict ">" comparison against future "≥" regressions.
			name:        "input_at_cap_is_accepted",
			input:       "MIT" + strings.Repeat("X", maxExpressionLength-3),
			wantRaws:    []string{"MIT" + strings.Repeat("X", maxExpressionLength-3)},
			wantSPDXIDs: []string{""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseExpression(tt.input)
			if got.Raw != tt.input {
				t.Errorf("Raw = %q, want %q", got.Raw, tt.input)
			}
			var gotRaws []string
			var gotIDs []string
			for _, op := range got.Operands {
				gotRaws = append(gotRaws, op.Raw)
				if op.Normalization.SPDX {
					gotIDs = append(gotIDs, op.Normalization.CanonicalID)
				} else {
					gotIDs = append(gotIDs, "")
				}
			}
			if !reflect.DeepEqual(gotRaws, tt.wantRaws) {
				t.Errorf("operand Raws = %#v, want %#v", gotRaws, tt.wantRaws)
			}
			if !reflect.DeepEqual(gotIDs, tt.wantSPDXIDs) {
				t.Errorf("operand SPDX IDs = %#v, want %#v", gotIDs, tt.wantSPDXIDs)
			}
		})
	}
}

func TestParseExpression_OperandMatchType(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTypes []LicenseMatchType
	}{
		{name: "noassertion", input: "NOASSERTION", wantTypes: []LicenseMatchType{MatchNoAssertion}},
		{name: "exact_canonical", input: "Apache-2.0", wantTypes: []LicenseMatchType{MatchCanonicalExact}},
		{name: "casefold_canonical", input: "apache-2.0", wantTypes: []LicenseMatchType{MatchCanonicalCaseFold}},
		{name: "alias_full_name", input: "Apache License 2.0", wantTypes: []LicenseMatchType{MatchAlias}},
		{name: "heuristic_unknown_with_clause", input: "GPL-2.0-only WITH Classpath-exception-2.0", wantTypes: []LicenseMatchType{MatchHeuristic}},
		{name: "compound_mixed_match_types", input: "Apache-2.0 OR Apache License 2.0", wantTypes: []LicenseMatchType{MatchCanonicalExact, MatchAlias}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseExpression(tt.input)
			if len(got.Operands) != len(tt.wantTypes) {
				t.Fatalf("operand count = %d, want %d (operands=%+v)", len(got.Operands), len(tt.wantTypes), got.Operands)
			}
			for i, want := range tt.wantTypes {
				if got.Operands[i].Normalization.MatchType != want {
					t.Errorf("operand[%d].MatchType = %v, want %v", i, got.Operands[i].Normalization.MatchType, want)
				}
			}
		})
	}
}

// TestParseExpression_LongChainTerminates locks in that a long flat OR chain
// completes without panic and yields the expected operand count. Recursion
// depth on a flat chain is shallow (operators are found in a single pass),
// so this guards regex-pass work and operand accumulation rather than stack
// growth from nesting.
func TestParseExpression_LongChainTerminates(t *testing.T) {
	const operandCount = 1024
	parts := make([]string, operandCount)
	for i := range parts {
		parts[i] = "MIT"
	}
	got := ParseExpression(strings.Join(parts, " OR "))
	if len(got.Operands) != operandCount {
		t.Fatalf("operand count = %d, want %d", len(got.Operands), operandCount)
	}
}

// TestParseExpression_DeepNestingTerminates drives the iterative paren-strip
// loop inside splitFlatten with deeply nested but well-formed input. Each
// loop pass peels one paren pair until the inner identifier is exposed; no
// top-level operators are present, so this complements the flat-chain test
// (which exercises operator-found recursion) rather than duplicating it.
func TestParseExpression_DeepNestingTerminates(t *testing.T) {
	const depth = 512
	input := strings.Repeat("(", depth) + "MIT" + strings.Repeat(")", depth)
	got := ParseExpression(input)
	if len(got.Operands) != 1 {
		t.Fatalf("operand count = %d, want 1 (operands=%+v)", len(got.Operands), got.Operands)
	}
	if got.Operands[0].Raw != "MIT" {
		t.Errorf("operand[0].Raw = %q, want %q", got.Operands[0].Raw, "MIT")
	}
}

func TestStripOuterParens(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "no_parens", in: "MIT", want: "MIT"},
		{name: "balanced_outer", in: "(MIT)", want: "MIT"},
		{name: "balanced_outer_with_inner", in: "(A OR (B AND C))", want: "A OR (B AND C)"},
		{name: "non_outer_pair", in: "(A) OR (B)", want: "(A) OR (B)"},
		{name: "unbalanced_extra_open", in: "((MIT)", want: "((MIT)"}, // depth never reaches 0 → unchanged
		{name: "unbalanced_extra_close", in: "(MIT))", want: "(MIT))"},
		{name: "double_wrap", in: "((MIT))", want: "(MIT)"}, // single pass; caller loops
		{name: "empty_parens", in: "()", want: ""},
		{name: "whitespace_inside", in: "(  MIT  )", want: "MIT"},
		{name: "no_close", in: "(MIT", want: "(MIT"},
		{name: "no_open", in: "MIT)", want: "MIT)"},
		{name: "empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripOuterParens(tt.in); got != tt.want {
				t.Errorf("stripOuterParens(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFindTopLevelOperators(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantCount int
	}{
		{name: "no_operators", in: "Apache-2.0", wantCount: 0},
		{name: "one_top_level_or", in: "Apache-2.0 OR MIT", wantCount: 1},
		{name: "two_top_level_or", in: "A OR B OR C", wantCount: 2},
		{name: "operator_inside_parens_skipped", in: "A OR (B OR C)", wantCount: 1},
		{name: "all_operators_inside_parens", in: "(B OR C)", wantCount: 0},
		{name: "deeply_nested", in: "A OR ((B AND C) OR D)", wantCount: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findTopLevelOperators(tt.in)
			if len(got) != tt.wantCount {
				t.Errorf("findTopLevelOperators(%q) returned %d operators, want %d (got=%v)", tt.in, len(got), tt.wantCount, got)
			}
		})
	}
}
