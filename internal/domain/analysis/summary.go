package analysis

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxSummaryLen is the maximum rune length of Repository.Summary.
// Exported for consumers that want to validate ingest-side values against the same cap.
const MaxSummaryLen = 200

// summaryEllipsis is appended when truncation occurs; counted toward MaxSummaryLen.
const summaryEllipsis = "…"

// NormalizeSummary returns a trimmed, single-line, rune-count-capped form of raw.
//
// Rules:
//   - Trim leading/trailing whitespace.
//   - Collapse any run of whitespace (spaces, tabs, newlines) to a single space.
//   - Truncate to MaxSummaryLen runes; when truncated, the last rune is replaced with "…".
//
// The helper does NOT perform first-sentence extraction — call FirstSentence first when the
// source is known to be multi-paragraph (e.g. Maven POM <description>). See issue #316.
func NormalizeSummary(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	inSpace := false
	for _, r := range trimmed {
		if unicode.IsSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}
	collapsed := b.String()
	if utf8.RuneCountInString(collapsed) <= MaxSummaryLen {
		return collapsed
	}
	// Truncate at (MaxSummaryLen - 1) runes and append an ellipsis so the final rune count
	// is exactly MaxSummaryLen.
	runes := []rune(collapsed)
	return strings.TrimRightFunc(string(runes[:MaxSummaryLen-1]), unicode.IsSpace) + summaryEllipsis
}

// FirstSentence returns the first sentence-like fragment of s.
//
// Heuristic boundaries (first match wins):
//  1. The first blank-line paragraph break ("\n\n" after any intervening whitespace).
//  2. A terminal sentence punctuation (.!?) followed by whitespace or end of input —
//     provided the preceding rune is not a digit (avoids splitting "v3.0 released").
//
// Returns the original (trimmed) string when no boundary is found. Intended for multi-paragraph
// sources (Maven POM <description>, README-style text) before passing to NormalizeSummary.
// Currently has no production caller; ships ahead of the Maven POM Summary wiring deferred
// from issue #316 so the helper is unit-testable in isolation.
func FirstSentence(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}

	if idx := paragraphBreak(trimmed); idx >= 0 {
		trimmed = strings.TrimSpace(trimmed[:idx])
	}

	if idx := sentenceBreak(trimmed); idx >= 0 {
		return strings.TrimSpace(trimmed[:idx])
	}
	return trimmed
}

// paragraphBreak returns the byte index of the first paragraph-style break (two or more
// consecutive newlines, optionally separated by spaces/tabs) in s. Returns -1 if none found.
func paragraphBreak(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != '\n' {
			continue
		}
		j := i + 1
		for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r') {
			j++
		}
		if j < len(s) && s[j] == '\n' {
			return i
		}
	}
	return -1
}

// sentenceBreak returns the byte index immediately after a terminal punctuation rune (.!?)
// that is followed by whitespace (or end of input) and not preceded by a digit. Returns -1
// when no such boundary is found.
func sentenceBreak(s string) int {
	var prev rune
	for i, r := range s {
		switch r {
		case '.', '!', '?':
			// Avoid splitting numeric fragments like "v3.0" or "1.5 released".
			if unicode.IsDigit(prev) {
				prev = r
				continue
			}
			next := i + utf8.RuneLen(r)
			if next >= len(s) {
				return next
			}
			nr, _ := utf8.DecodeRuneInString(s[next:])
			if unicode.IsSpace(nr) {
				return next
			}
		}
		prev = r
	}
	return -1
}
