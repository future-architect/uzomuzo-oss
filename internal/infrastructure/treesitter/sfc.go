//go:build cgo

package treesitter

import "bytes"

// scriptContents keeps script bodies at their original byte offsets so that
// tree-sitter sees no imports from component markup or styles. vue selects
// Vue's `{{ }}` interpolation; otherwise Svelte's `{ }` expressions apply.
func scriptContents(src []byte, vue bool) ([]byte, langID, bool) {
	masked := make([]byte, len(src))
	for i, b := range src {
		if b == '\n' || b == '\r' {
			masked[i] = b
		} else {
			masked[i] = ' '
		}
	}

	sc := markupScanner{src: src, vue: vue}
	sc.lastQuote = [2]int{bytes.LastIndexByte(src, '\'') + 1, bytes.LastIndexByte(src, '"') + 1}
	language := langJavaScript
	found := false
	for i := 0; i+7 <= len(src); {
		// Vue `{{ }}` and Svelte `{ }` hold JavaScript, where `'<!--'` or
		// `"<script>"` are just strings, not markup.
		if src[i] == '{' {
			if next, ok := sc.skipExpression(i); ok {
				i = next
				continue
			}
			i++
			continue
		}
		// Markup comments may hold disabled script blocks; skip them whole.
		// An unclosed one is skipped by its opener only: reading on risks
		// counting a commented-out use, but stopping would drop every later
		// script block, and a missed use is the worse error for diet.
		if src[i] == '<' && bytes.HasPrefix(src[i:], []byte("<!--")) {
			end := -1
			if !sc.commentUnclosed {
				end = bytes.Index(src[i+4:], []byte("-->"))
			}
			if end < 0 {
				sc.commentUnclosed = true
				i += 4
				continue
			}
			i += 4 + end + 3
			continue
		}
		if src[i] != '<' {
			i++
			continue
		}
		if !isTagNamed(src, i+1, "script") {
			i = sc.skipMarkupTag(i)
			continue
		}

		end, selfClosing := sc.openingTagEnd(i + 7)
		if end < 0 {
			i++ // a broken `<script` tag is read on as text
			continue
		}
		found = true
		// All blocks are parsed with one grammar: the latest named in the
		// order langJavaScript < langTypeScript < langTSX. TypeScript parses
		// plain JavaScript, but not JSX; TSX adds JSX but rejects `<T>x`
		// casts. A file whose blocks mix syntax only one grammar accepts (JSX
		// in a plain block next to `lang="ts"`, or `<T>x` next to `lang="tsx"`)
		// parses with errors, and uses in the broken part can be missed.
		if blockLanguage := scriptLanguage(src[i+7 : end]); blockLanguage > language {
			language = blockLanguage
		}
		if selfClosing {
			i = end + 1
			continue
		}

		contentStart := end + 1
		contentEnd := len(src)
		for j := contentStart; j+8 <= len(src); j++ {
			if src[j] == '<' && src[j+1] == '/' && isTagNamed(src, j+2, "script") {
				contentEnd = j
				break
			}
		}
		copy(masked[contentStart:contentEnd], src[contentStart:contentEnd])
		if contentEnd == len(src) {
			break
		}
		// The closing tag's first two bytes become "\n;": the newline ends a
		// `//` comment at the block's end, and the semicolon keeps
		// `export const x = p` and a next block starting with `[1]` from
		// joining into one expression. Byte offsets stay the same; line
		// numbers after the block shift by one, which is harmless because
		// coupling analysis reads no line or column positions.
		masked[contentEnd] = '\n'
		masked[contentEnd+1] = ';'
		i = contentEnd + 8
	}
	return masked, language, found
}

// rawTextTags are elements whose body is not scanned for `<script>` or `<!--`.
// The opening tag counts only in lower case, as the Vue and Svelte compilers
// treat any other spelling (`<Title>`, `<TITLE>`) as a component; the closing
// tag is matched in any case, as HTML does. The body ends at the first
// closing tag, even one written inside a template expression such as
// `<title>{"</title>"}</title>`; reading expressions there misread Vue's
// v-pre text and braces in regular expressions, so it is not attempted. See
// docs/diet.md for what this can cost.
var rawTextTags = [...]string{"style", "textarea", "title"}

