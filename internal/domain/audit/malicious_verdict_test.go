package audit_test

import (
	"errors"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

func TestMaliciousVerdictPrecedesError(t *testing.T) {
	a := &analysis.Analysis{Error: errors.New("not found"), MaliciousState: &analysis.MaliciousState{Status: analysis.MaliciousStatusFlagged}}
	if got := audit.DeriveVerdict(a); got != audit.VerdictReplace {
		t.Fatalf("got %q", got)
	}
}
