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
		if src[i] != '<' || !asciiEqualFold(src[i:i+7], "<script") ||
			(i+7 < len(src) && !isTagSeparator(src[i+7])) {
			i++
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
			if src[j] == '<' && asciiEqualFold(src[j:j+8], "</script") &&
				(j+8 == len(src) || isTagSeparator(src[j+8])) {
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
		masked[contentEnd] = '\n'
		i = contentEnd + 8
	}
	return masked, language, found
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
