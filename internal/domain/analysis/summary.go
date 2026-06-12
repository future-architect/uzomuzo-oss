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
	return truncateSummary(collapsed)
}

// truncateSummary truncates s to MaxSummaryLen runes with an ellipsis by iterating
// runes up to the cutoff point — avoids materializing the full []rune slice for
// large inputs where only the first 199 runes are needed.
func truncateSummary(s string) string {
	runeCount := 0
	cutoff := len(s)
	for i := range s {
		if runeCount == MaxSummaryLen-1 {
			cutoff = i
			break
		}
		runeCount++
	}
	return strings.TrimRightFunc(s[:cutoff], unicode.IsSpace) + summaryEllipsis
}
