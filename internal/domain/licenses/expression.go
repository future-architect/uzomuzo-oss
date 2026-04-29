package licenses

import (
	"regexp"
	"strings"
)

// ExpressionOperand represents one operand from a parsed SPDX license expression.
//
// For inputs like "Apache-2.0 OR MIT", two operands are produced (one per
// license-id). For "Apache-2.0 WITH Classpath-exception-2.0", a single operand
// is produced with the full text in Raw — WITH clauses bind to the preceding
// license-id per SPDX spec and are not split out in v1. Normalization on a
// WITH-bearing operand will not match SPDX (no exception table is present yet);
// callers should keep Raw for downstream display in that case.
type ExpressionOperand struct {
	// Raw is the operand substring after structural parsing (whitespace trimmed).
	Raw string
	// Normalization is the result of running Normalize on Raw.
	Normalization NormalizationResult
}

// ExpressionResult is the outcome of parsing an SPDX license expression.
//
// For a plain license-id input (e.g., "Apache-2.0"), Operands has length 1.
// For empty / whitespace-only input, Operands is nil.
type ExpressionResult struct {
	// Raw is the original input string, before any trimming.
	Raw string
	// Operands is one entry per top-level OR/AND-separated operand, in the
	// order they appear. Length 1 for non-compound input. Nil for empty input.
	Operands []ExpressionOperand
}

// expressionSplitter matches SPDX OR/AND operators (case-insensitive) bounded
// by ASCII whitespace on both sides. Identifiers without surrounding whitespace
// are never split, so substrings like "FOR-MIT" or "LICENSE-OR-FREE" stay
// intact. Unicode whitespace (e.g. NBSP) is not recognized as a separator —
// SPDX expressions in practice use ASCII whitespace only.
var expressionSplitter = regexp.MustCompile(`(?i)\s+(?:OR|AND)\s+`)

// leadingEdgeOperator matches a stray OR/AND token at the start of a segment,
// requiring trailing whitespace or end-of-string after the operator so that
// hyphenated identifiers like "OR-tools" or "AND-license" are not stripped.
var leadingEdgeOperator = regexp.MustCompile(`(?i)^(?:OR|AND)(?:\s+|$)`)

// trailingEdgeOperator matches a stray OR/AND token at the end of a segment,
// requiring leading whitespace or start-of-string before the operator (mirror
// of leadingEdgeOperator).
var trailingEdgeOperator = regexp.MustCompile(`(?i)(?:^|\s+)(?:OR|AND)$`)

// maxExpressionLength caps the input size accepted by ParseExpression. SPDX
// license expressions in practice are at most a few hundred characters (the
// longest common form is a 4-license dual/triple compound). Anything larger
// is treated as untrusted input and returns no operands so that quadratic
// recursion / regex passes cannot be amplified by adversarial metadata
// (e.g., a malformed ClearlyDefined.io response).
const maxExpressionLength = 64 * 1024

// ParseExpression parses an SPDX license expression and normalizes each operand.
//
// Algorithm (v1):
//  1. Reject inputs longer than maxExpressionLength: Operands is nil and Raw
//     holds the original input verbatim. Callers that ingest untrusted data
//     should treat the raw oversized payload accordingly (log truncated).
//  2. Trim outer whitespace, then repeatedly strip outer-paren wrapping and
//     trim stray edge OR/AND tokens until both reach a fixed point.
//     "((MIT))" → "MIT"; "OR MIT" → "MIT".
//  3. Find OR/AND operator matches via expressionSplitter, then keep only
//     those whose start position is at paren depth 0.
//  4. Split into segments at the surviving operator positions; recurse on
//     each segment so nested expressions such as
//     "EPL-1.0 OR (LGPL-2.1 OR LGPL-3.0)" flatten to a 3-operand list.
//  5. For each operand, run Normalize.
//
// The order of returned operands matches the input. Future versions may add
// operator-kind metadata as additional fields; existing callers will not
// break.
//
// Limitations (v1):
//   - Mixed AND/OR is flattened into a single operand list. Operator structure
//     and precedence are not preserved (no AST). Consumers that need to
//     distinguish AND vs OR semantics for legal compliance must not rely on
//     this parser alone.
//   - "WITH" clauses currently stay attached to the preceding license-id; the
//     combined "license-id WITH exception-id" string is normalized as a unit.
//     SPDX matching of license/exception pairs is out of scope for v1.
//   - The "+" suffix on a license-id is preserved in operand Raw. Whether the
//     operand normalizes to SPDX depends on the generated SPDX table — e.g.,
//     "Apache-2.0+" is mapped back to "Apache-2.0" while "GPL-2.0+" is itself
//     a canonical SPDX ID. The parser does not interpret "+" as "or-later"
//     semantically; consumers needing that semantic must inspect Raw.
//   - Operator boundaries require ASCII whitespace; Unicode whitespace
//     (NBSP, ideographic space) is not recognized as a separator.
func ParseExpression(raw string) ExpressionResult {
	res := ExpressionResult{Raw: raw}
	if len(raw) > maxExpressionLength {
		return res
	}
	parts := splitFlatten(raw)
	if len(parts) == 0 {
		return res
	}
	res.Operands = make([]ExpressionOperand, 0, len(parts))
	for _, p := range parts {
		res.Operands = append(res.Operands, ExpressionOperand{
			Raw:           p,
			Normalization: Normalize(p),
		})
	}
	return res
}

