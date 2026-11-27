//go:build cgo

package treesitter

import (
	"strings"
	"testing"
)

// openMask and closeMask are what a masked opening or closing tag becomes:
// spaces, with a newline at the byte next to the script body.
func openMask(tag string) string  { return strings.Repeat(" ", len(tag)-1) + "\n" }
func closeMask(tag string) string { return "\n" + strings.Repeat(" ", len(tag)-1) }

func TestScriptContents(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		lang  langID
		found bool
	}{
		{
			name:  "quoted angle bracket and offsets",
			input: "x\n<script data-x=\">\" lang='TS'>let x=1;</script>\rY",
			want:  " \n" + openMask("<script data-x=\">\" lang='TS'>") + "let x=1;" + closeMask("</script>") + "\r ",
			lang:  langTypeScript, found: true,
		},
		{
			name:  "similar tag is not a script",
			input: "<scripts>import x from 'x';</scripts>",
			want:  strings.Repeat(" ", len("<scripts>import x from 'x';</scripts>")),
			lang:  langJavaScript,
		},
		{
			name:  "hyphenated tag is not a script",
			input: "<script-foo>require('x')</script-foo>",
			want:  strings.Repeat(" ", len("<script-foo>require('x')</script-foo>")),
			lang:  langJavaScript,
		},
		{
			name:  "self-closing script has no body",
			input: "<script lang=ts/>require('x')",
			want:  strings.Repeat(" ", len("<script lang=ts/>require('x')")),
			lang:  langTypeScript, found: true,
		},
		{
			name:  "script inside an HTML comment is skipped",
			input: "<!-- <script>x</script> --><script>y</script>",
			want:  strings.Repeat(" ", len("<!-- <script>x</script> -->")) + openMask("<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "unterminated HTML comment hides the rest",
			input: "<!-- <script>x</script>",
			want:  strings.Repeat(" ", len("<!-- <script>x</script>")),
			lang:  langJavaScript,
		},
		{
			name:  "closing tag prefix inside the body",
			input: "<script>'</scripture>'</script>",
			want:  openMask("<script>") + "'</scripture>'" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "unclosed script keeps the body to the end",
			input: "<script>a\nb",
			want:  openMask("<script>") + "a\nb",
			lang:  langJavaScript, found: true,
		},
		{
			name:  "strongest language across blocks",
			input: "<script>one</script><SCRIPT LANG=typescript>two</SCRIPT><script lang=tsx>three</script>",
			want:  openMask("<script>") + "one" + closeMask("</script>") + openMask("<SCRIPT LANG=typescript>") + "two" + closeMask("</SCRIPT>") + openMask("<script lang=tsx>") + "three" + closeMask("</script>"),
			lang:  langTSX, found: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, lang, found := scriptContents([]byte(tt.input))
			if string(got) != tt.want || lang != tt.lang || found != tt.found {
				t.Errorf("scriptContents() = (%q, %v, %v), want (%q, %v, %v)", got, lang, found, tt.want, tt.lang, tt.found)
			}
			if len(got) != len(tt.input) {
				t.Errorf("length = %d, want %d", len(got), len(tt.input))
			}
		})
	}
}
