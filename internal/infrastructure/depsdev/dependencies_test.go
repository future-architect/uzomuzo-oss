package depsdev

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	commonpurl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
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

// TestFetchDependenciesBatch_VersionFallback_Recovers exercises issue #319:
// when the primary version returns 404, FetchDependenciesBatch should consult
// /packages/{name}, pick the next most-recent non-deprecated version, and
// retry :dependencies. The recovered response must be keyed under the input's
// canonical (versionless) key so downstream consumers see HasDependencyGraph=true.
func TestFetchDependenciesBatch_VersionFallback_Recovers(t *testing.T) {
	// Fixture mirrors the issue's scenario: react@19.2.5 → 404 on :dependencies,
	// react@19.1.0 → 200 with SELF-only (genuine leaf).
	packageVersionsPayload := `{"versions":[
		{"versionKey":{"version":"19.2.5"},"publishedAt":"2026-04-10T00:00:00Z","isDeprecated":false},
		{"versionKey":{"version":"19.1.0"},"publishedAt":"2026-02-01T00:00:00Z","isDeprecated":false},
		{"versionKey":{"version":"19.0.9-beta"},"publishedAt":"2026-01-15T00:00:00Z","isDeprecated":true},
		{"versionKey":{"version":"18.3.1"},"publishedAt":"2025-06-01T00:00:00Z","isDeprecated":false}
	]}`
	leafGraph := `{"nodes":[{"versionKey":{"system":"NPM","name":"react","version":"19.1.0"},"relation":"SELF","errors":[]}],"edges":[]}`

	var listCalls, depsCalls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3alpha/systems/npm/packages/react":
			atomic.AddInt64(&listCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(packageVersionsPayload))
		case "/v3alpha/systems/npm/packages/react/versions/19.2.5:dependencies":
			atomic.AddInt64(&depsCalls, 1)
			w.WriteHeader(http.StatusNotFound)
		case "/v3alpha/systems/npm/packages/react/versions/19.1.0:dependencies":
			atomic.AddInt64(&depsCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(leafGraph))
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0, BatchSize: 10,
	})

	results := client.FetchDependenciesBatch(context.Background(), []string{"pkg:npm/react@19.2.5"})

	key := commonpurl.CanonicalKey("pkg:npm/react@19.2.5")
	resp, ok := results[key]
	if !ok || resp == nil {
		t.Fatalf("expected recovered response under %q, got %+v", key, results)
	}
	direct, transitive := resp.CountByRelation()
	if direct != 0 || transitive != 0 {
		t.Errorf("counts = (%d, %d), want (0, 0)", direct, transitive)
	}
	if got := atomic.LoadInt64(&listCalls); got != 1 {
		t.Errorf("expected 1 package-versions call, got %d", got)
	}
	if got := atomic.LoadInt64(&depsCalls); got != 2 {
		t.Errorf("expected 2 :dependencies calls (primary + 1 fallback), got %d", got)
	}
}

// TestFetchDependenciesBatch_VersionFallback_BoundedAttempts ensures the helper
// makes at most maxDependencyFallbackVersions retry calls even when many
// candidates are available.
func TestFetchDependenciesBatch_VersionFallback_BoundedAttempts(t *testing.T) {
	packageVersionsPayload := `{"versions":[
		{"versionKey":{"version":"3.0.0"},"publishedAt":"2026-04-10T00:00:00Z"},
		{"versionKey":{"version":"2.0.0"},"publishedAt":"2026-03-10T00:00:00Z"},
		{"versionKey":{"version":"1.0.0"},"publishedAt":"2026-02-10T00:00:00Z"},
		{"versionKey":{"version":"0.9.0"},"publishedAt":"2026-01-10T00:00:00Z"}
	]}`

	var depsCalls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3alpha/systems/npm/packages/broken":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(packageVersionsPayload))
		default:
			// Every :dependencies call returns 404.
			atomic.AddInt64(&depsCalls, 1)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0, BatchSize: 10,
	})

	results := client.FetchDependenciesBatch(context.Background(), []string{"pkg:npm/broken@3.0.0"})
	if len(results) != 0 {
		t.Errorf("expected empty results (all attempts 404'd), got %+v", results)
	}
	// Primary (3.0.0) + maxDependencyFallbackVersions (2.0.0, 1.0.0).
	want := int64(1 + maxDependencyFallbackVersions)
	if got := atomic.LoadInt64(&depsCalls); got != want {
		t.Errorf("expected %d :dependencies calls (primary + %d fallback), got %d", want, maxDependencyFallbackVersions, got)
	}
}