// markupScanner reads component markup around script blocks.
type markupScanner struct {
	src []byte
	// vue is set for .vue files. Vue writes expressions in text as `{{ }}`
	// and in attributes only inside quotes, so a single `{` there is text.
	// Svelte uses `{ }` in both places, and a literal `{` must be escaped.
	vue bool
	// vueUnclosed is set once a `{{` has no `}}` after it; later ones cannot
	// have one either.
	vueUnclosed bool
	// commentUnclosed is set once a `<!--` has no `-->` after it; later ones
	// cannot have one either.
	commentUnclosed bool
	// lastQuote[q] is one past the position of the last `'` (q == 0) or `"`
	// (q == 1) in src, or 0 when there is none. A quote at or after it has
	// no closing quote, so the tag holding it is read without quotes; see
	// openingTagEnd.
	lastQuote [2]int
	// rawUnclosed[k] is set once rawTextTags[k] has no closing tag after it.
	rawUnclosed [len(rawTextTags)]bool
	// bracesUnbalanced is set once a `{` has no matching `}`. From then on
	// braces are plain text; retrying from every later `{` would make a file
	// of unmatched braces take quadratic time.
	bracesUnbalanced bool
}

// skipMarkupTag returns where scanning resumes after the non-script markup
// at src[at] == '<'. Attribute values are read quote- and brace-aware, so
// `title="<!--"` or `title={a > b}` cannot open a comment or end the tag, and
// the body of a raw-text tag such as style is skipped whole. Broken markup
// (a tag with no `>`, a raw-text tag with no closing tag) is read on as text
// rather than ending the scan, so later script blocks are still found.
func (sc *markupScanner) skipMarkupTag(at int) int {
	src := sc.src
	for k, name := range rawTextTags {
		if !hasTagName(src, at+1, name, false) {
			continue
		}
		end, selfClosing := sc.openingTagEnd(at + 1 + len(name))
		if end < 0 {
			return at + 1
		}
		if selfClosing || sc.rawUnclosed[k] {
			return end + 1
		}
		for j := end + 1; j+2+len(name) <= len(src); j++ {
			if src[j] == '<' && src[j+1] == '/' && hasTagName(src, j+2, name, true) {
				return j + 2 + len(name)
			}
		}
		sc.rawUnclosed[k] = true
		return end + 1
	}
	nameStart := at + 1
	if nameStart < len(src) && src[nameStart] == '/' {
		nameStart++
	}
	if nameStart >= len(src) || !isASCIILetter(src[nameStart]) {
		return at + 1 // a bare '<', such as `a < b` in text
	}
	end, _ := sc.openingTagEnd(nameStart)
	if end < 0 {
		return at + 1
	}
	return end + 1
}

// skipExpression returns the index after the template expression starting
// at src[at] == '{': `{{ ... }}` in Vue (ending at the first `}}`, as Vue's
// own parser does), a brace-matched `{ ... }` in Svelte. ok is false when
// src[at] starts no expression. Vue's v-pre is not tracked: following
// element nesting is beyond this scanner, so `<p v-pre>{{</p>` is read as
// an interpolation running to the next `}}`, possibly inside a script block;
// see docs/diet.md.
func (sc *markupScanner) skipExpression(at int) (next int, ok bool) {
	if !sc.vue {
		return sc.skipBraces(at)
	}
	if sc.vueUnclosed || !bytes.HasPrefix(sc.src[at:], []byte("{{")) {
		return 0, false
	}
	end := bytes.Index(sc.src[at+2:], []byte("}}"))
	if end < 0 {
		sc.vueUnclosed = true
		return 0, false
	}
	return at + 2 + end + 2, true
}

// skipBraces returns the index after the `}` that matches src[at] == '{',
// reading nested braces, JavaScript string literals and comments, so a `}`
// inside `'}'` or `/* } */` does not end the expression. ok is false when no
// match exists, and every later call then reports false at once.
//
// Regular expression literals are not recognized: telling `/x/` from
// division needs the previous token (`n++ / 2` divides, `typeof /x/` does
// not), and guessing from one byte misread Svelte's `{/if}` as a regex. So a
// `}` or quote inside a regex in a template expression can end or extend the
// expression; see docs/diet.md.
func (sc *markupScanner) skipBraces(at int) (next int, ok bool) {
	if sc.bracesUnbalanced {
		return 0, false
	}
	if end := braceEnd(sc.src, at); end >= 0 {
		return end, true
	}
	sc.bracesUnbalanced = true
	return 0, false
}

