package gomod_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/gomod"
)

func readTestData(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("failed to read testdata/%s: %v", name, err)
	}
	return data
}

func TestParser_Parse_Basic(t *testing.T) {
	p := &gomod.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "go.mod"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	// 3 direct deps (indirect is skipped)
	if len(deps) != 3 {
		t.Fatalf("got %d deps, want 3 (indirect excluded)", len(deps))
	}

	// Verify gin
	if deps[0].Name != "github.com/gin-gonic/gin" {
		t.Errorf("deps[0].Name = %q, want %q", deps[0].Name, "github.com/gin-gonic/gin")
	}
	if deps[0].PURL != "pkg:golang/github.com/gin-gonic/gin@v1.10.0" {
		t.Errorf("deps[0].PURL = %q, want %q", deps[0].PURL, "pkg:golang/github.com/gin-gonic/gin@v1.10.0")
	}

	// Verify /v3 suffix preserved
	if deps[1].Name != "github.com/Masterminds/semver/v3" {
		t.Errorf("deps[1].Name = %q, want %q", deps[1].Name, "github.com/Masterminds/semver/v3")
	}
	if deps[1].PURL != "pkg:golang/github.com/Masterminds/semver/v3@v3.4.0" {
		t.Errorf("deps[1].PURL = %q, want %q", deps[1].PURL, "pkg:golang/github.com/Masterminds/semver/v3@v3.4.0")
	}
}

func TestParser_Parse_WithReplace(t *testing.T) {
	p := &gomod.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "with_replace.mod"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("got %d deps, want 2", len(deps))
	}

	// old/module should be replaced by new/module
	if deps[0].Name != "github.com/new/module" {
		t.Errorf("replaced dep name = %q, want %q", deps[0].Name, "github.com/new/module")
	}
	if deps[0].Version != "v2.0.0" {
		t.Errorf("replaced dep version = %q, want %q", deps[0].Version, "v2.0.0")
	}
}

func TestParser_Parse_Empty(t *testing.T) {
	p := &gomod.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "empty.mod"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("got %d deps, want 0", len(deps))
	}
}

func TestParser_Parse_ToolDirective(t *testing.T) {
	p := &gomod.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "with_tool.mod"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should have 2 deps: copywrite (tool) and gin (regular)
	if len(deps) != 2 {
		t.Fatalf("got %d deps, want 2", len(deps))
	}

	// Find the tool dep by name
	var toolDep, regularDep *struct {
		name  string
		scope string
	}
	for i := range deps {
		switch deps[i].Name {
		case "github.com/hashicorp/copywrite":
			toolDep = &struct {
				name  string
				scope string
			}{deps[i].Name, deps[i].Scope}
		case "github.com/gin-gonic/gin":
			regularDep = &struct {
				name  string
				scope string
			}{deps[i].Name, deps[i].Scope}
		}
	}

	if toolDep == nil {
		t.Fatal("tool dependency github.com/hashicorp/copywrite not found")
	}
	if toolDep.scope != "tool" {
		t.Errorf("tool dep Scope = %q, want %q", toolDep.scope, "tool")
	}

	if regularDep == nil {
		t.Fatal("regular dependency github.com/gin-gonic/gin not found")
	}
	if regularDep.scope != "" {
		t.Errorf("regular dep Scope = %q, want %q (empty)", regularDep.scope, "")
	}
}

func TestParseToolPaths(t *testing.T) {
	toolPaths, err := gomod.ParseToolPaths(readTestData(t, "with_tool.mod"))
	if err != nil {
		t.Fatalf("ParseToolPaths() error = %v", err)
	}
	if len(toolPaths) != 1 {
		t.Fatalf("got %d tool paths, want 1", len(toolPaths))
	}
	if _, ok := toolPaths["github.com/hashicorp/copywrite"]; !ok {
		t.Error("expected tool path github.com/hashicorp/copywrite to be present")
	}
}

func TestParseToolPaths_NoTools(t *testing.T) {
	toolPaths, err := gomod.ParseToolPaths(readTestData(t, "go.mod"))
	if err != nil {
		t.Fatalf("ParseToolPaths() error = %v", err)
	}
	if len(toolPaths) != 0 {
		t.Errorf("expected 0 tool paths for go.mod without tool directives, got %d", len(toolPaths))
	}
}

func TestParser_Parse_InvalidData(t *testing.T) {
	p := &gomod.Parser{}
	_, err := p.Parse(context.Background(), []byte("not a go.mod"))
	if err == nil {
		t.Error("Parse() expected error for invalid go.mod, got nil")
	}
}

func TestParser_FormatName(t *testing.T) {
	p := &gomod.Parser{}
	if p.FormatName() != "go.mod" {
		t.Errorf("FormatName() = %q, want %q", p.FormatName(), "go.mod")
	}
}
