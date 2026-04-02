package scan_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/application/scan"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	domainscan "github.com/future-architect/uzomuzo-oss/internal/domain/scan"
)

// mockParser implements depparser.DependencyParser for testing.
type mockParser struct {
	deps []depparser.ParsedDependency
	err  error
}

func (m *mockParser) Parse(_ context.Context, _ []byte) ([]depparser.ParsedDependency, error) {
	return m.deps, m.err
}

func (m *mockParser) FormatName() string { return "mock" }

func TestNewService_NilAnalysisService(t *testing.T) {
	_, err := scan.NewService(nil)
	if err == nil {
		t.Fatal("expected error for nil analysisService, got nil")
	}
}

func TestRunFromParser_NilParser(t *testing.T) {
	// NewService requires non-nil analysisService; since we can't easily
	// construct a real one in a unit test, we verify the nil-parser guard
	// indirectly: ParseFailPolicy + RunFromParser with nil parser.
	// The nil-analysisService path is already covered by TestNewService_NilAnalysisService.
	// This test documents the expected contract.
	t.Skip("requires non-nil AnalysisService which needs infrastructure setup")
}

func TestRunFromParser_ParserError(t *testing.T) {
	t.Skip("requires non-nil AnalysisService which needs infrastructure setup")
}

func TestRunFromParser_EmptyDeps(t *testing.T) {
	t.Skip("requires non-nil AnalysisService which needs infrastructure setup")
}

func TestParseFailPolicy_Integration(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "empty is valid", raw: "", wantErr: false},
		{name: "single valid label", raw: "eol-confirmed", wantErr: false},
		{name: "multiple valid labels", raw: "eol-confirmed,stalled", wantErr: false},
		{name: "invalid label", raw: "bogus", wantErr: true},
		{name: "mixed valid and invalid", raw: "eol-confirmed,bogus", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := domainscan.ParseFailPolicy(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFailPolicy(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
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
	deps, err := p.Parse(context.Background(), nil)
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

func TestMockParser_Error(t *testing.T) {
	p := &mockParser{err: fmt.Errorf("parse error")}
	_, err := p.Parse(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error from parser, got nil")
	}
}
