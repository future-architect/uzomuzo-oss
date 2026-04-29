package licenses

import (
	"reflect"
	"strings"
	"testing"
)

// leafSummary is a compact projection of an ExprLicense for table-driven
// assertions. Captures Identifier, OrLater, Exception, and the Raw input —
// the full Normalization surface is exercised in TestParseExpression_LeafNormalization.
type leafSummary struct {
	Raw        string
	Identifier string
	OrLater    bool
	Exception  string
}

// summarize collects the leaves of result.Root in reading order.
func summarize(r ExpressionResult) []leafSummary {
	leaves := r.Leaves()
	if leaves == nil {
		return nil
	}
	out := make([]leafSummary, 0, len(leaves))
	for _, l := range leaves {
		out = append(out, leafSummary{
			Raw:        l.Raw,
			Identifier: l.Identifier,
			OrLater:    l.OrLater,
			Exception:  l.Exception,
		})
	}
	return out
}

func TestParseExpression_Leaves(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []leafSummary
	}{
		{name: "empty", input: "", want: nil},
		{name: "whitespace_only", input: "   \t\n", want: nil},
		{
			name:  "plain_spdx",
			input: "Apache-2.0",
			want:  []leafSummary{{Raw: "Apache-2.0", Identifier: "Apache-2.0"}},
		},
		{
			name:  "free_text_alias",
			input: "Apache License 2.0",
			want:  []leafSummary{{Raw: "Apache License 2.0", Identifier: "Apache-2.0"}},
		},
		{
			name:  "or_two_spdx",
			input: "Apache-2.0 OR MIT",
			want: []leafSummary{
				{Raw: "Apache-2.0", Identifier: "Apache-2.0"},
				{Raw: "MIT", Identifier: "MIT"},
			},
		},
		{
			name:  "and_two_spdx",
			input: "Apache-2.0 AND MIT",
			want: []leafSummary{
				{Raw: "Apache-2.0", Identifier: "Apache-2.0"},
				{Raw: "MIT", Identifier: "MIT"},
			},
		},
		{
			name:  "or_lowercase",
			input: "MIT or Apache-2.0",
			want: []leafSummary{
				{Raw: "MIT", Identifier: "MIT"},
				{Raw: "Apache-2.0", Identifier: "Apache-2.0"},
			},
		},
		{
			name:  "with_attached_single_leaf",
			input: "Apache-2.0 WITH Classpath-exception-2.0",
			want: []leafSummary{
				{Raw: "Apache-2.0 WITH Classpath-exception-2.0", Identifier: "Apache-2.0", Exception: "Classpath-exception-2.0"},
			},
		},
		{
			name:  "or_with_in_second_operand",
			input: "CDDL-1.1 OR GPL-2.0-only WITH Classpath-exception-2.0",
			want: []leafSummary{
				{Raw: "CDDL-1.1", Identifier: "CDDL-1.1"},
				{Raw: "GPL-2.0-only WITH Classpath-exception-2.0", Identifier: "GPL-2.0-only", Exception: "Classpath-exception-2.0"},
			},
		},
		{
			name:  "free_text_with_exception",
			input: "Apache License 2.0 WITH Classpath-exception-2.0",
			want: []leafSummary{
				{Raw: "Apache License 2.0 WITH Classpath-exception-2.0", Identifier: "Apache-2.0", Exception: "Classpath-exception-2.0"},
			},
		},
		{
			name:  "nested_paren_or_flattens",
			input: "EPL-1.0 OR (LGPL-2.1 OR LGPL-3.0)",
			want: []leafSummary{
				{Raw: "EPL-1.0", Identifier: "EPL-1.0"},
				{Raw: "LGPL-2.1", Identifier: "LGPL-2.1"},
				{Raw: "LGPL-3.0", Identifier: "LGPL-3.0"},
			},
		},
		{
			name:  "outer_paren_wrap",
			input: "(MIT OR Apache-2.0)",
			want: []leafSummary{
				{Raw: "MIT", Identifier: "MIT"},
				{Raw: "Apache-2.0", Identifier: "Apache-2.0"},
			},
		},
		{
			name:  "noassertion",
			input: "NOASSERTION",
			want: []leafSummary{
				{Raw: "NOASSERTION", Identifier: ""},
			},
		},
		{
			name:  "plus_suffix_or_later_flag",
			input: "Apache-2.0+",
			want: []leafSummary{
				// Apache-2.0+ → SPDX alias to Apache-2.0; OrLater preserved.
				{Raw: "Apache-2.0+", Identifier: "Apache-2.0", OrLater: true},
			},
		},
		{
			name:  "leading_operator_dropped",
			input: " OR Apache-2.0",
			want: []leafSummary{{Raw: "Apache-2.0", Identifier: "Apache-2.0"}},
		},
		{
			name:  "trailing_operator_dropped",
			input: "Apache-2.0 OR ",
			want: []leafSummary{{Raw: "Apache-2.0", Identifier: "Apache-2.0"}},
		},
		{
			name:  "consecutive_operators_skipped",
			input: "Apache-2.0 OR OR MIT",
			want: []leafSummary{
				{Raw: "Apache-2.0", Identifier: "Apache-2.0"},
				{Raw: "MIT", Identifier: "MIT"},
			},
		},
		{
			name:  "tabs_as_operator_whitespace",
			input: "Apache-2.0\tOR\tMIT",
			want: []leafSummary{
				{Raw: "Apache-2.0", Identifier: "Apache-2.0"},
				{Raw: "MIT", Identifier: "MIT"},
			},
		},
		{
			name:  "or_prefixed_identifier_not_split",
			input: "OR-tools",
			want:  []leafSummary{{Raw: "OR-tools", Identifier: ""}},
		},
		{
			name:  "and_prefixed_identifier_not_split",
			input: "AND-license",
			want:  []leafSummary{{Raw: "AND-license", Identifier: ""}},
		},
		{
			name:  "oversized_input_yields_no_leaves",
			input: strings.Repeat("X", maxExpressionLength+1),
			want:  nil,
		},
		{
			name:  "three_way_or_chain_flattens",
			input: "MIT OR Apache-2.0 OR BSD-3-Clause",
			want: []leafSummary{
				{Raw: "MIT", Identifier: "MIT"},
				{Raw: "Apache-2.0", Identifier: "Apache-2.0"},
				{Raw: "BSD-3-Clause", Identifier: "BSD-3-Clause"},
			},
		},
		{
			name:  "empty_parens_yields_no_leaves",
			input: "()",
			want:  nil,
		},
		{
			name:  "non_outer_paren_pair_left_intact",
			input: "(MIT) OR (Apache-2.0)",
			want: []leafSummary{
				{Raw: "MIT", Identifier: "MIT"},
				{Raw: "Apache-2.0", Identifier: "Apache-2.0"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarize(ParseExpression(tt.input))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Leaves(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseExpression_Structure(t *testing.T) {
	t.Run("flat_or_chain_is_single_compound", func(t *testing.T) {
		root := ParseExpression("A OR B OR C").Root
		if root == nil || root.Compound == nil {
			t.Fatalf("expected a Compound root, got %+v", root)
		}
		if root.Compound.Operator != "OR" {
			t.Errorf("Operator = %q, want OR", root.Compound.Operator)
		}
		if len(root.Compound.Operands) != 3 {
			t.Errorf("Operands len = %d, want 3", len(root.Compound.Operands))
		}
		// All operands should be leaves at depth 1 (no nested OR Compound).
		for i, op := range root.Compound.Operands {
			if op.License == nil {
				t.Errorf("operand[%d] is not a leaf: %+v", i, op)
			}
		}
	})

	t.Run("nested_paren_or_flattens_into_root_compound", func(t *testing.T) {
		root := ParseExpression("A OR (B OR C)").Root
		if root == nil || root.Compound == nil {
			t.Fatalf("expected a Compound root, got %+v", root)
		}
		if len(root.Compound.Operands) != 3 {
			t.Errorf("expected flattened 3 operands, got %d", len(root.Compound.Operands))
		}
	})

	t.Run("mixed_or_and_respects_precedence", func(t *testing.T) {
		// "A OR B AND C" must parse as "A OR (B AND C)" per SPDX precedence.
		root := ParseExpression("Apache-2.0 OR MIT AND BSD-3-Clause").Root
		if root == nil || root.Compound == nil || root.Compound.Operator != "OR" {
			t.Fatalf("expected OR root, got %+v", root)
		}
		if len(root.Compound.Operands) != 2 {
			t.Fatalf("expected 2 OR operands, got %d", len(root.Compound.Operands))
		}
		left := root.Compound.Operands[0]
		right := root.Compound.Operands[1]
		if left.License == nil || left.License.Identifier != "Apache-2.0" {
			t.Errorf("left = %+v, want leaf(Apache-2.0)", left)
		}
		if right.Compound == nil || right.Compound.Operator != "AND" {
			t.Errorf("right = %+v, want AND compound", right)
		}
		if got := len(right.Compound.Operands); got != 2 {
			t.Errorf("right AND operands = %d, want 2", got)
		}
	})

	t.Run("explicit_paren_overrides_precedence", func(t *testing.T) {
		// "(A OR B) AND C" parses as AND root with OR child.
		root := ParseExpression("(Apache-2.0 OR MIT) AND BSD-3-Clause").Root
		if root == nil || root.Compound == nil || root.Compound.Operator != "AND" {
			t.Fatalf("expected AND root, got %+v", root)
		}
		if len(root.Compound.Operands) != 2 {
			t.Fatalf("expected 2 AND operands, got %d", len(root.Compound.Operands))
		}
		left := root.Compound.Operands[0]
		if left.Compound == nil || left.Compound.Operator != "OR" {
			t.Errorf("left = %+v, want OR compound", left)
		}
	})

	t.Run("with_in_compound_attaches_to_correct_leaf", func(t *testing.T) {
		root := ParseExpression("Apache-2.0 OR MIT WITH Classpath-exception-2.0").Root
		if root == nil || root.Compound == nil {
			t.Fatalf("expected Compound root, got %+v", root)
		}
		if len(root.Compound.Operands) != 2 {
			t.Fatalf("expected 2 operands, got %d", len(root.Compound.Operands))
		}
		// First operand = leaf(Apache-2.0) with no exception.
		if l := root.Compound.Operands[0].License; l == nil || l.Identifier != "Apache-2.0" || l.Exception != "" {
			t.Errorf("first operand = %+v, want plain Apache-2.0 leaf", root.Compound.Operands[0])
		}
		// Second operand = leaf(MIT) WITH Classpath-exception-2.0.
		if l := root.Compound.Operands[1].License; l == nil || l.Identifier != "MIT" || l.Exception != "Classpath-exception-2.0" {
			t.Errorf("second operand = %+v, want MIT WITH Classpath-exception-2.0", root.Compound.Operands[1])
		}
	})
}

func TestExprNode_String(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "single_spdx", input: "Apache-2.0", want: "Apache-2.0"},
		{name: "free_text_alias_canonicalized", input: "Apache License 2.0", want: "Apache-2.0"},
		{name: "or_two", input: "Apache-2.0 OR MIT", want: "Apache-2.0 OR MIT"},
		{name: "and_two", input: "Apache-2.0 AND MIT", want: "Apache-2.0 AND MIT"},
		{name: "with_clause", input: "Apache-2.0 WITH Classpath-exception-2.0", want: "Apache-2.0 WITH Classpath-exception-2.0"},
		{name: "plus_suffix", input: "Apache-2.0+", want: "Apache-2.0+"},
		{name: "or_then_with", input: "CDDL-1.1 OR GPL-2.0-only WITH Classpath-exception-2.0", want: "CDDL-1.1 OR GPL-2.0-only WITH Classpath-exception-2.0"},
		{name: "or_three_flattened", input: "A OR B OR C", want: "A OR B OR C"},
		{name: "nested_or_flattens_in_render", input: "A OR (B OR C)", want: "A OR B OR C"},
		{name: "or_of_and_canonicalizes_with_parens", input: "Apache-2.0 OR MIT AND BSD-3-Clause", want: "Apache-2.0 OR (MIT AND BSD-3-Clause)"},
		{name: "explicit_or_in_and", input: "(Apache-2.0 OR MIT) AND BSD-3-Clause", want: "(Apache-2.0 OR MIT) AND BSD-3-Clause"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseExpression(tt.input).Root.String()
			if got != tt.want {
				t.Errorf("String(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestExprNode_StringIdempotent verifies that re-parsing a rendered AST
// produces an identical second render. This is the round-trip invariant for
// SBOM serialization: once an expression is canonicalized, it stays stable.
func TestExprNode_StringIdempotent(t *testing.T) {
	inputs := []string{
		"Apache-2.0",
		"Apache License 2.0",
		"Apache-2.0 OR MIT",
		"Apache-2.0 AND MIT",
		"Apache-2.0 WITH Classpath-exception-2.0",
		"CDDL-1.1 OR GPL-2.0-only WITH Classpath-exception-2.0",
		"Apache-2.0 OR (MIT AND BSD-3-Clause)",
		"(Apache-2.0 OR MIT) AND BSD-3-Clause",
		"A OR B OR C",
		"Apache-2.0+",
		"Apache-2.0+ WITH Classpath-exception-2.0",
		"NOASSERTION",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			first := ParseExpression(in).Root.String()
			second := ParseExpression(first).Root.String()
			if first != second {
				t.Errorf("not idempotent: input=%q, first=%q, second=%q", in, first, second)
			}
		})
	}
}

func TestParseExpression_LeafNormalization(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantType  LicenseMatchType
		wantSPDX  bool
		wantOrLat bool
	}{
		{name: "exact_canonical", input: "Apache-2.0", wantType: MatchCanonicalExact, wantSPDX: true},
		{name: "casefold_canonical", input: "apache-2.0", wantType: MatchCanonicalCaseFold, wantSPDX: true},
		{name: "alias_full_name", input: "Apache License 2.0", wantType: MatchAlias, wantSPDX: true},
		{name: "noassertion", input: "NOASSERTION", wantType: MatchNoAssertion, wantSPDX: false},
		{name: "heuristic_unknown", input: "Acme Internal", wantType: MatchHeuristic, wantSPDX: false},
		// "+" is stripped before normalization so the base "Apache-2.0" matches exact.
		{name: "or_later_flag", input: "Apache-2.0+", wantOrLat: true, wantSPDX: true, wantType: MatchCanonicalExact},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leaves := ParseExpression(tt.input).Leaves()
			if len(leaves) != 1 {
				t.Fatalf("got %d leaves, want 1", len(leaves))
			}
			l := leaves[0]
			if l.Normalization.MatchType != tt.wantType {
				t.Errorf("MatchType = %v, want %v", l.Normalization.MatchType, tt.wantType)
			}
			if l.Normalization.SPDX != tt.wantSPDX {
				t.Errorf("SPDX = %v, want %v", l.Normalization.SPDX, tt.wantSPDX)
			}
			if l.OrLater != tt.wantOrLat {
				t.Errorf("OrLater = %v, want %v", l.OrLater, tt.wantOrLat)
			}
		})
	}
}

// TestParseExpression_TerminationGuards verifies graceful handling at large
// inputs: a long flat OR chain (1024 operands) and deeply nested parens
// (512 levels) both complete without panic, and an input exactly at the
// length cap is still accepted (pinning the strict ">" comparison against
// future "≥" regressions). These are not hard caps — termination is bounded
// by maxExpressionLength, not by an explicit recursion / chain limit.
func TestParseExpression_TerminationGuards(t *testing.T) {
	t.Run("long_flat_chain", func(t *testing.T) {
		const operandCount = 1024
		parts := make([]string, operandCount)
		for i := range parts {
			parts[i] = "MIT"
		}
		root := ParseExpression(strings.Join(parts, " OR ")).Root
		if root == nil || root.Compound == nil {
			t.Fatalf("got %+v, want compound root", root)
		}
		if got := len(root.Compound.Operands); got != operandCount {
			t.Errorf("operand count = %d, want %d", got, operandCount)
		}
	})

	t.Run("deep_nested_parens", func(t *testing.T) {
		const depth = 512
		input := strings.Repeat("(", depth) + "MIT" + strings.Repeat(")", depth)
		root := ParseExpression(input).Root
		if root == nil || root.License == nil || root.License.Identifier != "MIT" {
			t.Errorf("got %+v, want leaf(MIT)", root)
		}
	})

	t.Run("input_at_cap_is_accepted", func(t *testing.T) {
		input := "MIT" + strings.Repeat("X", maxExpressionLength-3)
		got := ParseExpression(input)
		if got.Root == nil {
			t.Errorf("expected a non-nil root for input of length cap")
		}
	})
}

// TestExprNode_StringPanicsOnZeroValue verifies the invariant that a zero-
// valued ExprNode (neither License nor Compound set) is treated as a
// programmer error and surfaces immediately rather than silently producing
// an empty string.
func TestExprNode_StringPanicsOnZeroValue(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on zero-valued ExprNode.String()")
		}
	}()
	bad := &ExprNode{}
	_ = bad.String()
}

// TestExprNode_StringPanicsOnNilOperand verifies that a Compound containing a
// nil operand panics on render, locking the invariant that the AST shape is
// uniformly enforced regardless of how a node was constructed (parser-built
// or hand-built).
func TestExprNode_StringPanicsOnNilOperand(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on Compound with nil operand")
		}
	}()
	leaf := &ExprNode{License: &ExprLicense{Identifier: "MIT"}}
	bad := &ExprNode{Compound: &ExprCompound{Operator: "OR", Operands: []*ExprNode{leaf, nil}}}
	_ = bad.String()
}

