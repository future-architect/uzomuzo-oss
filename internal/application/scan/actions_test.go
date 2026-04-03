package scan_test

import (
	"context"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/application/scan"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	domainscan "github.com/future-architect/uzomuzo-oss/internal/domain/scan"
)

// mockDiscoverer implements scan.ActionsDiscoverer for testing.
type mockDiscoverer struct {
	directURLs     []string
	transitiveURLs []string
	errors         map[string]error
}

func (m *mockDiscoverer) DiscoverActions(_ context.Context, _ []string, _ bool) ([]string, []string, map[string]error, error) {
	return m.directURLs, m.transitiveURLs, m.errors, nil
}

func TestActionsConfig_DisabledByDefault(t *testing.T) {
	var cfg scan.ActionsConfig
	if cfg.Enabled {
		t.Error("ActionsConfig should be disabled by default (zero value)")
	}
	if cfg.Discoverer != nil {
		t.Error("ActionsConfig.Discoverer should be nil by default")
	}
}

func TestActionsDiscovererInterface(t *testing.T) {
	// Verify that mockDiscoverer satisfies the interface.
	var _ scan.ActionsDiscoverer = &mockDiscoverer{}
}

func TestEntrySource_Constants(t *testing.T) {
	if domainaudit.SourceDirect != "" {
		t.Errorf("SourceDirect should be empty string, got %q", domainaudit.SourceDirect)
	}
	if domainaudit.SourceActions != "actions" {
		t.Errorf("SourceActions should be 'actions', got %q", domainaudit.SourceActions)
	}
}

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