// TestFetchDependenciesBatch_VersionFallback_SkipsPrimaryVersion verifies the
// fallback does not retry the version that just 404'd even if it appears
// first in the package-versions listing.
func TestFetchDependenciesBatch_VersionFallback_SkipsPrimaryVersion(t *testing.T) {
	packageVersionsPayload := `{"versions":[
		{"versionKey":{"version":"19.2.5"},"publishedAt":"2026-04-10T00:00:00Z"},
		{"versionKey":{"version":"19.1.0"},"publishedAt":"2026-02-01T00:00:00Z"}
	]}`
	leafGraph := `{"nodes":[{"versionKey":{"system":"NPM","name":"react","version":"19.1.0"},"relation":"SELF"}],"edges":[]}`

	calledVersions := make(chan string, 5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3alpha/systems/npm/packages/react":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(packageVersionsPayload))
		case "/v3alpha/systems/npm/packages/react/versions/19.2.5:dependencies":
			calledVersions <- "19.2.5"
			w.WriteHeader(http.StatusNotFound)
		case "/v3alpha/systems/npm/packages/react/versions/19.1.0:dependencies":
			calledVersions <- "19.1.0"
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(leafGraph))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0, BatchSize: 10,
	})

	_ = client.FetchDependenciesBatch(context.Background(), []string{"pkg:npm/react@19.2.5"})
	close(calledVersions)

	// Order of calls: primary 19.2.5 (404) → fallback 19.1.0 (200). The 19.2.5
	// entry in the versions listing must be skipped since it already 404'd.
	var got []string
	for v := range calledVersions {
		got = append(got, v)
	}
	if len(got) != 2 || got[0] != "19.2.5" || got[1] != "19.1.0" {
		t.Errorf("call sequence = %v, want [19.2.5 19.1.0]", got)
	}
}

// TestFetchDependenciesBatch_VersionFallback_PrefersStableOverPrerelease
// documents the deps.dev quirk that motivated the stable-over-prerelease sort:
// canary/beta tags are often more recent by publishedAt but their
// :dependencies endpoint routinely 404s. The fallback must prefer a slightly
// older stable release over a newer pre-release to recover the graph.
func TestFetchDependenciesBatch_VersionFallback_PrefersStableOverPrerelease(t *testing.T) {
	// 19.3.0-canary has the most recent publishedAt; 19.1.0 is stable but older.
	packageVersionsPayload := `{"versions":[
		{"versionKey":{"version":"19.2.5"},"publishedAt":"2026-04-10T00:00:00Z"},
		{"versionKey":{"version":"19.3.0-canary-x"},"publishedAt":"2026-04-15T00:00:00Z"},
		{"versionKey":{"version":"19.1.0"},"publishedAt":"2026-02-01T00:00:00Z"}
	]}`
	leaf := `{"nodes":[{"relation":"SELF"}],"edges":[]}`

	calls := make(chan string, 5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3alpha/systems/npm/packages/react":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(packageVersionsPayload))
		case "/v3alpha/systems/npm/packages/react/versions/19.2.5:dependencies":
			calls <- "19.2.5"
			w.WriteHeader(http.StatusNotFound)
		case "/v3alpha/systems/npm/packages/react/versions/19.1.0:dependencies":
			calls <- "19.1.0"
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(leaf))
		case "/v3alpha/systems/npm/packages/react/versions/19.3.0-canary-x:dependencies":
			// If the sort regresses and the canary is attempted, record and 404.
			calls <- "19.3.0-canary-x"
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0, BatchSize: 10,
	})

	results := client.FetchDependenciesBatch(context.Background(), []string{"pkg:npm/react@19.2.5"})
	close(calls)

	if results[commonpurl.CanonicalKey("pkg:npm/react@19.2.5")] == nil {
		t.Fatalf("expected recovered response, got %+v", results)
	}
	var got []string
	for c := range calls {
		got = append(got, c)
	}
	// Expected: primary 19.2.5 → fallback 19.1.0 (stable first). The canary
	// must NOT be tried before the stable candidate.
	if len(got) < 2 || got[0] != "19.2.5" || got[1] != "19.1.0" {
		t.Errorf("call sequence = %v, want [19.2.5 19.1.0 ...]", got)
	}
}

