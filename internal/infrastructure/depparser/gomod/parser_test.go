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

	// Should have 3 deps: copywrite (tool, exact match), exhaustive (tool, /cmd/... match), gin (regular)
	if len(deps) != 3 {
		t.Fatalf("got %d deps, want 3", len(deps))
	}

	// Build scope map by name for easy lookup
	scopeByName := make(map[string]string, len(deps))
	for _, d := range deps {
		scopeByName[d.Name] = d.Scope
	}

	// Exact-match tool directive
	if scope, ok := scopeByName["github.com/hashicorp/copywrite"]; !ok {
		t.Fatal("tool dependency github.com/hashicorp/copywrite not found")
	} else if scope != "tool" {
		t.Errorf("copywrite Scope = %q, want %q", scope, "tool")
	}

	// /cmd/... tool directive resolved to module path
	if scope, ok := scopeByName["github.com/nishanths/exhaustive"]; !ok {
		t.Fatal("tool dependency github.com/nishanths/exhaustive not found")
	} else if scope != "tool" {
		t.Errorf("exhaustive Scope = %q, want %q", scope, "tool")
	}

	// Regular dep
	if scope, ok := scopeByName["github.com/gin-gonic/gin"]; !ok {
		t.Fatal("regular dependency github.com/gin-gonic/gin not found")
	} else if scope != "" {
		t.Errorf("gin Scope = %q, want %q (empty)", scope, "")
	}
}

func TestParseToolPaths(t *testing.T) {
	toolPaths, err := gomod.ParseToolPaths(readTestData(t, "with_tool.mod"))
	if err != nil {
		t.Fatalf("ParseToolPaths() error = %v", err)
	}
	if len(toolPaths) != 2 {
		t.Fatalf("got %d tool paths, want 2", len(toolPaths))
	}
	// Exact-match tool directive
	if _, ok := toolPaths["github.com/hashicorp/copywrite"]; !ok {
		t.Error("expected module path github.com/hashicorp/copywrite to be present")
	}
	// /cmd/... tool directive resolved to module path
	if _, ok := toolPaths["github.com/nishanths/exhaustive"]; !ok {
		t.Error("expected module path github.com/nishanths/exhaustive to be present (resolved from /cmd/exhaustive)")
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
