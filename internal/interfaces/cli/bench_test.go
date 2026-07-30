package cli

import (
	"fmt"
	"io"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

func makeBenchEntries(n int) []domainaudit.AuditEntry {
	entries := make([]domainaudit.AuditEntry, 0, n)
	verdicts := []domainaudit.Verdict{
		domainaudit.VerdictOK,
		domainaudit.VerdictReplace,
		domainaudit.VerdictReview,
	}
	for i := 0; i < n; i++ {
		e := domainaudit.AuditEntry{
			PURL:    fmt.Sprintf("pkg:npm/pkg-%d@1.%d.0", i, i%50),
			Verdict: verdicts[i%len(verdicts)],
		}
		switch i % 3 {
		case 0:
			e.Analysis = &analysis.Analysis{
				AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
					analysis.LifecycleAxis: {Label: string(analysis.LabelActive)},
				},
			}
		case 1:
			e.Analysis = &analysis.Analysis{
				EOL: analysis.EOLStatus{State: analysis.EOLEndOfLife},
			}
		default:
			e.Analysis = nil
		}
		entries = append(entries, e)
	}
	return entries
}

// BenchmarkRenderScanOutput measures scan rendering for each output format over a
// large entry set. Table and CSV build a row per entry, so their cost scales with
// the dependency count.
func BenchmarkRenderScanOutput(b *testing.B) {
	entries := makeBenchEntries(1000)

	for _, format := range []string{"table", "json", "csv"} {
		b.Run(format, func(b *testing.B) {
			cw := &countingWriter{}
			if err := renderScanOutput(cw, entries, entries, format, false); err != nil {
				b.Fatalf("renderScanOutput(%s) failed during setup: %v", format, err)
			}
			if cw.n < 1000 {
				b.Fatalf("renderScanOutput(%s) wrote only %d bytes, expected substantial output", format, cw.n)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := renderScanOutput(io.Discard, entries, entries, format, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type countingWriter struct{ n int }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += len(p)
	return len(p), nil
}
