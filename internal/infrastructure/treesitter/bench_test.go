//go:build cgo

package treesitter

import (
	"context"
	"testing"
)

// BenchmarkAnalyzeCoupling measures the dominant CPU cost of uzomuzo-diet: walking
// a source tree and tree-sitter-parsing every file a grammar handles.
func BenchmarkAnalyzeCoupling(b *testing.B) {
	root, importPaths := writeBenchCorpus(b, 50)
	ctx := context.Background()

	analyzer := NewAnalyzer()
	b.Cleanup(analyzer.Close)

	result, err := analyzer.AnalyzeCoupling(ctx, root, importPaths)
	if err != nil {
		b.Fatalf("AnalyzeCoupling failed during setup: %v", err)
	}
	if len(result) == 0 {
		b.Fatal("AnalyzeCoupling returned no coupling data; the benchmark would measure a no-op")
	}
	b.Logf("setup: %d PURLs with coupling data", len(result))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := analyzer.AnalyzeCoupling(ctx, root, importPaths); err != nil {
			b.Fatal(err)
		}
	}
}
