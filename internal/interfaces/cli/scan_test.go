package cli

import (
	"context"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
)

// stubParser is a minimal DependencyParser for testing detectFileParser routing.
type stubParser struct {
	name string
}

func (s *stubParser) Parse(_ context.Context, _ []byte) ([]depparser.ParsedDependency, error) {
	return nil, nil
}

func (s *stubParser) FormatName() string { return s.name }

func TestDetectFileParser(t *testing.T) {
	parsers := map[string]depparser.DependencyParser{
		"gomod": &stubParser{name: "go.mod"},
		"sbom":  &stubParser{name: "CycloneDX SBOM"},
	}

	tests := []struct {
		name       string
		filePath   string
		wantParser string // expected FormatName, empty means nil parser
		wantErr    bool
	}{
		{
			name:       "go.mod file",
			filePath:   "testdata/gomod/go.mod",
			wantParser: "go.mod",
		},
		{
			name:       "CycloneDX SBOM JSON",
			filePath:   "testdata/tiny-sbom.json",
			wantParser: "CycloneDX SBOM",
		},
		{
			name:       "plain text file falls through",
			filePath:   "testdata/purls.input",
			wantParser: "",
		},
		{
			name:     "nonexistent go.mod returns error",
			filePath: "testdata/nonexistent/go.mod",
			wantErr:  true,
		},
		{
			name:     "nonexistent JSON returns error",
			filePath: "testdata/nonexistent.json",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, data, err := detectFileParser(tt.filePath, parsers)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantParser == "" {
				if parser != nil {
					t.Errorf("expected nil parser, got %q", parser.FormatName())
				}
				if data != nil {
					t.Error("expected nil data when parser is nil")
				}
				return
			}

			if parser == nil {
				t.Fatalf("expected parser %q, got nil", tt.wantParser)
			}
			if parser.FormatName() != tt.wantParser {
				t.Errorf("parser = %q, want %q", parser.FormatName(), tt.wantParser)
			}
			if len(data) == 0 {
				t.Error("expected non-empty data")
			}
		})
	}
}

func TestDetectFileParser_MissingParser(t *testing.T) {
	// Empty parsers map — should error when file matches a known format
	empty := map[string]depparser.DependencyParser{}

	_, _, err := detectFileParser("testdata/gomod/go.mod", empty)
	if err == nil {
		t.Fatal("expected error for missing gomod parser, got nil")
	}

	_, _, err = detectFileParser("testdata/tiny-sbom.json", empty)
	if err == nil {
		t.Fatal("expected error for missing sbom parser, got nil")
	}
}

func TestNewEnrichedJSONEntry_NilAnalysis(t *testing.T) {
	e := &domainaudit.AuditEntry{
		PURL:     "pkg:npm/unknown@1.0.0",
		Verdict:  domainaudit.VerdictReview,
		ErrorMsg: "fetch failed",
	}

	je := newEnrichedJSONEntry(e)

	if je.PURL != e.PURL {
		t.Errorf("PURL = %q, want %q", je.PURL, e.PURL)
	}
	if je.Verdict != "review" {
		t.Errorf("Verdict = %q, want %q", je.Verdict, "review")
	}
	if je.Error != "fetch failed" {
		t.Errorf("Error = %q, want %q", je.Error, "fetch failed")
	}
	if je.RepoURL != "" {
		t.Errorf("RepoURL should be empty for nil Analysis, got %q", je.RepoURL)
	}
}

func TestNewEnrichedJSONEntry_WithAnalysis(t *testing.T) {
	a := &analysis.Analysis{
		RepoURL:        "https://github.com/expressjs/express",
		OverallScore:   85.5,
		DependentCount: 1000,
		EOL:            analysis.EOLStatus{State: analysis.EOLEndOfLife, Successor: "pkg:npm/fastify"},
		ProjectLicense: analysis.ResolvedLicense{Identifier: "MIT"},
		AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
			analysis.LifecycleAxis: {Label: analysis.LabelEOLConfirmed, Reason: "EOL confirmed"},
		},
	}

	e := &domainaudit.AuditEntry{
		PURL:     "pkg:npm/express@4.18.2",
		Verdict:  domainaudit.VerdictReplace,
		Analysis: a,
	}

	je := newEnrichedJSONEntry(e)

	if je.RepoURL != a.RepoURL {
		t.Errorf("RepoURL = %q, want %q", je.RepoURL, a.RepoURL)
	}
	if je.OverallScore != 85.5 {
		t.Errorf("OverallScore = %f, want 85.5", je.OverallScore)
	}
	if je.DependentCount != 1000 {
		t.Errorf("DependentCount = %d, want 1000", je.DependentCount)
	}
	if je.Successor != "pkg:npm/fastify" {
		t.Errorf("Successor = %q, want %q", je.Successor, "pkg:npm/fastify")
	}
	if je.ProjectLicense != "MIT" {
		t.Errorf("ProjectLicense = %q, want %q", je.ProjectLicense, "MIT")
	}
	if je.Reason != "EOL confirmed" {
		t.Errorf("Reason = %q, want %q", je.Reason, "EOL confirmed")
	}
}
