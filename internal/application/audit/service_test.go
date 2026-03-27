package audit_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/application/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
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

func TestService_Run_NilAnalysisService(t *testing.T) {
	svc := audit.NewService(nil)
	parser := &mockParser{
		deps: []depparser.ParsedDependency{
			{PURL: "pkg:npm/express@4.18.2"},
		},
	}

	_, _, err := svc.Run(context.Background(), parser, nil)
	if err == nil {
		t.Fatal("expected error for nil analysisService, got nil")
	}
}

func TestService_Run_NilParser(t *testing.T) {
	svc := audit.NewService(nil)

	_, _, err := svc.Run(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error for nil parser, got nil")
	}
}

func TestService_Run_ParserError(t *testing.T) {
	svc := audit.NewService(nil)
	parser := &mockParser{err: fmt.Errorf("parse error")}

	_, _, err := svc.Run(context.Background(), parser, nil)
	if err == nil {
		t.Fatal("expected error from parser, got nil")
	}
}

func TestService_Run_EmptyDeps(t *testing.T) {
	svc := audit.NewService(nil)
	parser := &mockParser{deps: nil}

	entries, hasReplace, err := svc.Run(context.Background(), parser, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries for empty deps, got %d", len(entries))
	}
	if hasReplace {
		t.Error("expected hasReplace=false for empty deps")
	}
}
