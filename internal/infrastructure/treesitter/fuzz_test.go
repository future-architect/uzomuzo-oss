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

// FuzzAnalyzeTypeScriptImportRequire feeds arbitrary TypeScript source through
// both TypeScript grammars to check that import-require parsing does not panic
// or hang on malformed input.
func FuzzAnalyzeTypeScriptImportRequire(f *testing.F) {
	seeds := []string{
		`import x = require('pkg'); x.f();`,
		`import type x = require('pkg');`,
		`export import x = require('pkg'); x.f();`,
		`import x = require('pkg'`,
		`import x = require(`,
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
		for _, name := range []string{"index.ts", "index.tsx"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, _ = analyzer.AnalyzeCoupling(context.Background(), dir, importPaths) // only panics and hangs matter here
	})
}

// vueSvelteSeeds are component snippets shared by the two Vue/Svelte fuzz
// targets: script blocks plus the markup forms the scanner skips.
var vueSvelteSeeds = []string{
	"<script>import p from 'pkg'; p.run();</script>",
	"<script lang='ts'>const p = require('pkg'); p.run();</script>",
	"<SCRIPT LANG=tsx>import p from 'pkg'; p.run();</SCRIPT>",
	"<template>require('pkg')</template><script>import p from 'pkg';",
	"<script setup lang=\"typescript\">import type { T } from 'pkg';</script>",
	"<p>{/[}]/.test('<!--')}</p><script>import p from 'pkg';</script>",
	"<p>{{ '<!--' }}</p><script>p()</script>",
	"<!-- <script>x</script> --><script>y</script>",
	"<style>a{content:\"<script>\"}</style><script>y</script>",
	"<textarea><script>x</script></textarea>\r\n<script>y</script>",
	"\u3042<p title={1 > 0 ? '<!--' : ''}>{'it\\'s'}</p><script>y</script>",
	"{",
	"{{",
	"<!--",
	"<style>",
	"<textarea>",
	"",
}

// FuzzAnalyzeVueSvelteScriptBlocks checks that malformed component files do
// not make source coupling analysis panic or hang.
func FuzzAnalyzeVueSvelteScriptBlocks(f *testing.F) {
	for _, seed := range vueSvelteSeeds {
		f.Add(seed)
	}

	analyzer := NewAnalyzer()
	f.Cleanup(analyzer.Close)
	importPaths := map[string][]string{"pkg:npm/pkg@1.0.0": {"pkg"}}

	f.Fuzz(func(t *testing.T, src string) {
		dir := t.TempDir()
		for _, filename := range []string{"App.vue", "Comp.svelte"} {
			if err := os.WriteFile(filepath.Join(dir, filename), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, _ = analyzer.AnalyzeCoupling(context.Background(), dir, importPaths) // only panics and hangs matter here
	})
}

// FuzzScriptContents checks the masking contract of scriptContents on
// arbitrary input, for both component kinds: the output has the input's
// length, every CR and LF stays where it was, and every other byte is either
// copied from the input or a space, a newline or a semicolon.
func FuzzScriptContents(f *testing.F) {
	for _, seed := range vueSvelteSeeds {
		f.Add(seed, true)
		f.Add(seed, false)
	}
	f.Fuzz(func(t *testing.T, src string, vue bool) {
		got, _, _ := scriptContents([]byte(src), vue)
		if len(got) != len(src) {
			t.Fatalf("len = %d, want %d", len(got), len(src))
		}
		for i := range got {
			in, out := src[i], got[i]
			switch {
			case in == '\n' || in == '\r':
				if out != in {
					t.Fatalf("byte %d: line break %q became %q", i, in, out)
				}
			case out != in && out != ' ' && out != '\n' && out != ';':
				t.Fatalf("byte %d: %q became %q, want a copy, space, newline or semicolon", i, in, out)
			}
		}
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
