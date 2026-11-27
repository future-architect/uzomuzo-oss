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
			break
		}
		found = true
		// When blocks name different languages, the latest in the order
		// langJavaScript < langTypeScript < langTSX wins: TypeScript also
		// parses plain JavaScript, and TSX adds JSX but rejects `<T>x` casts,
		// so only a file mixing `lang="tsx"` with such casts parses with errors.
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
// Matched in lower case only: the components `<Title>` or `<Textarea>` are
// not these elements. A Svelte `<title>{x}</title>` expression is skipped too,
// which is fine because diet reads no template expressions.
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
	// tagUnclosed is set once a tag has no `>` the quote- and brace-aware
	// reader can find. Later markup `<` is then plain text, so each one does
	// not rescan to the end of input.
	tagUnclosed bool
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
	if sc.tagUnclosed {
		return at + 1
	}
	for k, name := range rawTextTags {
		if !hasTagName(src, at+1, name, false) {
			continue
		}
		end, selfClosing := sc.openingTagEnd(at + 1 + len(name))
		if end < 0 {
			sc.tagUnclosed = true
			return at + 1
		}
		if selfClosing || sc.rawUnclosed[k] {
			return end + 1
		}
		for j := end + 1; j+2+len(name) <= len(src); j++ {
			if src[j] == '<' && src[j+1] == '/' && hasTagName(src, j+2, name, false) {
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
		sc.tagUnclosed = true
		return at + 1
	}
	return end + 1
}

// skipExpression returns the index after the template expression starting
// at src[at] == '{': `{{ ... }}` in Vue (ending at the first `}}`, as Vue's
// own parser does), a brace-matched `{ ... }` in Svelte. ok is false when
// src[at] starts no expression.
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
// reading nested braces, JavaScript string literals, comments and regular
// expression literals, so a `}` inside `'}'`, `/* } */` or `/[}]/` does not
// end the expression. ok is false when no match exists, and every later call
// then reports false at once.
func (sc *markupScanner) skipBraces(at int) (next int, ok bool) {
	if sc.bracesUnbalanced {
		return 0, false
	}
	src := sc.src
	depth := 0
	// prev is the last byte outside whitespace, comments and literals; a
	// literal is recorded as 'a'. It decides whether `/` starts a regular
	// expression or divides.
	var prev byte
	for i := at; i < len(src); i++ {
		b := src[i]
		end := i
		switch {
		case b == '\'' || b == '"' || b == '`':
			end = skipQuoted(src, i)
			prev = 'a'
		case b == '/' && i+1 < len(src) && src[i+1] == '/':
			if nl := bytes.IndexByte(src[i:], '\n'); nl >= 0 {
				end = i + nl
			} else {
				end = -1
			}
		case b == '/' && i+1 < len(src) && src[i+1] == '*':
			if e := bytes.Index(src[i+2:], []byte("*/")); e >= 0 {
				end = i + 2 + e + 1
			} else {
				end = -1
			}
		case b == '/' && regexCanStart(prev):
			if e := skipRegex(src, i); e >= 0 {
				end, prev = e, 'a'
			} else {
				prev = b
			}
		case b == '{':
			depth++
			prev = b
		case b == '}':
			depth--
			if depth == 0 {
				return i + 1, true
			}
			prev = b
		case isTagWhitespace(b):
		default:
			prev = b
		}
		if end < 0 {
			break
		}
		i = end
	}
	sc.bracesUnbalanced = true
	return 0, false
}

// skipQuoted returns the index of the quote that closes the string literal
// opening at src[at], or -1. Template literal `${}` parts are read as text.
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

// regexCanStart reports whether a `/` after prev starts a regular expression
// rather than dividing. Keywords such as `return` are not tracked: a regex
// after one is read as division, which matters only when it holds a brace.
func regexCanStart(prev byte) bool {
	return prev == 0 || bytes.IndexByte([]byte("({[,;:=!&|?+-*%<>~^"), prev) >= 0
}

// skipRegex returns the index of the `/` that closes the regular expression
// literal opening at src[at], or -1 when the line ends first. A `/` inside a
// `[...]` class does not close it.
func skipRegex(src []byte, at int) int {
	inClass := false
	for i := at + 1; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				return i
			}
		case '\n', '\r':
			return -1
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

func (sc *markupScanner) openingTagEnd(start int) (int, bool) {
	src := sc.src
	var quote byte
	for i := start; i < len(src); i++ {
		b := src[i]
		if quote != 0 {
			if b == quote {
				quote = 0
			}
			continue
		}
		if b == '\'' || b == '"' {
			quote = b
			continue
		}
		if b == '{' && !sc.vue {
			if next, ok := sc.skipBraces(i); ok {
				i = next - 1
			}
			continue
		}
		if b == '>' {
			for j := i - 1; j >= start; j-- {
				if isTagWhitespace(src[j]) {
					continue
				}
				return i, src[j] == '/'
			}
			return i, false
		}
	}
	return -1, false
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