// TestExprNode_StringPanicsOnUnderfilledCompound enforces the documented
// "Operands always length ≥ 2" invariant uniformly with the other panic
// paths. The parser cannot produce a 0/1-operand Compound (finalizeCompound
// collapses), but a hand-built misuse must surface immediately rather than
// rendering a malformed SPDX string.
func TestExprNode_StringPanicsOnUnderfilledCompound(t *testing.T) {
	tests := []struct {
		name     string
		operands []*ExprNode
	}{
		{name: "empty_operands", operands: nil},
		{name: "single_operand", operands: []*ExprNode{{License: &ExprLicense{Identifier: "MIT"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic on Compound with %d operands", len(tt.operands))
				}
			}()
			bad := &ExprNode{Compound: &ExprCompound{Operator: "OR", Operands: tt.operands}}
			_ = bad.String()
		})
	}
}

// TestLeaves_PanicsOnZeroValue mirrors TestExprNode_StringPanicsOnZeroValue
// for the AST walk path; both readers must enforce the same invariant.
func TestLeaves_PanicsOnZeroValue(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on zero-valued ExprNode in Leaves()")
		}
	}()
	r := ExpressionResult{Root: &ExprNode{}}
	_ = r.Leaves()
}

// TestLeaves_PanicsOnNilOperand mirrors TestExprNode_StringPanicsOnNilOperand
// for the walk path. Symmetry between the two reader entry points keeps the
// AST contract uniformly enforced.
func TestLeaves_PanicsOnNilOperand(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on Compound with nil operand in Leaves()")
		}
	}()
	leaf := &ExprNode{License: &ExprLicense{Identifier: "MIT"}}
	r := ExpressionResult{Root: &ExprNode{Compound: &ExprCompound{Operator: "OR", Operands: []*ExprNode{leaf, nil}}}}
	_ = r.Leaves()
}

