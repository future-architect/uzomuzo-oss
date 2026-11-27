//go:build cgo

package treesitter

import (
	"strings"
	"testing"
)

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
			want:  " \n" + strings.Repeat(" ", len("<script data-x=\">\" lang='TS'>")) + "let x=1;" + strings.Repeat(" ", len("</script>")) + "\r ",
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
			name:  "strongest language across blocks",
			input: "<script>one</script><SCRIPT LANG=typescript>two</SCRIPT><script lang=tsx>three</script>",
			want:  strings.Repeat(" ", len("<script>")) + "one" + strings.Repeat(" ", len("</script><SCRIPT LANG=typescript>")) + "two" + strings.Repeat(" ", len("</SCRIPT><script lang=tsx>")) + "three" + strings.Repeat(" ", len("</script>")),
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
