package analysis

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormalizeSummary(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "only_whitespace", in: "   \t\n  ", want: ""},
		{name: "trim_and_collapse", in: "  Hello\n\tworld   foo  ", want: "Hello world foo"},
		{name: "single_line_intact", in: "Already short.", want: "Already short."},
		{name: "ascii_under_cap", in: strings.Repeat("a", 199), want: strings.Repeat("a", 199)},
		{name: "ascii_at_cap", in: strings.Repeat("a", MaxSummaryLen), want: strings.Repeat("a", MaxSummaryLen)},
		{
			name: "ascii_over_cap_truncates_with_ellipsis",
			in:   strings.Repeat("a", MaxSummaryLen+50),
			want: strings.Repeat("a", MaxSummaryLen-1) + "…",
		},
		{
			name: "multibyte_truncated_by_runes_not_bytes",
			// 250 CJK runes (each 3 bytes in UTF-8) should truncate to 199 + ellipsis = 200 runes.
			in:   strings.Repeat("漢", 250),
			want: strings.Repeat("漢", MaxSummaryLen-1) + "…",
		},
		{
			name: "newlines_collapse_to_single_space",
			in:   "Line one\n\n\nLine two\r\n\tLine three",
			want: "Line one Line two Line three",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeSummary(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeSummary(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if rc := utf8.RuneCountInString(got); rc > MaxSummaryLen {
				t.Errorf("NormalizeSummary returned %d runes, exceeds cap %d", rc, MaxSummaryLen)
			}
		})
	}
}

func TestFirstSentence(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "single_sentence", in: "A short library.", want: "A short library."},
		{
			name: "two_sentences_takes_first",
			in:   "A short library. It does many things.",
			want: "A short library.",
		},
		{
			name: "paragraph_break_takes_first_paragraph",
			in:   "First paragraph here.\n\nSecond paragraph after blank line.",
			want: "First paragraph here.",
		},
		{
			name: "carriage_return_paragraph_break",
			in:   "First.\r\n\r\nSecond.",
			want: "First.",
		},
		{
			name: "no_terminal_punctuation",
			in:   "A bare phrase without period",
			want: "A bare phrase without period",
		},
		{
			name: "preserves_version_strings",
			// Period after digit should NOT be a sentence break.
			in:   "Spring framework 5.3 is the latest version.",
			want: "Spring framework 5.3 is the latest version.",
		},
		{
			name: "exclamation_mark_is_terminal",
			in:   "Awesome library! Read the docs.",
			want: "Awesome library!",
		},
		{
			name: "question_mark_is_terminal",
			in:   "Looking for something? Try this.",
			want: "Looking for something?",
		},
		{
			name: "leading_whitespace_trimmed",
			in:   "   First.\nSecond.",
			want: "First.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FirstSentence(tt.in); got != tt.want {
				t.Errorf("FirstSentence(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestNormalizeSummary_Composition verifies the documented composition pattern for
// multi-paragraph sources (Maven POM, README) — FirstSentence then NormalizeSummary.
func TestNormalizeSummary_Composition(t *testing.T) {
	pomLike := "Apache Commons Lang, a package of Java utility classes for the\n" +
		"  classes that are in java.lang's hierarchy, or are considered to\n" +
		"  be so standard as to justify existence in java.lang.\n\n" +
		"The Apache Commons Lang 3 library provides additional helpers."
	first := FirstSentence(pomLike)
	got := NormalizeSummary(first)
	want := "Apache Commons Lang, a package of Java utility classes for the classes that are in java.lang's hierarchy, or are considered to be so standard as to justify existence in java.lang."
	if got != want {
		t.Errorf("composed normalize+firstsentence = %q\nwant %q", got, want)
	}
}
