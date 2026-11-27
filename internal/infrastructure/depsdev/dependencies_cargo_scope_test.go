package depsdev

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

func TestFetchDependenciesBatch_CargoExcludesDevAndInactiveOptional(t *testing.T) {
	dependencies, err := os.ReadFile(filepath.Join("testdata", "cargo_dev_optional", "dependencies.json"))
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := os.ReadFile(filepath.Join("testdata", "cargo_dev_optional", "requirements.json"))
	if err != nil {
		t.Fatal(err)
	}
	const path = "/v3alpha/systems/cargo/packages/sample-crate/versions/0.1.0"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case path + ":dependencies":
			_, _ = w.Write(dependencies)
		case path + ":requirements":
			_, _ = w.Write(requirements)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0, BatchSize: 100})
	const purl = "pkg:cargo/sample-crate@0.1.0"
	graph := client.FetchDependenciesBatch(context.Background(), []string{purl})[purl]
	if graph == nil {
		t.Fatal("dependency graph is nil")
	}
	if direct, transitive := graph.CountByRelation(); direct != 5 || transitive != 3 {
		t.Errorf("counts = (%d, %d), want (5, 3)", direct, transitive)
	}
	wantRelations := map[string]string{
		"aho-corasick": "INDIRECT", "cc": "DIRECT", "fastrand": "DIRECT",
		"log": "DIRECT", "memchr": "INDIRECT", "regex": "DIRECT",
		"serde": "DIRECT", "shlex": "INDIRECT",
	}
	var names []string
	for _, node := range graph.Nodes {
		if node.Relation == "SELF" {
			continue
		}
		name := node.VersionKey.Name
		names = append(names, name)
		if want, ok := wantRelations[name]; !ok || node.Relation != want {
			t.Errorf("relation for %q = %q, want %q", name, node.Relation, want)
		}
	}
	sort.Strings(names)
	wantNames := []string{"aho-corasick", "cc", "fastrand", "log", "memchr", "regex", "serde", "shlex"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Errorf("non-SELF names = %v, want %v", names, wantNames)
	}
	for _, edge := range graph.Edges {
		if edge.FromNode < 0 || edge.FromNode >= len(graph.Nodes) || edge.ToNode < 0 || edge.ToNode >= len(graph.Nodes) {
			t.Errorf("edge indexes (%d, %d) outside %d nodes", edge.FromNode, edge.ToNode, len(graph.Nodes))
		}
	}
}

