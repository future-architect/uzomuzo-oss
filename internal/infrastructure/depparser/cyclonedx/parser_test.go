package cyclonedx_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
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

func TestParser_Parse_MinimalRelationUnknown(t *testing.T) {
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "minimal.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	for i, d := range deps {
		if d.Relation != depparser.RelationUnknown {
			t.Errorf("deps[%d].Relation = %v, want RelationUnknown", i, d.Relation)
		}
	}
}

func TestParser_Parse_WithDependencies_BOMRef(t *testing.T) {
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "with_dependencies.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 3 {
		t.Fatalf("got %d deps, want 3", len(deps))
	}

	relations := make(map[string]depparser.DependencyRelation, len(deps))
	viaParents := make(map[string][]string, len(deps))
	for _, d := range deps {
		relations[d.PURL] = d.Relation
		viaParents[d.PURL] = d.ViaParents
	}

	if r := relations["pkg:npm/express@4.18.2"]; r != depparser.RelationDirect {
		t.Errorf("express relation = %v, want RelationDirect", r)
	}
	if r := relations["pkg:npm/lodash@4.17.21"]; r != depparser.RelationDirect {
		t.Errorf("lodash relation = %v, want RelationDirect", r)
	}
	if r := relations["pkg:npm/body-parser@1.20.0"]; r != depparser.RelationTransitive {
		t.Errorf("body-parser relation = %v, want RelationTransitive", r)
	}

	if len(viaParents["pkg:npm/express@4.18.2"]) != 0 {
		t.Errorf("express ViaParents = %v, want empty", viaParents["pkg:npm/express@4.18.2"])
	}
	bp := viaParents["pkg:npm/body-parser@1.20.0"]
	if len(bp) != 1 || bp[0] != "express" {
		t.Errorf("body-parser ViaParents = %v, want [express]", bp)
	}
}

func TestParser_Parse_WithDependencies_PURLRef(t *testing.T) {
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "with_dependencies_purl_ref.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("got %d deps, want 2", len(deps))
	}

	relations := make(map[string]depparser.DependencyRelation, len(deps))
	for _, d := range deps {
		relations[d.PURL] = d.Relation
	}

	if r := relations["pkg:npm/express@4.18.2"]; r != depparser.RelationDirect {
		t.Errorf("express relation = %v, want RelationDirect", r)
	}
	if r := relations["pkg:npm/body-parser@1.20.0"]; r != depparser.RelationTransitive {
		t.Errorf("body-parser relation = %v, want RelationTransitive", r)
	}
}

func TestParser_Parse_WithDependencies_MultiVia(t *testing.T) {
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "with_dependencies_multi_via.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 3 {
		t.Fatalf("got %d deps, want 3", len(deps))
	}

	var debugDep *depparser.ParsedDependency
	for i := range deps {
		if deps[i].Name == "debug" {
			debugDep = &deps[i]
			break
		}
	}
	if debugDep == nil {
		t.Fatal("debug dep not found")
	}
	if debugDep.Relation != depparser.RelationTransitive {
		t.Errorf("debug relation = %v, want RelationTransitive", debugDep.Relation)
	}
	if len(debugDep.ViaParents) != 2 {
		t.Fatalf("debug ViaParents len = %d, want 2", len(debugDep.ViaParents))
	}
	if debugDep.ViaParents[0] != "axios" || debugDep.ViaParents[1] != "express" {
		t.Errorf("debug ViaParents = %v, want [axios express]", debugDep.ViaParents)
	}
}