// TestFetchDependenciesBatch_VersionFallback_PrefersClosestSemver verifies
// that, within the stable tier, the fallback tries the highest semver first —
// not the most-recently-indexed. deps.dev's publishedAt in the package listing
// reflects index time rather than upstream release order, so a batch
// re-index of an older patch can otherwise misdirect the fallback toward
// 19.0.x when 19.2.4 is available. Both are stable, but 19.2.4 is closer to
// the primary=19.2.5's dependency surface.
func TestFetchDependenciesBatch_VersionFallback_PrefersClosestSemver(t *testing.T) {
	// 19.0.5 has a MORE RECENT publishedAt than 19.2.4 (simulating deps.dev
	// re-index), but 19.2.4 is the higher semver and should be tried first.
	packageVersionsPayload := `{"versions":[
		{"versionKey":{"version":"19.2.5"},"publishedAt":"2026-04-08T18:39:24Z"},
		{"versionKey":{"version":"19.2.4"},"publishedAt":"2026-01-26T18:23:10Z"},
		{"versionKey":{"version":"19.0.5"},"publishedAt":"2026-04-08T18:40:53Z"}
	]}`
	leaf := `{"nodes":[{"relation":"SELF"}],"edges":[]}`

	calls := make(chan string, 5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3alpha/systems/npm/packages/react":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(packageVersionsPayload))
		case "/v3alpha/systems/npm/packages/react/versions/19.2.5:dependencies":
			calls <- "19.2.5"
			w.WriteHeader(http.StatusNotFound)
		case "/v3alpha/systems/npm/packages/react/versions/19.2.4:dependencies":
			calls <- "19.2.4"
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(leaf))
		case "/v3alpha/systems/npm/packages/react/versions/19.0.5:dependencies":
			calls <- "19.0.5"
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(leaf))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0, BatchSize: 10,
	})
	_ = client.FetchDependenciesBatch(context.Background(), []string{"pkg:npm/react@19.2.5"})
	close(calls)

	var got []string
	for c := range calls {
		got = append(got, c)
	}
	if len(got) < 2 || got[0] != "19.2.5" || got[1] != "19.2.4" {
		t.Errorf("call sequence = %v, want primary=19.2.5 then fallback=19.2.4 (highest semver < primary)", got)
	}
}

// TestFetchDependenciesBatch_NoFallbackForLeafWithGraph ensures the fallback
// does NOT run when the primary call returns a valid (but SELF-only) response.
// The zero-node response must be preserved per PR #315's HasDependencyGraph
// contract: "leaf package, graph was collected".
func TestFetchDependenciesBatch_NoFallbackForLeafWithGraph(t *testing.T) {
	var listCalls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3alpha/systems/npm/packages/react/versions/19.1.0:dependencies":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"nodes":[{"relation":"SELF"}],"edges":[]}`)
		case "/v3alpha/systems/npm/packages/react":
			atomic.AddInt64(&listCalls, 1)
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0, BatchSize: 10,
	})

	results := client.FetchDependenciesBatch(context.Background(), []string{"pkg:npm/react@19.1.0"})
	key := commonpurl.CanonicalKey("pkg:npm/react@19.1.0")
	if results[key] == nil {
		t.Fatalf("expected leaf response to be preserved, got %+v", results)
	}
	if got := atomic.LoadInt64(&listCalls); got != 0 {
		t.Errorf("expected 0 package-versions calls for successful primary, got %d", got)
	}
}

// TestFetchDependenciesBatch_VersionFallback_ContextCancelled verifies the
// end-to-end cancellation contract: once the caller cancels mid-workflow, no
// further retry :dependencies calls fire. Cancellation is triggered inside
// the primary handler; depending on Go's net/http scheduling the cancellation
// surfaces either as (a) FetchDependencies returning a ctx-wrapped error (and
// FetchDependenciesBatch exiting before fallback), or (b) the fallback
// helper's own `ctx.Err()` guard firing before the retry dispatch. Both are
// acceptable — the externally observable property under test is "no extra
// :dependencies calls after cancel".
func TestFetchDependenciesBatch_VersionFallback_ContextCancelled(t *testing.T) {
	packageVersionsPayload := `{"versions":[
		{"versionKey":{"version":"2.0.0"},"publishedAt":"2026-04-10T00:00:00Z"},
		{"versionKey":{"version":"1.0.0"},"publishedAt":"2026-03-10T00:00:00Z"}
	]}`

	var retryCalls int64
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3alpha/systems/npm/packages/broken/versions/3.0.0:dependencies":
			cancel()
			w.WriteHeader(http.StatusNotFound)
		case "/v3alpha/systems/npm/packages/broken":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(packageVersionsPayload))
		default:
			// Any retry :dependencies path must NOT be reached after cancel.
			atomic.AddInt64(&retryCalls, 1)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0, BatchSize: 10,
	})
	results := client.FetchDependenciesBatch(ctx, []string{"pkg:npm/broken@3.0.0"})
	if len(results) != 0 {
		t.Errorf("expected empty results on cancellation, got %+v", results)
	}
	if got := atomic.LoadInt64(&retryCalls); got != 0 {
		t.Errorf("expected 0 retry :dependencies calls after ctx cancel, got %d", got)
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
