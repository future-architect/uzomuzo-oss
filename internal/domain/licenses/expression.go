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

// expressionSplitter matches SPDX OR/AND operators bounded by whitespace.
// The (?i) flag makes the match case-insensitive per SPDX spec; \s+ on both
// sides ensures literal substrings within identifiers are not split (e.g.,
// no SPDX ID currently contains " OR " or " AND " as a substring).
var expressionSplitter = regexp.MustCompile(`(?i)\s+(?:OR|AND)\s+`)

// leadingEdgeOperator matches a stray OR/AND token at the start of a segment
// (with any trailing whitespace), so that malformed inputs like "OR Apache-2.0"
// or recursion segments like "OR MIT" (left over after splitting) yield a
// clean operand.
var leadingEdgeOperator = regexp.MustCompile(`(?i)^(?:OR|AND)\b\s*`)

// trailingEdgeOperator matches a stray OR/AND token at the end of a segment.
var trailingEdgeOperator = regexp.MustCompile(`(?i)\s*\b(?:OR|AND)$`)

// ParseExpression parses an SPDX license expression and normalizes each operand.
//
// Algorithm (v1):
//  1. Trim outer whitespace.
//  2. Strip outer-paren wrapping repeatedly (handles "((MIT))" → "MIT") when the
//     parens balance correctly across the entire string.
//  3. Find OR/AND operator positions at paren depth 0.
//  4. Split into segments; recurse on each segment to flatten nested expressions
//     such as "EPL-1.0 OR (LGPL-2.1 OR LGPL-3.0)" into a single 3-operand list.
//  5. For each operand, run Normalize.
//
// Limitations (v1):
//   - Mixed AND/OR is flattened into a single operand list. Operator structure
//     and precedence are not preserved (no AST). Consumers that need to
//     distinguish AND vs OR semantics for legal compliance must not rely on
//     this parser alone.
//   - "WITH" clauses stay attached to the preceding license-id; the combined
//     "license-id WITH exception-id" string is normalized as a unit and will
//     typically be reported as non-SPDX since no exception table exists yet.
//   - The "+" suffix on a license-id (e.g., "Apache-2.0+") is preserved in Raw;
//     normalization resolves it to the base SPDX ID via the generated alias table
//     (e.g., "Apache-2.0+" → "Apache-2.0").
func ParseExpression(raw string) ExpressionResult {
	res := ExpressionResult{Raw: raw}
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
