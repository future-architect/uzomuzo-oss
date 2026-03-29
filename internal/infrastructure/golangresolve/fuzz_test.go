package golangresolve

import "testing"

// FuzzParseModuleDirective fuzzes go.mod module directive extraction.
func FuzzParseModuleDirective(f *testing.F) {
	seeds := []string{
		"module github.com/example/repo\n\ngo 1.21\n",
		"// comment\nmodule github.com/example/repo\n",
		"module \n",
		"",
		"not a go.mod",
		"module github.com/example/repo\nmodule another.com/pkg\n",
		"\n\n\nmodule github.com/example/repo",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = parseModuleDirective(input)
	})
}
