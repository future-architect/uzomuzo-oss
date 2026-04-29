package licenses

import (
	"fmt"
	"strings"
	"unicode"
)

// ExprNode is a node in the parsed SPDX expression AST. It is a tagged union:
// exactly one of License or Compound is non-nil for a valid node. A zero-valued
// ExprNode is invalid and will panic in String / Leaves operations to surface
// programmer errors early.
type ExprNode struct {
	License  *ExprLicense
	Compound *ExprCompound
}

// ExprLicense is a leaf node — an SPDX simple-expression: a license-id with
// an optional "+" (or-later) suffix and an optional " WITH <exception-id>"
// clause, or a free-text license name normalized via Normalize.
type ExprLicense struct {
	// Raw is the source substring this leaf was built from, including any
	// "+" suffix and WITH clause attached during parsing. When an outer WITH
	// is distributed onto a Compound (recovery for "(A OR B) WITH X"), each
	// leaf's Raw is rewritten to include the inherited exception — Raw thus
	// reflects the leaf's effective form, not necessarily a verbatim slice
	// of the original input.
	Raw string
	// Identifier is the canonical SPDX ID of the base license, or "" when the
	// base did not normalize to SPDX. The "+" suffix and WITH clause are NOT
	// part of Identifier.
	Identifier string
	// Normalization is the full normalization result for the base license
	// (with "+" stripped and WITH clause removed before normalizing).
	Normalization NormalizationResult
	// OrLater is true when the base license-id had a trailing "+" indicating
	// "this version or any later version" per SPDX §10.2.3. The parser does
	// not interpret the semantics; consumers needing "or-later" handling
	// must read this flag.
	OrLater bool
	// Exception is the verbatim exception-id following a WITH clause, or ""
	// when no exception was present. The parser does NOT validate exceptions
	// against any SPDX exceptions table — there is no such table loaded in
	// this codebase yet. Future revisions will normalize this field.
	Exception string
}

// ExprCompound combines two or more sibling nodes with the same operator.
// Chains of the same operator at the same precedence level are flattened
// into a single Compound: "A OR B OR C" produces one Compound with three
// children, not two nested Compounds. This matches SPDX renderer convention
// and is set-equivalent in legal terms.
type ExprCompound struct {
	// Operator is "OR" or "AND" (canonical uppercase).
	Operator string
	// Operands is the ordered list of children, always length ≥ 2.
	Operands []*ExprNode
}

// ExpressionResult is the outcome of parsing an SPDX license expression.
type ExpressionResult struct {
	// Raw is the original input string, before any trimming.
	Raw string
	// Root is the parsed AST. Nil for empty / oversized / fully-malformed input.
	Root *ExprNode
}

// SPDX expression operator keywords.
const (
	opOR   = "OR"
	opAND  = "AND"
	opWITH = "WITH"
)

// maxExpressionLength caps the input size (in bytes) accepted by
// ParseExpression. SPDX license expressions in practice are at most a few
// hundred characters; the generous cap defends against adversarial metadata
// (e.g. malformed ClearlyDefined.io responses) that would amplify regex /
// recursion work. Counted in bytes via len(), not runes, because the goal is
// memory-bound resource control, not user-visible width.
const maxExpressionLength = 64 * 1024

// ParseExpression parses an SPDX license expression and returns the AST.
//
// Grammar (recursive descent, precedence WITH > AND > OR per SPDX §10.2.3):
//
//	expression = and_expr (OR and_expr)*
//	and_expr   = with_expr (AND with_expr)*
//	with_expr  = primary (WITH ident)?
//	primary    = ident ['+'] | '(' expression ')'
//
// Tokenization:
//   - "OR" / "AND" / "WITH" (case-insensitive) and "(" / ")" are reserved.
//   - Adjacent non-reserved word tokens merge into a single license-id (single
//     space joined), so free-text Maven POM "<name>" values like "Apache
//     License 2.0" are treated as one identifier and normalized via the alias
//     table. Substrings like "OR-tools" or "AND-license" stay as IDENTs (they
//     don't equal-fold to the reserved words).
//
// Robustness:
//   - Empty / whitespace-only / oversized input → Root is nil.
//   - Stray edge operators ("OR Apache-2.0", "Apache-2.0 OR") are silently
//     dropped (matches v1 contract).
//   - A Compound with fewer than 2 children after parsing collapses to its
//     single child or to nil; never emits a degenerate compound.
//   - The parser never returns an error — consumers ingesting external data
//     can treat a nil Root as "could not extract any license info" and fall
//     back to their non-SPDX path.
//
// Limitations:
//   - WITH exception-ids are kept verbatim in ExprLicense.Exception; no
//     normalization against an SPDX exceptions table (none is loaded yet).
//   - "+" suffix is recorded as ExprLicense.OrLater; the parser does not
//     interpret it semantically.
func ParseExpression(raw string) ExpressionResult {
	res := ExpressionResult{Raw: raw}
	if len(raw) > maxExpressionLength {
		return res
	}
	tokens := lex(raw)
	if len(tokens) == 0 {
		return res
	}
	p := &parser{tokens: tokens}
	res.Root = p.parseExpression()
	return res
}

