package depgraph

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/sbomgraph"
)

func TestAnalyzeGraph_Simple(t *testing.T) {
	// Root -> A -> C
	// Root -> B -> D
	// A and B are direct deps, C is exclusive to A, D is exclusive to B
	bom := sbomgraph.BOMEnvelope{
		Metadata: &sbomgraph.BOMMetadata{
			Component: &sbomgraph.Component{BOMRef: "root", PURL: "pkg:golang/myapp@v1.0.0"},
		},
		Components: []sbomgraph.Component{
			{BOMRef: "a", PURL: "pkg:golang/a@v1.0.0"},
			{BOMRef: "b", PURL: "pkg:golang/b@v1.0.0"},
			{BOMRef: "c", PURL: "pkg:golang/c@v1.0.0"},
			{BOMRef: "d", PURL: "pkg:golang/d@v1.0.0"},
		},
		Dependencies: []sbomgraph.Dependency{
			{Ref: "root", DependsOn: []string{"a", "b"}},
			{Ref: "a", DependsOn: []string{"c"}},
			{Ref: "b", DependsOn: []string{"d"}},
		},
	}

	data, err := json.Marshal(bom)
	if err != nil {
		t.Fatalf("failed to marshal BOM: %v", err)
	}
	analyzer := NewAnalyzer()
	result, err := analyzer.AnalyzeGraph(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.DirectDeps) != 2 {
		t.Errorf("expected 2 direct deps, got %d", len(result.DirectDeps))
	}

	aPURL := "pkg:golang/a@v1.0.0"
	bPURL := "pkg:golang/b@v1.0.0"

	if m, ok := result.Metrics[aPURL]; !ok {
		t.Errorf("missing metrics for %s", aPURL)
	} else if m.ExclusiveTransitiveCount != 1 {
		t.Errorf("expected 1 exclusive transitive for A, got %d", m.ExclusiveTransitiveCount)
	}

	if m, ok := result.Metrics[bPURL]; !ok {
		t.Errorf("missing metrics for %s", bPURL)
	} else if m.ExclusiveTransitiveCount != 1 {
		t.Errorf("expected 1 exclusive transitive for B, got %d", m.ExclusiveTransitiveCount)
	}

	// Neither A nor B appears in the other's transitive tree
	if result.Metrics[aPURL].StaysAsIndirect() {
		t.Error("A should not stay as indirect (B doesn't depend on A)")
	}
	if result.Metrics[bPURL].StaysAsIndirect() {
		t.Error("B should not stay as indirect (A doesn't depend on B)")
	}
}

