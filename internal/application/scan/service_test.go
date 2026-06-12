package scan_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/application"
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

// minimalService constructs a scan.Service backed by an AnalysisService with a
// nil AnalysisSource. This is sufficient for tests that exercise RunFromParser
// paths that exit before s.analysisService.ProcessBatchPURLs is called (nil-parser
// guard, parse-error path, empty-deps path).
func minimalService(t *testing.T) *scan.Service {
	t.Helper()
	svc, err := scan.NewService(application.NewAnalysisService(nil))
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestNewService_NilAnalysisService(t *testing.T) {
	_, err := scan.NewService(nil)
	if err == nil {
		t.Fatal("expected error for nil analysisService, got nil")
	}
}

func TestRunFromParser_NilParser(t *testing.T) {
	svc := minimalService(t)
	policy, err := domainscan.ParseFailPolicy("")
	if err != nil {
		t.Fatalf("ParseFailPolicy: %v", err)
	}
	_, err = svc.RunFromParser(context.Background(), nil, nil, policy, scan.ParserConfig{})
	if err == nil {
		t.Fatal("expected error for nil parser, got nil")
	}
	if !strings.Contains(err.Error(), "parser is nil") {
		t.Errorf("error %q does not mention 'parser is nil'", err.Error())
	}
}

func TestRunFromParser_ParserError(t *testing.T) {
	svc := minimalService(t)
	policy, err := domainscan.ParseFailPolicy("")
	if err != nil {
		t.Fatalf("ParseFailPolicy: %v", err)
	}
	p := &mockParser{err: fmt.Errorf("parse error")}
	_, err = svc.RunFromParser(context.Background(), p, nil, policy, scan.ParserConfig{})
	if err == nil {
		t.Fatal("expected error from parser, got nil")
	}
}

func TestRunFromParser_EmptyDeps(t *testing.T) {
	svc := minimalService(t)
	policy, err := domainscan.ParseFailPolicy("")
	if err != nil {
		t.Fatalf("ParseFailPolicy: %v", err)
	}
	p := &mockParser{deps: nil}
	result, err := svc.RunFromParser(context.Background(), p, nil, policy, scan.ParserConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(result.Entries))
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