// String renders the AST back to canonical SPDX expression syntax. Compound
// children are parenthesized only when needed for precedence — a child of an
// OR whose own operator is also OR renders bare; a child with a different
// operator gets parens. The result satisfies the idempotence property:
// re-parsing the rendered string and rendering again produces the same text.
//
// Panics on any zero-valued or nil-operand node — these can only arise from
// hand-built ASTs (the parser itself never produces them) and are surfaced
// as programmer errors so a misuse is caught at the first render rather than
// silently producing malformed SPDX text.
func (n *ExprNode) String() string {
	if n == nil {
		return ""
	}
	if n.License != nil {
		return n.License.String()
	}
	if n.Compound == nil {
		panic("licenses.ExprNode: zero-valued node (neither License nor Compound)")
	}
	parts := make([]string, len(n.Compound.Operands))
	for i, child := range n.Compound.Operands {
		if child == nil {
			panic("licenses.ExprNode: nil operand in Compound")
		}
		s := child.String()
		if needsParens(child, n.Compound.Operator) {
			s = "(" + s + ")"
		}
		parts[i] = s
	}
	return strings.Join(parts, " "+n.Compound.Operator+" ")
}

// String renders an ExprLicense leaf as "<id>[+][ WITH <exception>]". When
// the base did not normalize to SPDX, Raw of the base portion is used as a
// best-effort fallback.
func (l *ExprLicense) String() string {
	base := l.Identifier
	if base == "" {
		base = l.baseRaw()
	}
	if l.OrLater {
		base += "+"
	}
	if l.Exception != "" {
		return base + " " + opWITH + " " + l.Exception
	}
	return base
}

