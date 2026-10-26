package cyclonedx_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/cyclonedx"
)

// BenchmarkParseLargeSBOM measures CycloneDX parsing over a 2000-component SBOM,
// the large-input path that runs once per invocation.
func BenchmarkParseLargeSBOM(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("testdata", "large_sbom.json"))
	if err != nil {
		b.Fatalf("reading fixture: %v", err)
	}

	p := &cyclonedx.Parser{}
	ctx := context.Background()

	deps, err := p.Parse(ctx, data)
	if err != nil {
		b.Fatalf("parsing fixture: %v", err)
	}
	if len(deps) != 2000 {
		b.Fatalf("fixture produced %d dependencies, want 2000", len(deps))
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Parse(ctx, data); err != nil {
			b.Fatal(err)
		}
	}
}
