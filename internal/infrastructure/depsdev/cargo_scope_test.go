package depsdev

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRestrictCargoGraph_DefaultFeatures(t *testing.T) {
	cases := []struct {
		name     string
		implies  []string
		features []CargoFeature
		dep      CargoRequirementDependency
		want     []string
	}{
		{name: "explicit dependency", implies: []string{"dep:x"}, dep: CargoRequirementDependency{Name: "x", Optional: true}, want: []string{"root", "x"}},
		{name: "dependency feature", implies: []string{"x/feat"}, dep: CargoRequirementDependency{Name: "x", Optional: true}, want: []string{"root", "x"}},
		{name: "weak dependency feature", implies: []string{"x?/feat"}, dep: CargoRequirementDependency{Name: "x", Optional: true}, want: []string{"root"}},
		{name: "implicit feature", implies: []string{"x"}, dep: CargoRequirementDependency{Name: "x", Optional: true}, want: []string{"root", "x"}},
		{name: "nested feature", implies: []string{"outer"}, features: []CargoFeature{{Name: "outer", Implies: []string{"inner"}}, {Name: "inner", Implies: []string{"dep:x"}}}, dep: CargoRequirementDependency{Name: "x", Optional: true}, want: []string{"root", "x"}},
		{name: "no default", dep: CargoRequirementDependency{Name: "x", Optional: true}, want: []string{"root"}},
		{name: "underscore and hyphen", implies: []string{"dep:unicode_ident"}, dep: CargoRequirementDependency{Name: "unicode_ident", Optional: true}, want: []string{"root", "unicode-ident"}},
		{name: "package alias", implies: []string{"dep:alias"}, dep: CargoRequirementDependency{Name: "x", PackageAlias: "alias", Optional: true}, want: []string{"root", "x"}},
		{name: "package alias graph fallback", implies: []string{"dep:alias"}, dep: CargoRequirementDependency{Name: "x", PackageAlias: "alias", Optional: true}, want: []string{"root", "alias"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			depName := tc.dep.Name
			if tc.name == "underscore and hyphen" {
				depName = "unicode-ident"
			}
			if tc.name == "package alias graph fallback" {
				depName = "alias"
			}
			graph := &DependenciesResponse{
				Nodes: []DependencyNode{{VersionKey: DependencyVersionKey{Name: "root"}, Relation: "SELF"}, {VersionKey: DependencyVersionKey{Name: depName}, Relation: "DIRECT"}},
				Edges: []DependencyEdge{{FromNode: 0, ToNode: 1}},
			}
			features := tc.features
			if tc.name != "no default" {
				features = append(features, CargoFeature{Name: "default", Implies: tc.implies})
			}
			got := restrictCargoGraph(&CargoRequirements{Cargo: &CargoRequirementsData{Dependencies: []CargoRequirementDependency{tc.dep}, Features: features}}, graph)
			var names []string
			for _, node := range got.Nodes {
				names = append(names, node.VersionKey.Name)
			}
			if !reflect.DeepEqual(names, tc.want) {
				t.Errorf("names = %v, want %v", names, tc.want)
			}
		})
	}
}

func TestRestrictCargoGraph_FixtureReachability(t *testing.T) {
	read := func(name string, target any) {
		t.Helper()
		body, err := os.ReadFile(filepath.Join("testdata", "cargo_dev_optional", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, target); err != nil {
			t.Fatal(err)
		}
	}
	var graph DependenciesResponse
	var requirements CargoRequirements
	read("dependencies.json", &graph)
	read("requirements.json", &requirements)
	got := restrictCargoGraph(&requirements, &graph)
	wantNames := []string{"sample-crate", "serde", "log", "regex", "aho-corasick", "memchr", "fastrand", "cc", "shlex"}
	var names []string
	for _, node := range got.Nodes {
		names = append(names, node.VersionKey.Name)
	}
	if !reflect.DeepEqual(names, wantNames) {
		t.Errorf("nodes = %v, want %v", names, wantNames)
	}
	if direct, transitive := got.CountByRelation(); direct != 5 || transitive != 3 {
		t.Errorf("counts = (%d, %d), want (5, 3)", direct, transitive)
	}
	wantEdges := []DependencyEdge{
		{FromNode: 0, ToNode: 1, Requirement: "*"},
		{FromNode: 0, ToNode: 2, Requirement: "*"},
		{FromNode: 0, ToNode: 3, Requirement: "*"},
		{FromNode: 0, ToNode: 6, Requirement: "*"},
		{FromNode: 0, ToNode: 7, Requirement: "*"},
		{FromNode: 3, ToNode: 4, Requirement: "*"},
		{FromNode: 3, ToNode: 5, Requirement: "*"},
		{FromNode: 4, ToNode: 5, Requirement: "*"},
		{FromNode: 7, ToNode: 8, Requirement: "*"},
	}
	if !reflect.DeepEqual(got.Edges, wantEdges) {
		t.Errorf("edges = %+v, want %+v", got.Edges, wantEdges)
	}
	for _, edge := range got.Edges {
		if edge.FromNode < 0 || edge.FromNode >= len(got.Nodes) || edge.ToNode < 0 || edge.ToNode >= len(got.Nodes) {
			t.Errorf("invalid edge: %+v", edge)
		}
	}
	if len(graph.Nodes) != 16 {
		t.Errorf("input graph was mutated: %d nodes", len(graph.Nodes))
	}
}

func TestRestrictCargoGraph_UnmatchedRequirementKeepsOriginal(t *testing.T) {
	graph := &DependenciesResponse{Nodes: []DependencyNode{{Relation: "SELF"}, {VersionKey: DependencyVersionKey{Name: "actual"}, Relation: "DIRECT"}}, Edges: []DependencyEdge{{FromNode: 0, ToNode: 1}}}
	requirements := &CargoRequirements{Cargo: &CargoRequirementsData{Dependencies: []CargoRequirementDependency{{Name: "different"}}}}
	if got := restrictCargoGraph(requirements, graph); got != graph {
		t.Error("unmatched requirement should preserve original graph")
	}
}
