//go:build cgo

package treesitter

import "testing"

// BenchmarkNewAnalyzer measures analyzer construction, which compiles the
// import and call queries for every supported language.
//
// This cost is invisible to BenchmarkAnalyzeCoupling: that benchmark builds its
// analyzer before b.ResetTimer(), so query compilation never enters its timed
// region. It is not invisible to users — uzomuzo-diet constructs one analyzer
// per invocation and pays it in full before any file is read.
func BenchmarkNewAnalyzer(b *testing.B) {
	// Guard: a construction that compiled nothing would benchmark an empty
	// loop. Every language must arrive with both queries compiled.
	a := NewAnalyzer()
	for lid, cfg := range a.configs {
		if cfg.compiledImport == nil {
			b.Fatalf("lang %d has no compiled import query", lid)
		}
		if cfg.compiledCall == nil {
			b.Fatalf("lang %d has no compiled call query", lid)
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