// TestParseExpression_CompoundWithExceptionDistributes pins the documented
// recovery for "(A OR B) WITH X" — SPDX-strict-grammar-illegal but real in
// downstream data. The exception must propagate to every leaf so that
// downstream legal-compliance consumers do not lose the legally significant
// "WITH" clause.
func TestParseExpression_CompoundWithExceptionDistributes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []leafSummary
	}{
		{
			name:  "or_compound_distributes",
			input: "(GPL-2.0-only OR Apache-2.0) WITH Classpath-exception-2.0",
			want: []leafSummary{
				{Raw: "GPL-2.0-only WITH Classpath-exception-2.0", Identifier: "GPL-2.0-only", Exception: "Classpath-exception-2.0"},
				{Raw: "Apache-2.0 WITH Classpath-exception-2.0", Identifier: "Apache-2.0", Exception: "Classpath-exception-2.0"},
			},
		},
		{
			name:  "and_compound_distributes",
			input: "(GPL-2.0-only AND Apache-2.0) WITH Classpath-exception-2.0",
			want: []leafSummary{
				{Raw: "GPL-2.0-only WITH Classpath-exception-2.0", Identifier: "GPL-2.0-only", Exception: "Classpath-exception-2.0"},
				{Raw: "Apache-2.0 WITH Classpath-exception-2.0", Identifier: "Apache-2.0", Exception: "Classpath-exception-2.0"},
			},
		},
		{
			name:  "leaf_with_existing_exception_kept",
			// The first leaf already has its own exception via the inner WITH;
			// the outer WITH is distributed only to the second (bare) leaf.
			input: "(MIT WITH Inner-Exception OR Apache-2.0) WITH Outer-Exception",
			want: []leafSummary{
				{Raw: "MIT WITH Inner-Exception", Identifier: "MIT", Exception: "Inner-Exception"},
				{Raw: "Apache-2.0 WITH Outer-Exception", Identifier: "Apache-2.0", Exception: "Outer-Exception"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarize(ParseExpression(tt.input))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("\n got %#v\nwant %#v", got, tt.want)
			}
		})
	}
}

