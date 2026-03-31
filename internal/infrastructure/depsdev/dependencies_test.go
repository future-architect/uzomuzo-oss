package depsdev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

func TestFetchDependencies_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"nodes": [
				{"versionKey":{"system":"NPM","name":"express","version":"4.21.2"},"relation":"SELF","errors":[]},
				{"versionKey":{"system":"NPM","name":"accepts","version":"1.3.8"},"relation":"DIRECT","errors":[]},
				{"versionKey":{"system":"NPM","name":"mime-types","version":"2.1.35"},"relation":"INDIRECT","errors":[]},
				{"versionKey":{"system":"NPM","name":"mime-db","version":"1.52.0"},"relation":"INDIRECT","errors":[]}
			],
			"edges": [
				{"fromNode":0,"toNode":1,"requirement":"~1.3.8"},
				{"fromNode":1,"toNode":2,"requirement":"~2.1.34"},
				{"fromNode":2,"toNode":3,"requirement":"1.52.0"}
			]
		}`))
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5e9,
		MaxRetries: 0,
		BatchSize:  100,
	})

	resp, err := client.FetchDependencies(context.Background(), "pkg:npm/express@4.21.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Nodes) != 4 {
		t.Errorf("Nodes count = %d, want 4", len(resp.Nodes))
	}
	if len(resp.Edges) != 3 {
		t.Errorf("Edges count = %d, want 3", len(resp.Edges))
	}

	direct, transitive := resp.CountByRelation()
	if direct != 1 {
		t.Errorf("direct count = %d, want 1", direct)
	}
	if transitive != 2 {
		t.Errorf("transitive count = %d, want 2", transitive)
	}
}

func TestFetchDependencies_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5e9,
		MaxRetries: 0,
		BatchSize:  100,
	})

	resp, err := client.FetchDependencies(context.Background(), "pkg:npm/nonexistent@1.0.0")
	if err != nil {
		t.Fatalf("unexpected error on 404: %v", err)
	}
	if resp != nil {
		t.Errorf("expected nil response for 404, got %+v", resp)
	}
}

func TestFetchDependencies_VersionlessSkipped(t *testing.T) {
	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    "http://unused",
		Timeout:    5e9,
		MaxRetries: 0,
		BatchSize:  100,
	})

	resp, err := client.FetchDependencies(context.Background(), "pkg:npm/express")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != nil {
		t.Errorf("expected nil response for versionless PURL, got %+v", resp)
	}
}

func TestFetchDependenciesBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"nodes": [
				{"versionKey":{"system":"NPM","name":"test","version":"1.0.0"},"relation":"SELF","errors":[]},
				{"versionKey":{"system":"NPM","name":"dep","version":"2.0.0"},"relation":"DIRECT","errors":[]}
			],
			"edges": [{"fromNode":0,"toNode":1,"requirement":"^2.0.0"}]
		}`))
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5e9,
		MaxRetries: 0,
		BatchSize:  100,
	})

	purls := []string{"pkg:npm/express@4.18.2", "pkg:maven/org.slf4j/slf4j-api@2.0.16"}
	results := client.FetchDependenciesBatch(context.Background(), purls)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for key, resp := range results {
		if len(resp.Nodes) != 2 {
			t.Errorf("key=%s: Nodes count = %d, want 2", key, len(resp.Nodes))
		}
	}
}

func TestFetchDependenciesBatch_Empty(t *testing.T) {
	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    "http://unused",
		Timeout:    5e9,
		MaxRetries: 0,
		BatchSize:  100,
	})

	results := client.FetchDependenciesBatch(context.Background(), nil)
	if len(results) != 0 {
		t.Errorf("expected empty map, got %d entries", len(results))
	}
}

func TestDependenciesResponse_CountByRelation(t *testing.T) {
	tests := []struct {
		name           string
		resp           DependenciesResponse
		wantDirect     int
		wantTransitive int
	}{
		{
			name: "mixed relations",
			resp: DependenciesResponse{
				Nodes: []DependencyNode{
					{Relation: "SELF"},
					{Relation: "DIRECT"},
					{Relation: "DIRECT"},
					{Relation: "INDIRECT"},
				},
			},
			wantDirect:     2,
			wantTransitive: 1,
		},
		{
			name:           "empty nodes",
			resp:           DependenciesResponse{},
			wantDirect:     0,
			wantTransitive: 0,
		},
		{
			name: "only self",
			resp: DependenciesResponse{
				Nodes: []DependencyNode{{Relation: "SELF"}},
			},
			wantDirect:     0,
			wantTransitive: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			direct, transitive := tt.resp.CountByRelation()
			if direct != tt.wantDirect {
				t.Errorf("direct = %d, want %d", direct, tt.wantDirect)
			}
			if transitive != tt.wantTransitive {
				t.Errorf("transitive = %d, want %d", transitive, tt.wantTransitive)
			}
		})
	}
}
