//go:build cgo

package treesitter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// FuzzAnalyzeJavaScriptRequire feeds arbitrary JavaScript source through the
// coupling analysis, including the require() binding walk, to check that
// malformed, truncated, or deeply nested input does not panic or hang.
func FuzzAnalyzeJavaScriptRequire(f *testing.F) {
	seeds := []string{
		`const ipc = require("pkg").default; ipc.serve(() => {});`,
		`const post = require("pkg").post; post({});`,
		`const sub = require("pkg").api.v2; sub.run();`,
		`var X = window.X || require("pkg").default; X.on("tap");`,
		`const x = ready && require("pkg").api; x.run();`,
		`const x = require("pkg").flag && local; x();`,
		`const x = c ? require("pkg").a : b; x.go();`,
		`deps.ipc = require("pkg").default; deps.ipc.serve();`,
		`module.exports = require("pkg").helpers;`,
		`const x = (deps.x = require("pkg").y); x();`,
		`const p = (((require("pkg").api))); p.call();`,
		`const x = a || (b || (c || (d || (e || require("pkg").y)))); x();`,
		`const x = require("pkg").`,
		`const x = require("pkg"`,
		`require(`,
		`const { a } = require("pkg").b; a();`,
		`const x = require("pkg")["y"]; x();`,
		`const x = require(` + "`pkg`" + `).y;`,
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	analyzer := NewAnalyzer()
	f.Cleanup(analyzer.Close)
	importPaths := map[string][]string{"pkg:npm/pkg@1.0.0": {"pkg"}}

	f.Fuzz(func(t *testing.T, src string) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _ = analyzer.AnalyzeCoupling(context.Background(), dir, importPaths) // only panics and hangs matter here
	})
}

// FuzzAnalyzeGoPackageNames feeds two arbitrary Go files of one directory
// through the coupling analysis, including the package-name inference of
// #551 and its directory scan, to check that malformed input does not panic
// or hang.
func FuzzAnalyzeGoPackageNames(f *testing.F) {
	seeds := [][2]string{
		{"package main\n\nimport \"github.com/json-iterator/go\"\n\nfunc main() { jsoniter.Marshal(nil) }\n", "package main\n"},
		{"package main\n\nimport \"k8s.io/api/core/v1\"\n\nvar _ = v1.Pod{}\n", "package main\n\nfunc f() { v1 := 1; _ = v1 }\n"},
		{"package main\n\nimport (\n\t. \"github.com/json-iterator/go\"\n\t\"C\"\n)\n", "package other\n"},
		{"package main\n\nimport \"github.com/json-iterator/go\"\n\nfunc main() { jsoniter.", "package main\n\nfunc ("},
		{"import \"github.com/json-iterator/go\"", ""},
		{"", ""},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1])
	}

	analyzer := NewAnalyzer()
	f.Cleanup(analyzer.Close)
	importPaths := map[string][]string{"pkg:golang/github.com/json-iterator/go@v1.1.12": {"github.com/json-iterator/go"}}

	f.Fuzz(func(t *testing.T, src, sibling string) {
		dir := t.TempDir()
		for name, content := range map[string]string{"main.go": src, "sibling.go": sibling} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, _ = analyzer.AnalyzeCoupling(context.Background(), dir, importPaths) // only panics and hangs matter here
	})
}