// splitFlatten returns the flat list of operand strings produced by recursively
// splitting s on top-level OR/AND operators. Outer parens are stripped before
// splitting so that "(A OR B)" yields the same operands as "A OR B". Stray
// OR/AND tokens at the start or end (whether from malformed input or from a
// recursion segment) are trimmed so we never emit phantom "OR ..." operands.
//
// Returns nil for empty / whitespace-only input.
func splitFlatten(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for {
		next := trimEdgeOperators(stripOuterParens(s))
		if next == s {
			break
		}
		s = next
	}
	if s == "" {
		return nil
	}

	ops := findTopLevelOperators(s)
	if len(ops) == 0 {
		return []string{s}
	}

	out := make([]string, 0, len(ops)+1)
	last := 0
	for _, op := range ops {
		out = append(out, splitFlatten(s[last:op[0]])...)
		last = op[1]
	}
	out = append(out, splitFlatten(s[last:])...)
	return out
}

// trimEdgeOperators repeatedly strips bare OR/AND tokens at the start or end
// of s, removing the surrounding whitespace each pass. Idempotent: if the
// input has no edge operator tokens, returns s unchanged.
func trimEdgeOperators(s string) string {
	for {
		next := leadingEdgeOperator.ReplaceAllString(s, "")
		next = trailingEdgeOperator.ReplaceAllString(next, "")
		next = strings.TrimSpace(next)
		if next == s {
			return s
		}
		s = next
	}
}

// findTopLevelOperators returns [start, end) byte index pairs for every
// OR/AND operator match in s that occurs at paren depth 0. The end index is
// exclusive, matching regexp.FindAllStringIndex semantics.
func findTopLevelOperators(s string) [][2]int {
	matches := expressionSplitter.FindAllStringIndex(s, -1)
	if len(matches) == 0 {
		return nil
	}
	depthAt := computeDepth(s)
	out := make([][2]int, 0, len(matches))
	for _, m := range matches {
		if depthAt[m[0]] != 0 {
			continue
		}
		out = append(out, [2]int{m[0], m[1]})
	}
	return out
}

// computeDepth returns a slice such that depthAt[i] is the paren nesting depth
// just before the byte at position i. Length is len(s)+1 so the trailing index
// is also valid for end-of-string queries.
func computeDepth(s string) []int {
	depthAt := make([]int, len(s)+1)
	depth := 0
	for i := 0; i < len(s); i++ {
		depthAt[i] = depth
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
	}
	depthAt[len(s)] = depth
	return depthAt
}

// stripOuterParens removes one outer-paren pair if and only if the parens
// balance to zero exactly at the final character. For "(A OR B)" this returns
// "A OR B"; for "(A) OR (B)" it returns the input unchanged because the first
// "(" closes before the end of the string.
func stripOuterParens(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
		return s
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i != len(s)-1 {
				return s
			}
		}
	}
	if depth != 0 {
		return s
	}
	return strings.TrimSpace(s[1 : len(s)-1])
}
