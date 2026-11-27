//go:build cgo

package treesitter

import "bytes"

// scriptContents keeps script bodies at their original byte offsets so that
// tree-sitter sees no imports from component markup or styles.
func scriptContents(src []byte) ([]byte, langID, bool) {
	masked := make([]byte, len(src))
	for i, b := range src {
		if b == '\n' || b == '\r' {
			masked[i] = b
		} else {
			masked[i] = ' '
		}
	}

	language := langJavaScript
	found := false
	for i := 0; i+7 <= len(src); {
		// Markup comments may hold disabled script blocks; skip them whole.
		if src[i] == '<' && bytes.HasPrefix(src[i:], []byte("<!--")) {
			end := bytes.Index(src[i+4:], []byte("-->"))
			if end < 0 {
				break
			}
			i += 4 + end + 3
			continue
		}
		if src[i] != '<' {
			i++
			continue
		}
		if !isTagNamed(src, i+1, "script") {
			next, ok := skipMarkupTag(src, i)
			if !ok {
				break
			}
			i = next
			continue
		}

		end, selfClosing := openingTagEnd(src, i+7)
		if end < 0 {
			break
		}
		found = true
		// Relies on langJavaScript < langTypeScript < langTSX: each later
		// grammar also parses the earlier ones, so one mixed file never fails.
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
		// A newline in place of each tag's edge byte ends a `//` comment at the
		// block's end, so it cannot run into the next block's code.
		masked[end] = '\n'
		if contentEnd == len(src) {
			break
		}
		// The semicolon keeps `export const x = p` and a next block starting
		// with `[1]` from joining into one expression.
		masked[contentEnd] = '\n'
		masked[contentEnd+1] = ';'
		i = contentEnd + 8
	}
	return masked, language, found
}

// skipMarkupTag returns where scanning resumes after the non-script markup
// at src[at] == '<'. Attribute values are read quote-aware, so `title="<!--"`
// cannot open a comment, and a style body is skipped whole, so CSS text such
// as `content: "<script>"` is never read as code. ok is false when the rest
// of the file cannot hold a script block (an unterminated tag or style).
func skipMarkupTag(src []byte, at int) (next int, ok bool) {
	if isTagNamed(src, at+1, "style") {
		end, selfClosing := openingTagEnd(src, at+len("<style"))
		if end < 0 {
			return 0, false
		}
		if selfClosing {
			return end + 1, true
		}
		for j := end + 1; j+len("</style") <= len(src); j++ {
			if src[j] == '<' && src[j+1] == '/' && isTagNamed(src, j+2, "style") {
				return j + len("</style"), true
			}
		}
		return 0, false
	}
	nameStart := at + 1
	if nameStart < len(src) && src[nameStart] == '/' {
		nameStart++
	}
	if nameStart >= len(src) || !isASCIILetter(src[nameStart]) {
		return at + 1, true // a bare '<', such as `a < b` in text
	}
	end, _ := openingTagEnd(src, nameStart)
	if end < 0 {
		return 0, false
	}
	return end + 1, true
}

// isTagNamed reports whether src[at:] starts with name (ASCII
// case-insensitive) followed by a tag separator or the end of input, so
// `<scripts>` and `</scripture` do not match "script".
func isTagNamed(src []byte, at int, name string) bool {
	if at+len(name) > len(src) || !asciiEqualFold(src[at:at+len(name)], name) {
		return false
	}
	return at+len(name) == len(src) || isTagSeparator(src[at+len(name)])
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func openingTagEnd(src []byte, start int) (int, bool) {
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