// TestParseExpression_WithChain pins the truncation behavior for SPDX-illegal
// chained WITH clauses ("A WITH B WITH C"). SPDX 2.1+ allows at most one
// exception per simple-expression; we accept the first and silently truncate
// subsequent WITH tokens. Documents the choice via test rather than letting a
// future refactor accidentally change it.
func TestParseExpression_WithChain(t *testing.T) {
	got := summarize(ParseExpression("Apache-2.0 WITH Foo WITH Bar"))
	want := []leafSummary{{
		Raw: "Apache-2.0 WITH Foo", Identifier: "Apache-2.0", Exception: "Foo",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("\n got %#v\nwant %#v", got, want)
	}
}

// TestParseExpression_PlusEdgeCases pins handling of "+" placement variants:
// only an exact trailing-single-"+" attached to a non-empty base triggers
// OrLater. Bare "+" and "++" do NOT set OrLater — they are passed verbatim
// to the SPDX normalizer, whose alias-key collapsing may still resolve some
// to a canonical ID, but that is the table's call, not the parser's.
//
// The "Apache-2.0++" case also pins a silent input-data-loss path: the parser
// preserves Raw verbatim but the rendered String() drops the second "+"
// because Normalize() aliases "Apache-2.0++" → canonical "Apache-2.0". The
// test asserts the rendered form so a future change cannot accidentally
// promote double-plus to "or-later" semantics.
func TestParseExpression_PlusEdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantOrLater    bool
		wantIdentifier string
		wantRendered   string
	}{
		{
			name: "single_trailing_plus_sets_or_later", input: "Apache-2.0+",
			wantOrLater: true, wantIdentifier: "Apache-2.0", wantRendered: "Apache-2.0+",
		},
		{
			name: "double_plus_does_not_set_or_later", input: "Apache-2.0++",
			wantOrLater: false, wantIdentifier: "Apache-2.0", wantRendered: "Apache-2.0",
		},
		{
			name: "bare_plus_does_not_set_or_later", input: "+",
			wantOrLater: false, wantIdentifier: "", wantRendered: "+",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ParseExpression(tt.input)
			leaves := res.Leaves()
			if len(leaves) != 1 {
				t.Fatalf("expected 1 leaf, got %d", len(leaves))
			}
			if leaves[0].OrLater != tt.wantOrLater {
				t.Errorf("OrLater = %v, want %v (raw=%q)", leaves[0].OrLater, tt.wantOrLater, leaves[0].Raw)
			}
			if leaves[0].Identifier != tt.wantIdentifier {
				t.Errorf("Identifier = %q, want %q", leaves[0].Identifier, tt.wantIdentifier)
			}
			if got := res.Root.String(); got != tt.wantRendered {
				t.Errorf("String() = %q, want %q", got, tt.wantRendered)
			}
		})
	}
}