func TestAnalyzeGraph_SharedTransitive(t *testing.T) {
	// Root -> A -> C
	// Root -> B -> C
	// C is shared between A and B
	bom := sbomgraph.BOMEnvelope{
		Metadata: &sbomgraph.BOMMetadata{
			Component: &sbomgraph.Component{BOMRef: "root", PURL: "pkg:golang/myapp@v1.0.0"},
		},
		Components: []sbomgraph.Component{
			{BOMRef: "a", PURL: "pkg:golang/a@v1.0.0"},
			{BOMRef: "b", PURL: "pkg:golang/b@v1.0.0"},
			{BOMRef: "c", PURL: "pkg:golang/c@v1.0.0"},
		},
		Dependencies: []sbomgraph.Dependency{
			{Ref: "root", DependsOn: []string{"a", "b"}},
			{Ref: "a", DependsOn: []string{"c"}},
			{Ref: "b", DependsOn: []string{"c"}},
		},
	}

	data, err := json.Marshal(bom)
	if err != nil {
		t.Fatalf("failed to marshal BOM: %v", err)
	}
	analyzer := NewAnalyzer()
	result, err := analyzer.AnalyzeGraph(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aPURL := "pkg:golang/a@v1.0.0"
	if m := result.Metrics[aPURL]; m.ExclusiveTransitiveCount != 0 {
		t.Errorf("expected 0 exclusive (C is shared), got %d", m.ExclusiveTransitiveCount)
	}
	if m := result.Metrics[aPURL]; m.SharedTransitiveCount != 1 {
		t.Errorf("expected 1 shared, got %d", m.SharedTransitiveCount)
	}
}

func TestAnalyzeGraph_DeepChain(t *testing.T) {
	// Root -> A -> B -> C -> D
	// All exclusive to A
	bom := sbomgraph.BOMEnvelope{
		Metadata: &sbomgraph.BOMMetadata{
			Component: &sbomgraph.Component{BOMRef: "root", PURL: "pkg:golang/myapp@v1.0.0"},
		},
		Components: []sbomgraph.Component{
			{BOMRef: "a", PURL: "pkg:golang/a@v1.0.0"},
			{BOMRef: "b", PURL: "pkg:golang/b@v1.0.0"},
			{BOMRef: "c", PURL: "pkg:golang/c@v1.0.0"},
			{BOMRef: "d", PURL: "pkg:golang/d@v1.0.0"},
		},
		Dependencies: []sbomgraph.Dependency{
			{Ref: "root", DependsOn: []string{"a"}},
			{Ref: "a", DependsOn: []string{"b"}},
			{Ref: "b", DependsOn: []string{"c"}},
			{Ref: "c", DependsOn: []string{"d"}},
		},
	}

	data, err := json.Marshal(bom)
	if err != nil {
		t.Fatalf("failed to marshal BOM: %v", err)
	}
	analyzer := NewAnalyzer()
	result, err := analyzer.AnalyzeGraph(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aPURL := "pkg:golang/a@v1.0.0"
	if m := result.Metrics[aPURL]; m.ExclusiveTransitiveCount != 3 {
		t.Errorf("expected 3 exclusive transitive (B,C,D), got %d", m.ExclusiveTransitiveCount)
	}
	if result.TotalTransitive != 3 {
		t.Errorf("expected 3 total transitive, got %d", result.TotalTransitive)
	}
}

func TestAnalyzeGraph_StaysAsIndirect(t *testing.T) {
	// Root -> A -> B
	// Root -> C -> A
	// A is direct AND reachable via C, so A.StaysAsIndirect() = true
	// C is direct but NOT reachable via A, so C.StaysAsIndirect() = false
	bom := sbomgraph.BOMEnvelope{
		Metadata: &sbomgraph.BOMMetadata{
			Component: &sbomgraph.Component{BOMRef: "root", PURL: "pkg:golang/myapp@v1.0.0"},
		},
		Components: []sbomgraph.Component{
			{BOMRef: "a", PURL: "pkg:golang/a@v1.0.0"},
			{BOMRef: "b", PURL: "pkg:golang/b@v1.0.0"},
			{BOMRef: "c", PURL: "pkg:golang/c@v1.0.0"},
		},
		Dependencies: []sbomgraph.Dependency{
			{Ref: "root", DependsOn: []string{"a", "c"}},
			{Ref: "a", DependsOn: []string{"b"}},
			{Ref: "c", DependsOn: []string{"a"}},
		},
	}

	data, err := json.Marshal(bom)
	if err != nil {
		t.Fatalf("failed to marshal BOM: %v", err)
	}
	analyzer := NewAnalyzer()
	result, err := analyzer.AnalyzeGraph(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aPURL := "pkg:golang/a@v1.0.0"
	cPURL := "pkg:golang/c@v1.0.0"

	mA := result.Metrics[aPURL]
	if !mA.StaysAsIndirect() {
		t.Error("A should stay as indirect (C depends on A)")
	}
	if len(mA.IndirectVia) != 1 || mA.IndirectVia[0] != cPURL {
		t.Errorf("A.IndirectVia = %v, want [%s]", mA.IndirectVia, cPURL)
	}

	mC := result.Metrics[cPURL]
	if mC.StaysAsIndirect() {
		t.Error("C should NOT stay as indirect (A doesn't depend on C)")
	}
}

func TestAnalyzeGraph_NoDependencies(t *testing.T) {
	bom := sbomgraph.BOMEnvelope{
		Components: []sbomgraph.Component{
			{BOMRef: "a", PURL: "pkg:golang/a@v1.0.0"},
		},
	}
	data, err := json.Marshal(bom)
	if err != nil {
		t.Fatalf("failed to marshal BOM: %v", err)
	}
	analyzer := NewAnalyzer()
	_, err = analyzer.AnalyzeGraph(context.Background(), data)
	if err == nil {
		t.Error("expected error for SBOM without dependency graph")
	}
}
