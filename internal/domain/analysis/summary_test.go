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