// TestParseExpression_PlusWithCombined pins the combined +/WITH form, which
// is the natural SPDX way to express "Apache-2.0 or any later version, with
// the Classpath exception".
func TestParseExpression_PlusWithCombined(t *testing.T) {
	got := summarize(ParseExpression("Apache-2.0+ WITH Classpath-exception-2.0"))
	want := []leafSummary{{
		Raw:        "Apache-2.0+ WITH Classpath-exception-2.0",
		Identifier: "Apache-2.0",
		OrLater:    true,
		Exception:  "Classpath-exception-2.0",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("\n got %#v\nwant %#v", got, want)
	}
	// Round-trip via String() must preserve both flags.
	rendered := ParseExpression("Apache-2.0+ WITH Classpath-exception-2.0").Root.String()
	if rendered != "Apache-2.0+ WITH Classpath-exception-2.0" {
		t.Errorf("String() = %q, want exact preservation", rendered)
	}
}

// TestParseExpression_FreeTextAdjacentOperators verifies the tokenizer's
// adjacent-IDENT-merge behavior: a multi-word license name like "Apache
// License 2.0" stays as one IDENT even when followed by an operator and a
// second license. Also pins the inverse case ("Apache OR Tools") where two
// non-SPDX words happen to flank an OR keyword and should split.
func TestParseExpression_FreeTextAdjacentOperators(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []leafSummary
	}{
		{
			name:  "free_text_alias_then_or_then_spdx",
			input: "Apache License 2.0 OR MIT",
			want: []leafSummary{
				{Raw: "Apache License 2.0", Identifier: "Apache-2.0"},
				{Raw: "MIT", Identifier: "MIT"},
			},
		},
		{
			name:  "two_words_split_by_or",
			input: "Apache OR Tools",
			want: []leafSummary{
				{Raw: "Apache", Identifier: ""},
				{Raw: "Tools", Identifier: ""},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarize(ParseExpression(tt.input))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("\n got %#v\nwant %#v", got, tt.want)
			}
		})
	}
}

