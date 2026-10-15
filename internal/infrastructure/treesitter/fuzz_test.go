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