// braceEnd returns the index after the `}` that matches src[at] == '{', read
// as skipBraces describes, or -1. Inside a template literal, each `${ }` part
// is read as an expression again, so a backtick or `}` in `outer ${`}`}`
// does not end the literal. The nesting of expressions and template literals
// is kept on an explicit stack rather than in Go recursion, so every depth is
// read the same way; the stack never grows past the input length, and each
// byte is read once.
func braceEnd(src []byte, at int) int {
	// braces[k] is how many `{` are still open in the k-th open expression,
	// the outermost first. inTemplate[k] is set while that expression is
	// inside a template literal, which ends at its backtick or opens the next
	// expression at `${`.
	braces := []int{1}
	inTemplate := []bool{false}
	for i := at + 1; i < len(src); i++ {
		top := len(braces) - 1
		b := src[i]
		if inTemplate[top] {
			switch {
			case b == '\\':
				i++
			case b == '`':
				inTemplate[top] = false
			case b == '$' && i+1 < len(src) && src[i+1] == '{':
				braces = append(braces, 1)
				inTemplate = append(inTemplate, false)
				i++
			}
			continue
		}
		switch {
		case b == '\'' || b == '"':
			end := skipQuoted(src, i)
			if end < 0 {
				return -1
			}
			i = end
		case b == '`':
			inTemplate[top] = true
		case b == '/' && i+1 < len(src) && src[i+1] == '/':
			end := lineEnd(src, i+2)
			if end < 0 {
				return -1
			}
			i = end
		case b == '/' && i+1 < len(src) && src[i+1] == '*':
			end := bytes.Index(src[i+2:], []byte("*/"))
			if end < 0 {
				return -1
			}
			i += 2 + end + 1
		case b == '{':
			braces[top]++
		case b == '}':
			braces[top]--
			if braces[top] > 0 {
				continue
			}
			if top == 0 {
				return i + 1
			}
			// The `${ }` part ends; reading goes back into its template literal.
			braces, inTemplate = braces[:top], inTemplate[:top]
		}
	}
	return -1
}

// skipQuoted returns the index of the quote that closes the '...' or "..."
// string literal opening at src[at], or -1.
func skipQuoted(src []byte, at int) int {
	quote := src[at]
	for i := at + 1; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case quote:
			return i
		}
	}
	return -1
}

// lineEnd returns the index of the first JavaScript line terminator at or
// after src[from] (LF, CR, U+2028 or U+2029), or -1.
func lineEnd(src []byte, from int) int {
	for i := from; i < len(src); i++ {
		switch src[i] {
		case '\n', '\r':
			return i
		case 0xE2:
			if i+2 < len(src) && src[i+1] == 0x80 && (src[i+2] == 0xA8 || src[i+2] == 0xA9) {
				return i
			}
		}
	}
	return -1
}

// isTagNamed reports whether src[at:] starts with name (ASCII
// case-insensitive) followed by a tag separator or the end of input, so
// `<scripts>` and `</scripture` do not match "script".
func isTagNamed(src []byte, at int, name string) bool {
	return hasTagName(src, at, name, true)
}