// TestParseExpression_AdjacentSimpleExpressions pins the silent-truncation
// behavior for malformed inputs that juxtapose two simple-expressions with
// no operator between them — e.g., `(MIT) (Apache-2.0)` from a buggy SBOM
// exporter. The parser returns the first compound and stops; the trailing
// tokens are left unconsumed. Locked-in to prevent silent regressions when
// the parser's recovery policy is revised.
func TestParseExpression_AdjacentSimpleExpressions(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string // expected Identifier sequence from Leaves()
	}{
		{name: "adjacent_paren_groups", input: "(MIT) (Apache-2.0)", want: []string{"MIT"}},
		{name: "adjacent_idents_merge_to_one", input: "MIT Apache-2.0", want: []string{""}}, // tokenizer merges to "MIT Apache-2.0", not SPDX
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leaves := ParseExpression(tt.input).Leaves()
			got := make([]string, 0, len(leaves))
			for _, l := range leaves {
				got = append(got, l.Identifier)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestParseExpression_TrailingOperators pins recovery for inputs ending in a
// dangling binary operator. The grammar requires a right-hand operand, but
// real-world Maven POM and ClearlyDefined data occasionally truncate. The
// parser drops the trailing operator and returns the left primary unchanged
// — never produces a degenerate single-operand Compound. WITH at end-of-
// input falls through to the same recovery path as "WITH not followed by an
// identifier".
func TestParseExpression_TrailingOperators(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []leafSummary
	}{
		{
			name:  "or_at_end",
			input: "Apache-2.0 OR ",
			want:  []leafSummary{{Raw: "Apache-2.0", Identifier: "Apache-2.0"}},
		},
		{
			name:  "and_at_end",
			input: "Apache-2.0 AND ",
			want:  []leafSummary{{Raw: "Apache-2.0", Identifier: "Apache-2.0"}},
		},
		{
			name:  "with_at_end_no_exception",
			input: "Apache-2.0 WITH ",
			want:  []leafSummary{{Raw: "Apache-2.0", Identifier: "Apache-2.0"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarize(ParseExpression(tt.input))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("\n got %#v\nwant %#v", got, tt.want)
			}
		})
	}
}

// TestParseExpression_UnbalancedParens pins the asymmetric handling of
// unbalanced parens. v2 is more permissive than v1 because the recursive-
// descent parser tolerates both unmatched open and unmatched close, but the
// behavior is still asymmetric and worth locking in.
func TestParseExpression_UnbalancedParens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string // expected Leaves() Identifier sequence
	}{
		{name: "open_only", input: "(MIT OR Apache-2.0", want: []string{"MIT", "Apache-2.0"}},
		{name: "close_only", input: "MIT OR Apache-2.0)", want: []string{"MIT", "Apache-2.0"}},
		{name: "mixed", input: "(MIT) OR (Apache-2.0)", want: []string{"MIT", "Apache-2.0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leaves := ParseExpression(tt.input).Leaves()
			got := make([]string, 0, len(leaves))
			for _, l := range leaves {
				got = append(got, l.Identifier)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
