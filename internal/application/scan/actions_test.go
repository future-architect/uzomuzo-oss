package scan_test

import (
	"testing"

	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	domainscan "github.com/future-architect/uzomuzo-oss/internal/domain/scan"
)

func TestParseFailPolicy_ForActions(t *testing.T) {
	// Verify that fail policy works for entries regardless of source.
	policy, err := domainscan.ParseFailPolicy("")
	if err != nil {
		t.Fatalf("ParseFailPolicy('') error = %v", err)
	}

	entries := []domainaudit.AuditEntry{
		{PURL: "https://github.com/actions/checkout", Verdict: domainaudit.VerdictOK, Source: domainaudit.SourceActions},
		{PURL: "pkg:npm/express@4.18.2", Verdict: domainaudit.VerdictOK, Source: domainaudit.SourceDirect},
	}

	hasFailure := policy.Evaluate(entries)
	if hasFailure {
		t.Error("empty policy should not trigger failure")
	}
}
