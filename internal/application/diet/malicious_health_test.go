package diet

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

func TestMaliciousVersionMaximumHealthRisk(t *testing.T) {
	a := &analysis.Analysis{MaliciousState: &analysis.MaliciousState{Status: analysis.MaliciousStatusFlagged, Malicious: true}}
	if got := computeHealthSignals(a).HealthRisk; got != 1 {
		t.Fatalf("risk=%v, want 1", got)
	}
}
