package audit_test

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

// mockParser implements depparser.DependencyParser for testing.
type mockParser struct {
	deps []depparser.ParsedDependency
	err  error
}

func (m *mockParser) Parse(_ []byte) ([]depparser.ParsedDependency, error) {
	return m.deps, m.err
}

func (m *mockParser) FormatName() string { return "mock" }

func TestDeriveVerdict_Integration(t *testing.T) {
	// This test verifies that the domain verdict logic works correctly
	// when called through the same pattern as the service.
	// Full integration tests with AnalysisService require network access.

	tests := []struct {
		name string
		want domainaudit.Verdict
	}{
		{name: "nil_analysis_returns_review", want: domainaudit.VerdictReview},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domainaudit.DeriveVerdict(nil)
			if got != tt.want {
				t.Errorf("DeriveVerdict(nil) = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMockParser_Parse(t *testing.T) {
	p := &mockParser{
		deps: []depparser.ParsedDependency{
			{PURL: "pkg:npm/express@4.18.2", Ecosystem: "npm", Name: "express", Version: "4.18.2"},
		},
	}
	deps, err := p.Parse(nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("got %d deps, want 1", len(deps))
	}
	if deps[0].PURL != "pkg:npm/express@4.18.2" {
		t.Errorf("PURL = %q, want %q", deps[0].PURL, "pkg:npm/express@4.18.2")
	}
}
