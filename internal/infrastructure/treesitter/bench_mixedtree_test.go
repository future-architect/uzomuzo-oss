//go:build cgo

package treesitter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkAnalyzeCouplingMixedTree measures analysis over a tree that also
// contains files no parser handles.
//
// BenchmarkAnalyzeCoupling's corpus is 100% parseable source, which no
// repository is. Measured on this one: 275 of 455 files are source, so 40% of
// what the walker visits is Markdown, JSON, YAML, shell, and lockfiles. Work
// spent per *rejected* file — the stat, the extension lookup, the ordering
// between them — is invisible to a corpus where nothing is ever rejected.
//
// The ratio here follows that observation: 3 non-source files for every 5
// source files.
func BenchmarkAnalyzeCouplingMixedTree(b *testing.B) {
	root, importPaths := writeMixedCorpus(b, 50)
	ctx := context.Background()

	analyzer := NewAnalyzer()
	b.Cleanup(analyzer.Close)

	// Guard: the non-source files must not have displaced the source ones —
	// coupling data still has to come out, or this measures an empty walk.
	result, err := analyzer.AnalyzeCoupling(ctx, root, importPaths)
	if err != nil {
		b.Fatalf("AnalyzeCoupling failed during setup: %v", err)
	}
	if len(result) == 0 {
		b.Fatal("AnalyzeCoupling returned no coupling data; the benchmark would measure a no-op")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := analyzer.AnalyzeCoupling(ctx, root, importPaths); err != nil {
			b.Fatal(err)
		}
	}
}

// writeMixedCorpus generates the multi-language corpus plus the non-source
// files a real repository carries alongside it. Deterministic, like
// writeBenchCorpus.
func writeMixedCorpus(tb testing.TB, filesPerLang int) (string, map[string][]string) {
	tb.Helper()
	root, importPaths := writeBenchCorpus(tb, filesPerLang)

	// writeBenchCorpus emits 4 source files per iteration; 3 non-source files
	// per iteration lands near the 60/40 split observed in a real tree.
	for i := 0; i < filesPerLang; i++ {
		dir := filepath.Join(root, "src", fmt.Sprintf("mod%d", i%10))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			tb.Fatalf("creating corpus dir: %v", err)
		}
		writeCorpusFile(tb, filepath.Join(dir, fmt.Sprintf("README%d.md", i)), fmt.Sprintf(`# Module %d

Describes what module %d does and how to use it.

## Usage

    import "github.com/foo/bar"

The snippet above is prose, not code the walker should parse.
`, i, i))
		writeCorpusFile(tb, filepath.Join(dir, fmt.Sprintf("config%d.json", i)), fmt.Sprintf(`{
  "name": "mod%d",
  "version": "1.0.%d",
  "dependencies": {"github.com/foo/bar": "v1.0.0"}
}
`, i%10, i))
		writeCorpusFile(tb, filepath.Join(dir, fmt.Sprintf("workflow%d.yml", i)), fmt.Sprintf(`name: build-%d
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: go build ./...
`, i))
	}

	return root, importPaths
}