// hasTagName is isTagNamed with the case rule chosen by foldCase.
func hasTagName(src []byte, at int, name string, foldCase bool) bool {
	if at+len(name) > len(src) {
		return false
	}
	got := src[at : at+len(name)]
	if (foldCase && !asciiEqualFold(got, name)) || (!foldCase && string(got) != name) {
		return false
	}
	return at+len(name) == len(src) || isTagSeparator(src[at+len(name)])
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// openingTagEnd returns the index of the `>` that ends the tag whose name or
// attributes start at src[start], or -1, and whether the tag is self-closing.
// Attribute values are read quote- and brace-aware. A `<` outside quotes and
// braces breaks the tag (returns -1): it is not valid inside one, and reading
// past it could swallow the next `<script` tag. A quote with no closing quote
// after it (`<p don't`), known from lastQuote without reading ahead, makes
// that tag be reread from its start by plainTagEnd, so each tag is read at
// most twice; later tags are still read with quotes. A quote can still pair
// with an unrelated one later in the file, such as a `"` inside a comment, so
// a broken tag can hide a later script. Braces inside a quoted value are not
// read, so in Svelte's `title="{ '"' }"` the `"` in the string ends the
// value; see docs/diet.md.
func (sc *markupScanner) openingTagEnd(start int) (int, bool) {
	src := sc.src
	for i := start; i < len(src); i++ {
		b := src[i]
		if b == '\'' || b == '"' {
			q := 0
			if b == '"' {
				q = 1
			}
			if i+1 >= sc.lastQuote[q] {
				return sc.plainTagEnd(start)
			}
			i += 1 + bytes.IndexByte(src[i+1:], b)
			continue
		}
		if b == '{' && !sc.vue {
			if next, ok := sc.skipBraces(i); ok {
				i = next - 1
			}
			continue
		}
		if b == '<' {
			return -1, false
		}
		if b == '>' {
			return i, isSelfClosing(src, start, i)
		}
	}
	return -1, false
}

// plainTagEnd is openingTagEnd without quote or brace reading: the tag ends
// at the first `>`, and a `<` before it means the tag is broken.
func (sc *markupScanner) plainTagEnd(start int) (int, bool) {
	k := bytes.IndexAny(sc.src[start:], "<>")
	if k < 0 || sc.src[start+k] == '<' {
		return -1, false
	}
	return start + k, isSelfClosing(sc.src, start, start+k)
}

// isSelfClosing reports whether the last byte before src[gt] == '>' that is
// not whitespace, within the tag starting at src[start], is `/`.
func isSelfClosing(src []byte, start, gt int) bool {
	for j := gt - 1; j >= start; j-- {
		if !isTagWhitespace(src[j]) {
			return src[j] == '/'
		}
	}
	return false
}

func scriptLanguage(attrs []byte) langID {
	language := langJavaScript
	for i := 0; i < len(attrs); {
		if !isTagWhitespace(attrs[i]) {
			i++
			continue
		}
		for i < len(attrs) && isTagWhitespace(attrs[i]) {
			i++
		}
		start := i
		for i < len(attrs) && !isTagWhitespace(attrs[i]) && attrs[i] != '=' && attrs[i] != '/' {
			i++
		}
		if i == start {
			i++
			continue
		}
		name := attrs[start:i]
		j := i
		for j < len(attrs) && isTagWhitespace(attrs[j]) {
			j++
		}
		if j >= len(attrs) || attrs[j] != '=' {
			continue
		}
		i = j + 1
		for i < len(attrs) && isTagWhitespace(attrs[i]) {
			i++
		}
		if i >= len(attrs) {
			break
		}
		var value []byte
		if attrs[i] == '\'' || attrs[i] == '"' {
			quote := attrs[i]
			i++
			start = i
			for i < len(attrs) && attrs[i] != quote {
				i++
			}
			value = attrs[start:i]
			if i < len(attrs) {
				i++
			}
		} else {
			start = i
			for i < len(attrs) && !isTagWhitespace(attrs[i]) {
				i++
			}
			value = attrs[start:i]
			if i == len(attrs) && len(value) > 0 && value[len(value)-1] == '/' {
				value = value[:len(value)-1]
			}
		}
		if asciiEqualFold(name, "lang") {
			switch {
			case asciiEqualFold(value, "tsx"):
				language = langTSX
			case asciiEqualFold(value, "ts"), asciiEqualFold(value, "typescript"):
				language = langTypeScript
			}
		}
	}
	return language
}

func asciiEqualFold(value []byte, literal string) bool {
	if len(value) != len(literal) {
		return false
	}
	for i, b := range value {
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if b != literal[i] {
			return false
		}
	}
	return true
}

func isTagSeparator(b byte) bool {
	return isTagWhitespace(b) || b == '>' || b == '/'
}

func isTagWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}
