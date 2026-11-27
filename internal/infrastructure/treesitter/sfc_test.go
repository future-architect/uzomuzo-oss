//go:build cgo

package treesitter

import (
	"strings"
	"testing"
	"time"
)

// openMask and closeMask are what a masked opening or closing tag becomes:
// spaces, except that a closing tag starts with a newline and a semicolon.
func openMask(tag string) string  { return strings.Repeat(" ", len(tag)) }
func closeMask(tag string) string { return "\n;" + strings.Repeat(" ", len(tag)-2) }

func TestScriptContents(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		lang  langID
		found bool
		vue   bool
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
			name:  "unterminated HTML comment does not hide later scripts",
			input: "<!-- <script>x</script>",
			want:  openMask("<!-- <script>") + "x" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "multibyte text and CRLF keep their bytes",
			input: "\u3042\r\n<script>let s='\u65e5\u672c';</script>\r\n",
			want:  "   \r\n" + openMask("<script>") + "let s='\u65e5\u672c';" + closeMask("</script>") + "\r\n",
			lang:  langJavaScript, found: true,
		},
		{
			name:  "tag with no closing bracket is read on as text",
			input: "<p>x <y don't</p><script>y</script>",
			want:  openMask("<p>x <y don't</p><script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "style with no closing tag is read on as text",
			input: "<style>a{}<script>y</script>",
			want:  openMask("<style>a{}<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "script inside textarea is text",
			input: "<textarea><script>x</script></textarea><script>y</script>",
			want:  openMask("<textarea><script>x</script></textarea><script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "capitalized component named like a raw-text tag is not skipped",
			input: "<Title><script>y</script></Title>",
			want:  openMask("<Title><script>") + "y" + closeMask("</script>") + openMask("</Title>"),
			lang:  langJavaScript, found: true,
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
			name:  "comment opener in a quoted attribute",
			input: "<div title=\"<!--\">t</div><script>y</script>",
			want:  strings.Repeat(" ", len("<div title=\"<!--\">t</div>")) + openMask("<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "style body is skipped",
			input: "<style>a{content:\"<script>x</script>\"}</style><script>y</script>",
			want:  strings.Repeat(" ", len("<style>a{content:\"<script>x</script>\"}</style>")) + openMask("<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "bare less-than in text",
			input: "a < b<script>y</script>",
			want:  strings.Repeat(" ", len("a < b")) + openMask("<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "unmatched brace is plain text",
			input: "a { b<script>y</script>",
			want:  strings.Repeat(" ", len("a { b")) + openMask("<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "Svelte expression string holding a script tag",
			input: "<p>{\"<script>x</script>\"}</p><script>y</script>",
			want:  openMask("<p>{\"<script>x</script>\"}</p><script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "Svelte expression with an escaped quote",
			input: "{'it\\'s <!--'}<script>y</script>",
			want:  openMask("{'it\\'s <!--'}<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "Svelte expression with a brace in a regular expression",
			input: "{/[}]/.test('<!--')}<script>y</script>",
			want:  openMask("{/[}]/.test('<!--')}<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "Svelte expression with a brace in a block comment",
			input: "{/* } */ '<!--'}<script>y</script>",
			want:  openMask("{/* } */ '<!--'}<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "Svelte expression with a brace in a line comment",
			input: "{a // }\n '<!--'}<script>y</script>",
			want:  openMask("{a // }") + "\n" + openMask(" '<!--'}<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "Svelte expression with division is not a regular expression",
			input: "{a / b / c}<script>y</script>",
			want:  openMask("{a / b / c}<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "Vue single brace is text",
			input: "{<script>y</script>}",
			want:  " " + openMask("<script>") + "y" + closeMask("</script>") + " ",
			lang:  langJavaScript, found: true, vue: true,
		},
		{
			name:  "Vue interpolation ends at the first double brace",
			input: "{{ '<script>' }}<script>y</script>",
			want:  strings.Repeat(" ", len("{{ '<script>' }}")) + openMask("<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true, vue: true,
		},
		{
			name:  "Vue braces in an unquoted attribute do not hide the tag end",
			input: "<p a={>t</p><script>y</script>",
			want:  strings.Repeat(" ", len("<p a={>t</p>")) + openMask("<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true, vue: true,
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
			got, lang, found := scriptContents([]byte(tt.input), tt.vue)
			if string(got) != tt.want || lang != tt.lang || found != tt.found {
				t.Errorf("scriptContents() = (%q, %v, %v), want (%q, %v, %v)", got, lang, found, tt.want, tt.lang, tt.found)
			}
			if len(got) != len(tt.input) {
				t.Errorf("length = %d, want %d", len(got), len(tt.input))
			}
		})
	}
}

// TestScriptContentsUnmatchedBracesStayLinear guards the bracesUnbalanced
// flag: without it, each of the 1 MiB of `{` would rescan to the end of input.
func TestScriptContentsUnmatchedBracesStayLinear(t *testing.T) {
	input := strings.Repeat("{", 1<<20) + "<script>y</script>"
	type result struct {
		got   []byte
		found bool
	}
	done := make(chan result, 1)
	go func() {
		got, _, found := scriptContents([]byte(input), false)
		done <- result{got, found}
	}()
	select {
	case r := <-done:
		if !r.found || !strings.HasSuffix(string(r.got), "y\n;       ") {
			t.Errorf("script after unmatched braces not kept: found=%v", r.found)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("scriptContents did not finish within 10s on unmatched braces")
	}
}

// TestScriptContentsUnclosedVueInterpolationsStayLinear guards vueUnclosed:
// without it, each `{{` would search to the end of input for `}}`.
func TestScriptContentsUnclosedVueInterpolationsStayLinear(t *testing.T) {
	input := strings.Repeat("{{", 1<<19) + "<script>y</script>"
	type result struct {
		got   []byte
		found bool
	}
	done := make(chan result, 1)
	go func() {
		got, _, found := scriptContents([]byte(input), true)
		done <- result{got, found}
	}()
	select {
	case r := <-done:
		if !r.found || !strings.HasSuffix(string(r.got), "y\n;       ") {
			t.Errorf("script after unclosed interpolations not kept: found=%v", r.found)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("scriptContents did not finish within 10s on unclosed interpolations")
	}
}
