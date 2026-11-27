package depsdev

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var projectKeyShape = regexp.MustCompile(`^github\.com/[a-z0-9-]+/[a-z0-9._-]+$`)

// FuzzConvertRepoURLToProjectKey fuzzes repository URL conversion for panics and
// checks that any non-empty result is a well-formed github.com/<owner>/<repo> key.
func FuzzConvertRepoURLToProjectKey(f *testing.F) {
	seeds := []string{
		"https://github.com/Owner/Repo.git",
		"git+ssh://git@github.com:Owner/Repo.git",
		"git@github.com:Owner/Repo",
		"github.com:Owner/Repo",
		"https://git+ssh://git@github.com/Owner/Repo",
		"https://git@github.com:443@evil.example/Owner/Repo",
		"https://evil.example://github.com/Owner/Repo",
		"a://b://\u023a\u023a\u023a\u023a\u023a\u023a\u023a\u023a\u023a\u023a\u023a\u023a",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in string) {
		got := convertRepoURLToProjectKey(in)
		if got != "" && !projectKeyShape.MatchString(got) {
			t.Fatalf("convertRepoURLToProjectKey(%q) = %q, not a github.com/<owner>/<repo> key", in, got)
		}
	})
}

// FuzzPickStableDevAndMax checks version parsing safety and Stable's input-order
// independence, including equal publication dates for Maven.
func FuzzPickStableDevAndMax(f *testing.F) {
	for _, seed := range [][3]string{
		{"maven", "3.20.0.Final", "r09"},
		{"maven", "1.0.0.Final", "1.0.0-jre"},
		{"npm", "1.0.0.0", "0.9.0"},
		{"pypi", "2.0b3", "1.9.0"},
		{"golang", "v1.1.0-0.20260327151000-87f8819acf6d", "v1.0.0"},
		{"", "", "-"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	f.Fuzz(func(t *testing.T, system, first, second string) {
		secondDate := "2027-01-01T00:00:00Z"
		if strings.EqualFold(system, "maven") {
			secondDate = "2026-01-01T00:00:00Z"
		}
		versions := []Version{
			v("2.0.0-rc1", "2025-01-01T00:00:00Z", true),
			v(first, "2026-01-01T00:00:00Z", false),
			v(second, secondDate, false),
		}
		for i := range versions {
			versions[i].VersionKey.System = system
		}
		for _, hasDefault := range []bool{true, false} {
			versions[0].IsDefault = hasDefault
			forward, _, _ := pickStableDevAndMax(versions, "")
			versions[0], versions[2] = versions[2], versions[0]
			reverse, _, _ := pickStableDevAndMax(versions, "")
			if forward.VersionKey.Version != reverse.VersionKey.Version {
				t.Fatalf("Stable changed with input order (default=%v): %q vs %q", hasDefault, forward.VersionKey.Version, reverse.VersionKey.Version)
			}
			versions[0], versions[2] = versions[2], versions[0]
		}
	})
}

// FuzzRestrictCargoGraph checks that cargo graph restriction returns a valid
// graph for decoded requirements and dependency graphs.
func FuzzRestrictCargoGraph(f *testing.F) {
	requirements, err := os.ReadFile(filepath.Join("testdata", "cargo_dev_optional", "requirements.json"))
	if err != nil {
		f.Fatal(err)
	}
	dependencies, err := os.ReadFile(filepath.Join("testdata", "cargo_dev_optional", "dependencies.json"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(requirements, dependencies)

	for _, seed := range [][2]string{
		{
			`{"cargo":{"dependencies":[{"name":"x","requirement":"^1","optional":true}],"features":[{"name":"default","implies":["a"]},{"name":"a","implies":["b"]},{"name":"b","implies":["a","x"]}]}}`,
			`{"nodes":[{"relation":"SELF"},{"versionKey":{"name":"x"},"relation":"DIRECT"}],"edges":[{"fromNode":0,"toNode":1,"requirement":"^1"}]}`,
		},
		{
			`{"cargo":{"dependencies":[{"name":"x","requirement":"^1"}]}}`,
			`{"nodes":[{"relation":"SELF"},{"versionKey":{"name":"x"}}],"edges":[{"fromNode":0,"toNode":1,"requirement":"^1"},{"fromNode":0,"toNode":2},{"fromNode":-1,"toNode":1},{"fromNode":1,"toNode":-1}]}`,
		},
		{
			`{"cargo":{"dependencies":[{"name":"x","requirement":"^1"}]}}`,
			`{"nodes":[{"relation":"SELF"},{"versionKey":{"name":"x"}},{"relation":"SELF"}],"edges":[{"fromNode":0,"toNode":1,"requirement":"^1"},{"fromNode":1,"toNode":2}]}`,
		},
		{
			`{"cargo":{"dependencies":[{"name":"x","requirement":"^1"}]}}`,
			`{"nodes":[{"relation":"SELF"},{"relation":"SELF"}],"edges":[{"fromNode":0,"toNode":2}]}`,
		},
		{
			`{"cargo":{"dependencies":[{"name":"x","requirement":"^1"}]}}`,
			`{"nodes":[{"versionKey":{"name":"x"}}],"edges":[]}`,
		},
		{`{}`, `{"nodes":[{"relation":"SELF"}],"edges":[]}`},
		{`{"cargo":`, `{"nodes":[]}`},
		{`{"cargo":{}}`, `{"nodes":`},
	} {
		f.Add([]byte(seed[0]), []byte(seed[1]))
	}

	f.Fuzz(func(t *testing.T, requirementsJSON, graphJSON []byte) {
		var req CargoRequirements
		if err := json.Unmarshal(requirementsJSON, &req); err != nil {
			return
		}
		var graph DependenciesResponse
		if err := json.Unmarshal(graphJSON, &graph); err != nil {
			return
		}

		result := restrictCargoGraph(&req, &graph)
		if result == nil {
			t.Fatal("restrictCargoGraph returned nil")
		}
		// An unchanged input is returned as deps.dev sent it; only a rebuilt
		// graph must have consistent indexes.
		if result != &graph {
			for i, edge := range result.Edges {
				if edge.FromNode < 0 || edge.FromNode >= len(result.Nodes) || edge.ToNode < 0 || edge.ToNode >= len(result.Nodes) {
					t.Fatalf("result edge %d has invalid node indexes: %+v (nodes: %d)", i, edge, len(result.Nodes))
				}
			}
			if len(result.Nodes) > len(graph.Nodes) {
				t.Fatalf("result has %d nodes, input has %d", len(result.Nodes), len(graph.Nodes))
			}
			selfCount := 0
			for _, node := range result.Nodes {
				if node.Relation == "SELF" {
					selfCount++
				}
			}
			if selfCount > 1 {
				t.Fatalf("result has %d SELF nodes", selfCount)
			}
		}
	})
}
