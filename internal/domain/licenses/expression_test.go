package licenses

import (
	"reflect"
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
			name:        "consecutive_operators_skip_empty_segments",
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

func TestParseExpression_NoassertionMatchType(t *testing.T) {
	got := ParseExpression("NOASSERTION")
	if len(got.Operands) != 1 {
		t.Fatalf("expected 1 operand, got %d", len(got.Operands))
	}
	if got.Operands[0].Normalization.MatchType != MatchNoAssertion {
		t.Errorf("MatchType = %v, want MatchNoAssertion", got.Operands[0].Normalization.MatchType)
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