func TestParser_Parse_WithDependencies_DeepChain(t *testing.T) {
	// express → body-parser → raw-body → bytes (3 levels of transitive)
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "with_dependencies_deep_chain.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 4 {
		t.Fatalf("got %d deps, want 4", len(deps))
	}

	relations := make(map[string]depparser.DependencyRelation, len(deps))
	viaParents := make(map[string][]string, len(deps))
	for _, d := range deps {
		relations[d.PURL] = d.Relation
		viaParents[d.PURL] = d.ViaParents
	}

	// Only express is direct.
	if r := relations["pkg:npm/express@4.18.2"]; r != depparser.RelationDirect {
		t.Errorf("express relation = %v, want RelationDirect", r)
	}
	// All others are transitive.
	for _, purl := range []string{
		"pkg:npm/body-parser@1.20.0",
		"pkg:npm/raw-body@2.5.1",
		"pkg:npm/bytes@3.1.2",
	} {
		if r := relations[purl]; r != depparser.RelationTransitive {
			t.Errorf("%s relation = %v, want RelationTransitive", purl, r)
		}
	}

	// All transitive deps trace back to express.
	for _, purl := range []string{
		"pkg:npm/body-parser@1.20.0",
		"pkg:npm/raw-body@2.5.1",
		"pkg:npm/bytes@3.1.2",
	} {
		via := viaParents[purl]
		if len(via) != 1 || via[0] != "express" {
			t.Errorf("%s ViaParents = %v, want [express]", purl, via)
		}
	}
}

func TestParser_Parse_WithDependencies_Cycle(t *testing.T) {
	// lib-a → lib-b → lib-c → lib-a (cycle). Must not hang or panic.
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "with_dependencies_cycle.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 3 {
		t.Fatalf("got %d deps, want 3", len(deps))
	}

	relations := make(map[string]depparser.DependencyRelation, len(deps))
	for _, d := range deps {
		relations[d.PURL] = d.Relation
	}

	// lib-a is the only direct dep.
	if r := relations["pkg:npm/lib-a@1.0.0"]; r != depparser.RelationDirect {
		t.Errorf("lib-a relation = %v, want RelationDirect", r)
	}
	if r := relations["pkg:npm/lib-b@2.0.0"]; r != depparser.RelationTransitive {
		t.Errorf("lib-b relation = %v, want RelationTransitive", r)
	}
	if r := relations["pkg:npm/lib-c@3.0.0"]; r != depparser.RelationTransitive {
		t.Errorf("lib-c relation = %v, want RelationTransitive", r)
	}
}

func TestParser_Parse_WithDependencies_BrokenRef(t *testing.T) {
	// dependsOn contains refs that don't exist in components.
	// Must not panic; unresolvable refs are silently skipped.
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "with_dependencies_broken_ref.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("got %d deps, want 2", len(deps))
	}

	relations := make(map[string]depparser.DependencyRelation, len(deps))
	for _, d := range deps {
		relations[d.PURL] = d.Relation
	}

	// express is direct (ghost-ref in root's dependsOn is ignored).
	if r := relations["pkg:npm/express@4.18.2"]; r != depparser.RelationDirect {
		t.Errorf("express relation = %v, want RelationDirect", r)
	}
	// lodash is not in root's dependsOn, so transitive.
	if r := relations["pkg:npm/lodash@4.17.21"]; r != depparser.RelationTransitive {
		t.Errorf("lodash relation = %v, want RelationTransitive", r)
	}
}

func TestParser_Parse_WithDependencies_NoRootInDeps(t *testing.T) {
	// metadata.component exists but root ref is not in dependencies section.
	// All dependencies should fall back to RelationUnknown.
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "with_dependencies_no_root.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("got %d deps, want 2", len(deps))
	}

	for _, d := range deps {
		if d.Relation != depparser.RelationUnknown {
			t.Errorf("%s relation = %v, want RelationUnknown", d.PURL, d.Relation)
		}
	}
}

func TestParser_Parse_WithDependencies_AllDirect(t *testing.T) {
	// Root depends on all 3 components directly. No transitive deps.
	p := &cyclonedx.Parser{}
	deps, err := p.Parse(context.Background(), readTestData(t, "with_dependencies_all_direct.json"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(deps) != 3 {
		t.Fatalf("got %d deps, want 3", len(deps))
	}

	for _, d := range deps {
		if d.Relation != depparser.RelationDirect {
			t.Errorf("%s relation = %v, want RelationDirect", d.PURL, d.Relation)
		}
		if len(d.ViaParents) != 0 {
			t.Errorf("%s ViaParents = %v, want empty", d.PURL, d.ViaParents)
		}
	}
}

func TestParser_FormatName(t *testing.T) {
	p := &cyclonedx.Parser{}
	if p.FormatName() != "CycloneDX SBOM" {
		t.Errorf("FormatName() = %q, want %q", p.FormatName(), "CycloneDX SBOM")
	}
}
