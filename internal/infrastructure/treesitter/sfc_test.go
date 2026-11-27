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
			name:  "Svelte expression with a brace in a block comment",
			input: "{/* } */ '<!--'}<script>y</script><!-- end -->",
			want:  openMask("{/* } */ '<!--'}<script>") + "y" + closeMask("</script>") + openMask("<!-- end -->"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "Svelte expression with a brace in a line comment",
			input: "{a // }\n '<!--'}<script>y</script><!-- end -->",
			want:  openMask("{a // }") + "\n" + openMask(" '<!--'}<script>") + "y" + closeMask("</script>") + openMask("<!-- end -->"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "Svelte block closers are not regular expressions",
			input: "<ul>{#if ok}<li>yes</li>{/if}</ul>\n<script>y</script>\n<p><b>ratio</b> {a / b}</p>",
			want:  openMask("<ul>{#if ok}<li>yes</li>{/if}</ul>") + "\n" + openMask("<script>") + "y" + closeMask("</script>") + "\n" + openMask("<p><b>ratio</b> {a / b}</p>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "string after a Svelte block closer stays a string",
			input: "{#if a}b{/if}</p>{'<script>x</script>'}<script>y</script>",
			want:  openMask("{#if a}b{/if}</p>{'<script>x</script>'}<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "postfix increment then division in a Svelte expression",
			input: "{n++ / 2}{'<script>x</script>'}<script>y</script>",
			want:  openMask("{n++ / 2}{'<script>x</script>'}<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "division in a Svelte expression",
			input: "{a / b}{'<script>x</script>'}<script>y</script>",
			want:  openMask("{a / b}{'<script>x</script>'}<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "upper-case raw-text closing tag",
			input: "<style>a{content:\"<script>x</script>\"}</STYLE><script>y</script>",
			want:  openMask("<style>a{content:\"<script>x</script>\"}</STYLE><script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true, vue: true,
		},
		{
			name:  "upper-case STYLE is a component, as the Vue compiler reads it",
			input: "<STYLE>a{content:\"<script>x</script>\"}</STYLE><script>y</script>",
			want:  openMask("<STYLE>a{content:\"<script>") + "x" + closeMask("</script>") + openMask("\"}</STYLE><script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true, vue: true,
		},
		{
			name:  "Vue upper-case TITLE component with a closing tag in an interpolation",
			input: "<template><TITLE>{{ \"</TITLE><!--\" }}</TITLE></template> <script setup>y</script> <!-- end -->",
			want:  openMask("<template><TITLE>{{ \"</TITLE><!--\" }}</TITLE></template> <script setup>") + "y" + closeMask("</script>") + openMask(" <!-- end -->"),
			lang:  langJavaScript, found: true, vue: true,
		},
		{
			name:  "Vue title body skips interpolations when looking for its end",
			input: "<title>{{ \"</title><!--\" }}</title><script>y</script><!-- end -->",
			want:  openMask("<title>{{ \"</title><!--\" }}</title><script>") + "y" + closeMask("</script>") + openMask("<!-- end -->"),
			lang:  langJavaScript, found: true, vue: true,
		},
		{
			name:  "Svelte title body skips expressions when looking for its end",
			input: "<svelte:head><title>{\"</title><!--\"}</title></svelte:head> <script>y</script> <!-- end -->",
			want:  openMask("<svelte:head><title>{\"</title><!--\"}</title></svelte:head> <script>") + "y" + closeMask("</script>") + openMask(" <!-- end -->"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "Vue v-pre textarea keeps its {{ as text",
			input: "<template><textarea v-pre>{{</textarea></template>\n<script setup>y; const s = \"}}\";</script>",
			want:  openMask("<template><textarea v-pre>{{</textarea></template>") + "\n" + openMask("<script setup>") + "y; const s = \"}}\";" + closeMask("</script>"),
			lang:  langJavaScript, found: true, vue: true,
		},
		{
			name:  "Svelte title expression with a brace in a regular expression",
			input: "<svelte:head><title>{(/[{]/).test(\"\")}</title></svelte:head>\n<script>y;const r=/[}]/;</script>",
			want:  openMask("<svelte:head><title>{(/[{]/).test(\"\")}</title></svelte:head>") + "\n" + openMask("<script>") + "y;const r=/[}]/;" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			// Known limitation, pinned: after the stray `'`, title=" pairs with
			// the `"` inside the later comment, and the script is read as part
			// of that attribute value. See docs/diet.md.
			name:  "limitation: quote after a broken tag pairs with one in a later comment",
			input: "<x '<p title=\"oops><script>y</script><!-- \" -->",
			want:  openMask("<x '<p title=\"oops><script>y</script><!-- \" -->"),
			lang:  langJavaScript,
		},
		{
			name:  "script tag with an unclosed quote is read on as text",
			input: "<script lang=\"ts</script><script>y</script>",
			want:  openMask("<script lang=\"ts</script><script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "script tag with an unclosed quote around another script",
			input: "<script x='<script>y</script>",
			want:  openMask("<script x='<script>") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "Svelte upper-case component named like a raw-text tag",
			input: "<TITLE>{\"</TITLE><!--\"}</TITLE>\n<script>y</script>\n<!-- end -->",
			want:  openMask("<TITLE>{\"</TITLE><!--\"}</TITLE>") + "\n" + openMask("<script>") + "y" + closeMask("</script>") + "\n" + openMask("<!-- end -->"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "quoted self-closing marker after a quote with no close",
			input: "<x '<script data-x=\"/>\">y</script>",
			want:  openMask("<x '<script data-x=\"/>\">") + "y" + closeMask("</script>"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "quoted comment opener after a quote with no close",
			input: "<x '<div title=\"<!--\"></div><script>y</script><!-- end -->",
			want:  openMask("<x '<div title=\"<!--\"></div><script>") + "y" + closeMask("</script>") + openMask("<!-- end -->"),
			lang:  langJavaScript, found: true,
		},
		{
			name:  "raw-text tag after a broken tag is still skipped",
			input: "<y don't <style>/* <!-- */</style><script>y</script><!-- end -->",
			want:  openMask("<y don't <style>/* <!-- */</style><script>") + "y" + closeMask("</script>") + openMask("<!-- end -->"),
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

// TestScriptContentsStaysLinear feeds about 1 MiB of each pattern that once
// made, or could make, the scan retry from every repeat: each would take
// minutes at quadratic cost. The cases cover the failure flags
// (bracesUnbalanced, vueUnclosed, commentUnclosed, rawUnclosed), tags broken
// by a `<`, and quotes with no closing quote (lastQuote).
func TestScriptContentsStaysLinear(t *testing.T) {
	tests := []struct {
		name   string
		repeat string
		vue    bool
	}{
		{name: "unmatched Svelte braces (bracesUnbalanced)", repeat: "{"},
		{name: "unclosed Vue interpolations (vueUnclosed)", repeat: "{{", vue: true},
		{name: "unclosed comments (commentUnclosed)", repeat: "<!--"},
		{name: "tags broken by the next tag", repeat: "<a b"},
		{name: "tags after a quote with no close", repeat: "<b c"},
		{name: "tags that each open a quote", repeat: "<a '"},
		{name: "tags with alternating quotes", repeat: "<a '<b \""},
		{name: "unclosed style tags (rawUnclosed)", repeat: "<style>"},
		{name: "title bodies with an unclosed expression (rawExprOff)", repeat: "<title>{</title>"},
		{name: "title bodies before one script tag (nextScriptTag cache)", repeat: "<title>{}</title>"},
		{name: "slashes and classes in a Svelte expression", repeat: "(/["},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var prefix string
			switch tt.repeat {
			case "(/[":
				prefix = "{"
			case "<b c":
				prefix = "<a '" // a quote with no close, so <a is read without quotes
			}
			input := prefix + strings.Repeat(tt.repeat, (1<<20)/len(tt.repeat)) + "<script>y</script>"
			type result struct {
				got   []byte
				found bool
			}
			done := make(chan result, 1)
			go func() {
				got, _, found := scriptContents([]byte(input), tt.vue)
				done <- result{got, found}
			}()
			select {
			case r := <-done:
				if !r.found || !strings.HasSuffix(string(r.got), "y\n;       ") {
					t.Errorf("script after the repeats not kept: found=%v", r.found)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("scriptContents did not finish within 10s")
			}
		})
	}
}
