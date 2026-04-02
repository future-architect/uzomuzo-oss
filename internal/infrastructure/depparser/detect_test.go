package depparser_test

import (
	"context"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	infradepparser "github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser"
)

// stubParser is a minimal DependencyParser for testing DetectFileParser routing.
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
			parser, data, err := infradepparser.DetectFileParser(tt.filePath, parsers)
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
	empty := map[string]depparser.DependencyParser{}

	_, _, err := infradepparser.DetectFileParser("testdata/gomod/go.mod", empty)
	if err == nil {
		t.Fatal("expected error for missing gomod parser, got nil")
	}

	_, _, err = infradepparser.DetectFileParser("testdata/tiny-sbom.json", empty)
	if err == nil {
		t.Fatal("expected error for missing sbom parser, got nil")
	}
}
