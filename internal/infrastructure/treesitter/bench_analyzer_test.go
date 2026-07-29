//go:build cgo

package treesitter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkNewAnalyzer measures analyzer construction on its own.
//
// Read it together with BenchmarkSingleLanguageRepo, never alone: query
// compilation is lazy, so this benchmark deliberately excludes it. A drop here
// means work moved to first use, not that it disappeared.
//
// Neither cost is visible to BenchmarkAnalyzeCoupling, which builds its
// analyzer before b.ResetTimer(). It is visible to users — uzomuzo-diet
// constructs one analyzer per invocation.
func BenchmarkNewAnalyzer(b *testing.B) {
	// Guard: every language must arrive registered with a language handle and
	// both query sources. Compiled queries are deliberately not asserted here —
	// they are compiled on first use, and BenchmarkSingleLanguageRepo's guard
	// covers the path that actually needs them.
	a := NewAnalyzer()
	if len(a.configs) == 0 {
		b.Fatal("NewAnalyzer registered no languages")
	}
	for lid, cfg := range a.configs {
		if cfg.language == nil {
			b.Fatalf("lang %d has no language handle", lid)
		}
		if cfg.importQuery == "" || cfg.callQuery == "" {
			b.Fatalf("lang %d has an empty query source", lid)
		}
	}
	a.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a := NewAnalyzer()
		a.Close()
	}
}

// BenchmarkSingleLanguageRepo measures what a user actually pays for a
// single-language repository: construct an analyzer, then analyze the tree.
//
// This is the honest counterpart to BenchmarkNewAnalyzer. Construction cost on
// its own can be moved around without the total changing — deferring query
// compilation to first use makes construction look free while the compilation
// still happens later. Only a benchmark spanning both steps shows whether the
// work went away or merely moved, and a single-language tree is the case where
// compiling every supported language is most obviously wasted.
func BenchmarkSingleLanguageRepo(b *testing.B) {
	root := writeGoOnlyCorpus(b, 40)
	importPaths := map[string][]string{
		"pkg:golang/github.com/foo/bar@v1.0.0": {"github.com/foo/bar"},
	}
	ctx := context.Background()

	// Guard: confirm the corpus yields coupling data, so the timed loop is not
	// measuring a walk that parses nothing.
	probe := NewAnalyzer()
	result, err := probe.AnalyzeCoupling(ctx, root, importPaths)
	probe.Close()
	if err != nil {
		b.Fatalf("AnalyzeCoupling failed during setup: %v", err)
	}
	if len(result) == 0 {
		b.Fatal("AnalyzeCoupling returned no coupling data; the benchmark would measure a no-op")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a := NewAnalyzer()
		if _, err := a.AnalyzeCoupling(ctx, root, importPaths); err != nil {
			a.Close()
			b.Fatal(err)
		}
		a.Close()
	}
}

// writeGoOnlyCorpus generates a deterministic Go-only source tree.
func writeGoOnlyCorpus(tb testing.TB, files int) string {
	tb.Helper()
	root := tb.TempDir()
	for i := 0; i < files; i++ {
		dir := filepath.Join(root, "src", fmt.Sprintf("mod%d", i%8))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			tb.Fatalf("creating corpus dir: %v", err)
		}
		writeCorpusFile(tb, filepath.Join(dir, fmt.Sprintf("file%d.go", i)), fmt.Sprintf(`package mod%d

import (
	"fmt"
	"github.com/foo/bar"
)

func Run%d() {
	bar.Do()
	bar.Also()
	fmt.Println(bar.Value())
}
`, i%8, i))
	}
	return root
}
