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
		{name: "feature underscores are exact", implies: []string{"with_x"}, features: []CargoFeature{{Name: "with_x", Implies: []string{"dep:x"}}, {Name: "with-x"}}, dep: CargoRequirementDependency{Name: "x", Optional: true}, want: []string{"root", "x"}},
		{name: "explicit feature shadows implicit dependency", implies: []string{"x"}, features: []CargoFeature{{Name: "x"}, {Name: "extra", Implies: []string{"dep:x"}}}, dep: CargoRequirementDependency{Name: "x", Optional: true}, want: []string{"root"}},
		{name: "explicit dep token disables implicit feature", implies: []string{"x"}, features: []CargoFeature{{Name: "extra", Implies: []string{"dep:x"}}}, dep: CargoRequirementDependency{Name: "x", Optional: true}, want: []string{"root"}},
		{name: "optional dependency local name is exact", implies: []string{"dep:x"}, dep: CargoRequirementDependency{Name: "x", PackageAlias: "alias", Optional: true}, want: []string{"root"}},
		{name: "optional dependency case is exact", implies: []string{"dep:X"}, dep: CargoRequirementDependency{Name: "x", Optional: true}, want: []string{"root"}},
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

func TestRestrictCargoGraph_RequirementSelection(t *testing.T) {
	cases := []struct {
		name          string
		declaration   string
		declarations  []CargoRequirementDependency
		runtimeEdge   string
		devEdge       string
		secondVersion string
		want          []string
	}{
		{name: "same crate with different requirements", declaration: "^1", runtimeEdge: "^1", devEdge: "^2", want: []string{"root", "x@1.0.0"}},
		{name: "requirements are trimmed", declaration: " ^1 ", runtimeEdge: "^1", devEdge: "^2", want: []string{"root", "x@1.0.0"}},
		{name: "mismatched requirement falls back to name", declaration: "1", runtimeEdge: "^1", devEdge: "^2", want: []string{"root", "x@1.0.0", "x@2.0.0", "child"}},
		{name: "unmatched build requirement falls back to name", declarations: []CargoRequirementDependency{{Name: "x", Requirement: "^1"}, {Name: "x", Kind: "build", Requirement: "2"}}, runtimeEdge: "^1", devEdge: "^2", want: []string{"root", "x@1.0.0", "x@2.0.0", "child"}},
		{name: "unmatched alias requirement falls back to name", declarations: []CargoRequirementDependency{{Name: "x", PackageAlias: "first", Requirement: "^1"}, {Name: "x", PackageAlias: "second", Requirement: "1.2"}}, runtimeEdge: "^1", devEdge: "^1.2", secondVersion: "1.2.0", want: []string{"root", "x@1.0.0", "x@1.2.0", "child"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			secondVersion := tc.secondVersion
			if secondVersion == "" {
				secondVersion = "2.0.0"
			}
			graph := &DependenciesResponse{
				Nodes: []DependencyNode{
					{VersionKey: DependencyVersionKey{Name: "root"}, Relation: "SELF"},
					{VersionKey: DependencyVersionKey{Name: "x", Version: "1.0.0"}, Relation: "DIRECT"},
					{VersionKey: DependencyVersionKey{Name: "x", Version: secondVersion}, Relation: "DIRECT"},
					{VersionKey: DependencyVersionKey{Name: "child"}, Relation: "INDIRECT"},
				},
				Edges: []DependencyEdge{{FromNode: 0, ToNode: 1, Requirement: tc.runtimeEdge}, {FromNode: 0, ToNode: 2, Requirement: tc.devEdge}, {FromNode: 2, ToNode: 3}},
			}
			declarations := tc.declarations
			if declarations == nil {
				declarations = []CargoRequirementDependency{
					{Name: "x", Requirement: tc.declaration},
					{Name: "x", Kind: "dev", Requirement: "^2"},
				}
			}
			requirements := &CargoRequirements{Cargo: &CargoRequirementsData{Dependencies: declarations}}
			got := restrictCargoGraph(requirements, graph)
			var names []string
			for _, node := range got.Nodes {
				name := node.VersionKey.Name
				if node.VersionKey.Version != "" {
					name += "@" + node.VersionKey.Version
				}
				names = append(names, name)
			}
			if !reflect.DeepEqual(names, tc.want) {
				t.Errorf("nodes = %v, want %v", names, tc.want)
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