// Leaves walks the AST and returns leaf ExprLicense pointers in reading order.
// Returns nil when Root is nil so callers using a sentinel-nil check can
// distinguish "no license info parsed" from "empty result accidentally
// produced". This is the convenience accessor for consumers that do not need
// operator structure (e.g., simple license enumeration).
//
// Panics if the AST contains a zero-valued ExprNode (programmer error).
func (r ExpressionResult) Leaves() []*ExprLicense {
	if r.Root == nil {
		return nil
	}
	var out []*ExprLicense
	walkLeaves(r.Root, &out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// needsParens decides whether a child node needs to be parenthesized when
// rendered inside a parent compound with the given operator. Rule: a Compound
// child needs parens iff its operator differs from the parent's. Leaves never
// need parens — WITH binds tighter than AND/OR per SPDX precedence.
func needsParens(child *ExprNode, parentOp string) bool {
	return child != nil && child.Compound != nil && child.Compound.Operator != parentOp
}

// walkLeaves appends every ExprLicense leaf reachable from n to out, in
// reading order. Mirrors String()'s panic invariants: zero-valued and
// nil-operand nodes are programmer errors and surface immediately.
func walkLeaves(n *ExprNode, out *[]*ExprLicense) {
	if n == nil {
		return
	}
	if n.License != nil {
		*out = append(*out, n.License)
		return
	}
	if n.Compound == nil {
		panic("licenses.ExprNode: zero-valued node during AST walk")
	}
	for _, c := range n.Compound.Operands {
		if c == nil {
			panic("licenses.ExprNode: nil operand in Compound during AST walk")
		}
		walkLeaves(c, out)
	}
}

// baseRaw returns the substring of Raw before any "+" suffix and " WITH "
// clause — useful as a String() fallback when normalization failed. Uses the
// already-parsed Exception field rather than re-scanning Raw for "WITH",
// avoiding duplicate parsing logic that could drift from lex().
func (l *ExprLicense) baseRaw() string {
	s := l.Raw
	if l.Exception != "" {
		suffix := " " + opWITH + " " + l.Exception
		s = strings.TrimSuffix(s, suffix)
	}
	if isOrLaterSuffix(s) {
		s = s[:len(s)-1]
	}
	return s
}

// --- Lexer -----------------------------------------------------------------

type tokenKind int

const (
	tokIdent tokenKind = iota
	tokOR
	tokAND
	tokWITH
	tokLParen
	tokRParen
)

type token struct {
	kind tokenKind
	text string // for tokIdent only; canonical form (single-spaced, original case)
}

// lex tokenizes the input. Whitespace separates raw chunks; "(" and ")" are
// always their own chunks. Each non-paren chunk that case-folds to "OR" /
// "AND" / "WITH" becomes a reserved token; everything else accumulates into
// IDENT tokens, with adjacent IDENTs merged into a single IDENT separated by
// one space (so free-text license names like "Apache License 2.0" survive as
// one identifier).
func lex(s string) []token {
	chunks := splitChunks(s)
	if len(chunks) == 0 {
		return nil
	}

	var out []token
	var pending strings.Builder
	flushIdent := func() {
		if pending.Len() > 0 {
			out = append(out, token{kind: tokIdent, text: pending.String()})
			pending.Reset()
		}
	}
	for _, c := range chunks {
		switch {
		case c == "(":
			flushIdent()
			out = append(out, token{kind: tokLParen})
		case c == ")":
			flushIdent()
			out = append(out, token{kind: tokRParen})
		case strings.EqualFold(c, opOR):
			flushIdent()
			out = append(out, token{kind: tokOR})
		case strings.EqualFold(c, opAND):
			flushIdent()
			out = append(out, token{kind: tokAND})
		case strings.EqualFold(c, opWITH):
			flushIdent()
			out = append(out, token{kind: tokWITH})
		default:
			if pending.Len() > 0 {
				pending.WriteByte(' ')
			}
			pending.WriteString(c)
		}
	}
	flushIdent()
	return out
}

// splitChunks splits s on whitespace, treating "(" and ")" as their own chunks
// even when adjacent to non-whitespace characters. Empty results are dropped.
func splitChunks(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '(' || r == ')':
			flush()
			out = append(out, string(r))
		case unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// --- Parser ---------------------------------------------------------------

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() (token, bool) {
	if p.pos >= len(p.tokens) {
		return token{}, false
	}
	return p.tokens[p.pos], true
}

func (p *parser) consume() (token, bool) {
	t, ok := p.peek()
	if ok {
		p.pos++
	}
	return t, ok
}

// parseExpression: expression = and_expr (OR and_expr)*
func (p *parser) parseExpression() *ExprNode {
	left := p.parseAnd()
	if !p.acceptKind(tokOR) {
		return left
	}
	operands := []*ExprNode{}
	if left != nil {
		operands = append(operands, left)
	}
	for {
		right := p.parseAnd()
		if right != nil {
			operands = append(operands, right)
		}
		if !p.acceptKind(tokOR) {
			break
		}
	}
	return finalizeCompound(opOR, operands)
}

// parseAnd: and_expr = with_expr (AND with_expr)*
func (p *parser) parseAnd() *ExprNode {
	left := p.parseWith()
	if !p.acceptKind(tokAND) {
		return left
	}
	operands := []*ExprNode{}
	if left != nil {
		operands = append(operands, left)
	}
	for {
		right := p.parseWith()
		if right != nil {
			operands = append(operands, right)
		}
		if !p.acceptKind(tokAND) {
			break
		}
	}
	return finalizeCompound(opAND, operands)
}

// parseWith: with_expr = primary (WITH ident)?
//
// Two recovery paths for malformed inputs:
//
//   - WITH is not followed by an identifier (`Apache-2.0 WITH`): the WITH
//     token is consumed but not applied; the primary is returned untouched.
//   - WITH follows a Compound primary (`(A OR B) WITH X`): SPDX strict
//     grammar forbids this, but real-world Maven / ClearlyDefined data ships
//     it. We distribute the exception to every leaf reachable from the
//     compound — set-equivalent to the most generous interpretation
//     ("either license, both with the same exception"). Each leaf that
//     already has its own exception is left untouched.
func (p *parser) parseWith() *ExprNode {
	primary := p.parsePrimary()
	if !p.acceptKind(tokWITH) {
		return primary
	}
	exceptionTok, ok := p.peek()
	if !ok || exceptionTok.kind != tokIdent {
		return primary
	}
	p.consume()
	if primary == nil {
		return nil
	}
	if primary.License != nil {
		attachException(primary.License, exceptionTok.text)
		return primary
	}
	distributeException(primary, exceptionTok.text)
	return primary
}

// attachException sets the exception on a leaf that does not yet have one,
// updating Raw to include the WITH clause. A leaf that already carries an
// exception (e.g., chained "A WITH B WITH C") keeps its first exception —
// SPDX 2.1+ forbids chains so additional WITHs are silently dropped.
func attachException(l *ExprLicense, exception string) {
	if l.Exception != "" {
		return
	}
	l.Exception = exception
	l.Raw = l.Raw + " " + opWITH + " " + exception
}

// distributeException applies the exception to every leaf reachable from n.
// Used when a WITH clause attaches to a Compound primary; mathematically
// equivalent to wrapping each leaf with its own WITH clause. Leaves that
// already carry an exception retain theirs.
func distributeException(n *ExprNode, exception string) {
	if n == nil {
		return
	}
	if n.License != nil {
		attachException(n.License, exception)
		return
	}
	if n.Compound == nil {
		return
	}
	for _, c := range n.Compound.Operands {
		distributeException(c, exception)
	}
}

// parsePrimary: primary = ident ['+'] | '(' expression ')'
//
// Stray operator tokens (OR / AND / WITH) at the primary position are silently
// skipped — this preserves the v1 contract that malformed edge-operator inputs
// like "OR Apache-2.0", "Apache-2.0 OR", and "Apache-2.0 OR OR MIT" still
// yield as much information as the parser can recover.
func (p *parser) parsePrimary() *ExprNode {
	for {
		t, ok := p.peek()
		if !ok {
			return nil
		}
		switch t.kind {
		case tokLParen:
			p.consume()
			inner := p.parseExpression()
			_ = p.acceptKind(tokRParen) // tolerate missing RPAREN
			return inner
		case tokIdent:
			p.consume()
			return makeLeaf(t.text)
		case tokOR, tokAND, tokWITH:
			p.consume()
			continue
		default: // tokRParen — let the surrounding LParen handler consume it
			return nil
		}
	}
}

func (p *parser) acceptKind(kind tokenKind) bool {
	t, ok := p.peek()
	if !ok || t.kind != kind {
		return false
	}
	p.pos++
	return true
}

// finalizeCompound returns a normalized compound node, flattening same-
// operator children into the parent so "A OR B OR C" — and equivalently
// "A OR (B OR C)" via paren grouping — both produce a single Compound with
// three operands. Set-equivalent in legal terms; matches SPDX renderer
// convention.
//
//   - 0 operands → nil
//   - 1 operand → that operand directly (collapse degenerate compound)
//   - 2+ operands → ExprCompound{Operator: op, Operands: flattened}
func finalizeCompound(op string, operands []*ExprNode) *ExprNode {
	flat := make([]*ExprNode, 0, len(operands))
	for _, o := range operands {
		if o == nil {
			continue
		}
		if o.Compound != nil && o.Compound.Operator == op {
			flat = append(flat, o.Compound.Operands...)
			continue
		}
		flat = append(flat, o)
	}
	switch len(flat) {
	case 0:
		return nil
	case 1:
		return flat[0]
	default:
		return &ExprNode{Compound: &ExprCompound{Operator: op, Operands: flat}}
	}
}

// makeLeaf builds a leaf node from a license-id text. Recognises a trailing
// "+" (or-later) suffix, but only when exactly one "+" terminates a non-empty
// base — bare "+" and "++" are treated as part of the identifier and left to
// fail normalization, since SPDX has no notion of such forms.
func makeLeaf(raw string) *ExprNode {
	base := raw
	orLater := false
	if isOrLaterSuffix(base) {
		base = base[:len(base)-1]
		orLater = true
	}
	norm := Normalize(base)
	id := ""
	if norm.SPDX {
		id = norm.CanonicalID
	}
	return &ExprNode{License: &ExprLicense{
		Raw:           raw,
		Identifier:    id,
		Normalization: norm,
		OrLater:       orLater,
	}}
}

// isOrLaterSuffix reports whether s ends with exactly one "+" attached to a
// non-empty base (e.g. "Apache-2.0+"). Returns false for bare "+", "++", or
// the empty string.
func isOrLaterSuffix(s string) bool {
	n := len(s)
	if n < 2 || s[n-1] != '+' {
		return false
	}
	return s[n-2] != '+'
}

// Compile-time guard that the package's public types remain stringer-
// compatible. The fmt import is otherwise unused; this also catches the case
// where a future refactor accidentally drops the String method receiver.
var _ fmt.Stringer = (*ExprNode)(nil)
var _ fmt.Stringer = (*ExprLicense)(nil)
