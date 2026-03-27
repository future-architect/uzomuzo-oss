package cyclonedx_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/cyclonedx"
)

func readTestData(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("failed to read testdata/%s: %v", name, err)
	}
	return data
}

func TestParser_Parse_Minimal(t *testing.T) {
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "minimal.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 3 {
		t.Fatalf("got %d deps, want 3", len(deps))
	}

	// Verify first dep
	if deps[0].PURL != "pkg:npm/express@4.18.2" {
		t.Errorf("deps[0].PURL = %q, want %q", deps[0].PURL, "pkg:npm/express@4.18.2")
	}
	if deps[0].Ecosystem != "npm" {
		t.Errorf("deps[0].Ecosystem = %q, want %q", deps[0].Ecosystem, "npm")
	}
	if deps[0].Name != "express" {
		t.Errorf("deps[0].Name = %q, want %q", deps[0].Name, "express")
	}

	// Verify Go dep has namespace in name
	if deps[1].Name != "github.com/gin-gonic/gin" {
		t.Errorf("deps[1].Name = %q, want %q", deps[1].Name, "github.com/gin-gonic/gin")
	}
	if deps[1].Ecosystem != "golang" {
		t.Errorf("deps[1].Ecosystem = %q, want %q", deps[1].Ecosystem, "golang")
	}
}

func TestParser_Parse_Nested(t *testing.T) {
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "nested.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("got %d deps, want 2 (parent + nested child)", len(deps))
	}
	if deps[1].Name != "body-parser" {
		t.Errorf("nested dep name = %q, want %q", deps[1].Name, "body-parser")
	}
}

func TestParser_Parse_QualifiersStripped(t *testing.T) {
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "with_qualifiers.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("got %d deps, want 1", len(deps))
	}
	// Qualifiers should be stripped
	want := "pkg:golang/github.com/gin-gonic/gin@v1.10.0"
	if deps[0].PURL != want {
		t.Errorf("PURL = %q, want %q (qualifiers should be stripped)", deps[0].PURL, want)
	}
}

func TestParser_Parse_Empty(t *testing.T) {
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "empty.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("got %d deps, want 0", len(deps))
	}
}

func TestParser_Parse_NoPURL(t *testing.T) {
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "no_purl.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("got %d deps, want 1 (component without purl skipped)", len(deps))
	}
	if deps[0].Name != "express" {
		t.Errorf("dep name = %q, want %q", deps[0].Name, "express")
	}
}

func TestParser_Parse_Duplicates(t *testing.T) {
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "duplicates.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 1 {
		t.Errorf("got %d deps, want 1 (duplicates should be deduped)", len(deps))
	}
}

func TestParser_Parse_InvalidJSON(t *testing.T) {
	p := &cyclonedx.Parser{}
	_, err := p.Parse(context.Background(), []byte("not json"))
	if err == nil {
		t.Error("Parse() expected error for invalid JSON, got nil")
	}
}

func TestParser_FormatName(t *testing.T) {
	p := &cyclonedx.Parser{}
	if p.FormatName() != "CycloneDX SBOM" {
		t.Errorf("FormatName() = %q, want %q", p.FormatName(), "CycloneDX SBOM")
	}
}