func TestFetchTransitiveAdvisoryKeys_CargoIgnoresDevOnlyAdvisories(t *testing.T) {
	dependencies, err := os.ReadFile(filepath.Join("testdata", "cargo_dev_optional", "dependencies.json"))
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := os.ReadFile(filepath.Join("testdata", "cargo_dev_optional", "requirements.json"))
	if err != nil {
		t.Fatal(err)
	}
	const path = "/v3alpha/systems/cargo/packages/sample-crate/versions/0.1.0"
	advisories := map[string]string{
		"tar@0.4.40":   "RUSTSEC-0000-0001",
		"rand@0.8.5":   "RUSTSEC-0000-0002",
		"memchr@2.7.4": "RUSTSEC-0000-0003",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case path + ":dependencies":
			_, _ = w.Write(dependencies)
		case path + ":requirements":
			_, _ = w.Write(requirements)
		default:
			if !strings.HasPrefix(r.URL.Path, "/v3alpha/purl/") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			decoded, err := url.PathUnescape(strings.TrimPrefix(r.URL.EscapedPath(), "/v3alpha/purl/"))
			if err != nil || !strings.HasPrefix(decoded, "pkg:cargo/") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			nameVersion := strings.TrimPrefix(decoded, "pkg:cargo/")
			if id := advisories[nameVersion]; id != "" {
				_, _ = fmt.Fprintf(w, `{"version":{"advisoryKeys":[{"id":%q}]}}`, id)
				return
			}
			_, _ = w.Write([]byte(`{"version":{"advisoryKeys":[]}}`))
		}
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0, BatchSize: 100})
	const purl = "pkg:cargo/sample-crate@0.1.0"
	graph := client.FetchDependenciesBatch(context.Background(), []string{purl})[purl]
	if graph == nil {
		t.Fatal("dependency graph is nil")
	}
	got, err := client.FetchTransitiveAdvisoryKeys(context.Background(), graph)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]AdvisoryKey{"memchr@2.7.4": {{ID: "RUSTSEC-0000-0003"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("advisory keys = %v, want %v", got, want)
	}
}

func TestFetchDependenciesBatch_CargoRequirementsUnavailableKeepsFullGraph(t *testing.T) {
	dependencies, err := os.ReadFile(filepath.Join("testdata", "cargo_dev_optional", "dependencies.json"))
	if err != nil {
		t.Fatal(err)
	}
	const path = "/v3alpha/systems/cargo/packages/sample-crate/versions/0.1.0"
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"not found", http.StatusNotFound, ""},
		{"server error", http.StatusInternalServerError, ""},
		{"missing cargo object", http.StatusOK, `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case path + ":dependencies":
					_, _ = w.Write(dependencies)
				case path + ":requirements":
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()
			client := NewDepsDevClient(&config.DepsDevConfig{BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0, BatchSize: 100})
			const purl = "pkg:cargo/sample-crate@0.1.0"
			graph := client.FetchDependenciesBatch(context.Background(), []string{purl})[purl]
			if graph == nil {
				t.Fatal("dependency graph is nil")
			}
			if direct, transitive := graph.CountByRelation(); direct != 9 || transitive != 6 {
				t.Errorf("counts = (%d, %d), want (9, 6)", direct, transitive)
			}
		})
	}
}

func TestFetchDependenciesBatch_NonCargoNeverCallsRequirements(t *testing.T) {
	const graphJSON = `{"nodes":[{"versionKey":{"system":"NPM","name":"sample","version":"1.0.0"},"relation":"SELF"},{"versionKey":{"system":"NPM","name":"left-pad","version":"1.0.0"},"relation":"DIRECT"}],"edges":[{"fromNode":0,"toNode":1}],"error":""}`
	var requirementsCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ":requirements") {
			requirementsCalls.Add(1)
		}
		if r.URL.Path != "/v3alpha/systems/npm/packages/sample/versions/1.0.0:dependencies" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(graphJSON))
	}))
	defer srv.Close()
	client := NewDepsDevClient(&config.DepsDevConfig{BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0, BatchSize: 100})
	const purl = "pkg:npm/sample@1.0.0"
	graph := client.FetchDependenciesBatch(context.Background(), []string{purl})[purl]
	if graph == nil {
		t.Fatal("dependency graph is nil")
	}
	if got := requirementsCalls.Load(); got != 0 {
		t.Errorf("requirements calls = %d, want 0", got)
	}
	if direct, transitive := graph.CountByRelation(); direct != 1 || transitive != 0 {
		t.Errorf("counts = (%d, %d), want (1, 0)", direct, transitive)
	}
	if len(graph.Nodes) != 2 || graph.Nodes[0].VersionKey.Name != "sample" || graph.Nodes[1].VersionKey.Name != "left-pad" {
		t.Errorf("nodes = %+v, want sample and left-pad", graph.Nodes)
	}
	if len(graph.Edges) != 1 || graph.Edges[0].FromNode != 0 || graph.Edges[0].ToNode != 1 {
		t.Errorf("edges = %+v, want 0 -> 1", graph.Edges)
	}
}

func TestFetchDependenciesBatch_CargoNoRuntimeDependencies(t *testing.T) {
	const graphJSON = `{"nodes":[{"versionKey":{"system":"CARGO","name":"dev-only","version":"0.1.0"},"relation":"SELF"},{"versionKey":{"system":"CARGO","name":"tar","version":"0.4.40"},"relation":"DIRECT"},{"versionKey":{"system":"CARGO","name":"rand","version":"0.8.5"},"relation":"DIRECT"},{"versionKey":{"system":"CARGO","name":"filetime","version":"0.2.25"},"relation":"INDIRECT"}],"edges":[{"fromNode":0,"toNode":1},{"fromNode":0,"toNode":2},{"fromNode":1,"toNode":3}],"error":""}`
	const requirementsJSON = `{"cargo":{"dependencies":[{"name":"tar","requirement":"^0.4","kind":"dev","optional":false,"packageAlias":"","usesDefaultFeatures":true,"features":[],"target":""},{"name":"rand","requirement":"^0.8","kind":"dev","optional":false,"packageAlias":"","usesDefaultFeatures":true,"features":[],"target":""}],"features":[{"name":"default","implies":[]}]}}`
	const path = "/v3alpha/systems/cargo/packages/dev-only/versions/0.1.0"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case path + ":dependencies":
			_, _ = w.Write([]byte(graphJSON))
		case path + ":requirements":
			_, _ = w.Write([]byte(requirementsJSON))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	client := NewDepsDevClient(&config.DepsDevConfig{BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0, BatchSize: 100})
	const purl = "pkg:cargo/dev-only@0.1.0"
	graph := client.FetchDependenciesBatch(context.Background(), []string{purl})[purl]
	if graph == nil {
		t.Fatal("dependency graph is nil")
	}
	if direct, transitive := graph.CountByRelation(); direct != 0 || transitive != 0 {
		t.Errorf("counts = (%d, %d), want (0, 0)", direct, transitive)
	}
	if len(graph.Nodes) != 1 || graph.Nodes[0].Relation != "SELF" || graph.Nodes[0].VersionKey.Name != "dev-only" {
		t.Errorf("nodes = %+v, want only dev-only SELF", graph.Nodes)
	}
	if len(graph.Edges) != 0 {
		t.Errorf("edges = %+v, want none", graph.Edges)
	}
}
