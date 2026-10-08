package scan

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

func TestMaliciousFailPolicy(t *testing.T) {
	entry := audit.AuditEntry{Analysis: &analysis.Analysis{MaliciousState: &analysis.MaliciousState{Status: analysis.MaliciousStatusFlagged}}}
	for _, raw := range []string{"malicious", "malicious,eol-confirmed", "eol-confirmed"} {
		p, err := ParseFailPolicy(raw)
		if err != nil {
			t.Fatal(err)
		}
		if p.IsEmpty() || !p.Evaluate([]audit.AuditEntry{entry}) {
			t.Errorf("policy %q did not trigger", raw)
		}
	}
	if p, err := ParseFailPolicy(""); err != nil || p.Evaluate([]audit.AuditEntry{entry}) {
		t.Fatalf("empty policy triggered: %v", err)
	}
}
