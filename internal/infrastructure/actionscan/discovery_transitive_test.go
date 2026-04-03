package actionscan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
)

// fakeGitHubAPI builds an httptest.Server that serves GitHub Contents API responses
// needed for BFS transitive discovery tests.
//
// routes maps "owner/repo/path" to response content:
//   - directory listings: JSON array of DirectoryEntry
//   - file content: raw YAML bytes
//
// Paths not in the map return 404.
func fakeGitHubAPI(t *testing.T, routes map[string][]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path: /repos/{owner}/{repo}/contents/{path}
		path := strings.TrimPrefix(r.URL.Path, "/repos/")
		// Split into owner/repo and the rest after /contents/
		parts := strings.SplitN(path, "/contents/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		ownerRepo := parts[0]
		contentPath := parts[1]
		key := ownerRepo + "/" + contentPath

		data, ok := routes[key]
		if !ok {
			http.NotFound(w, r)
			return
		}

		accept := r.Header.Get("Accept")
		if accept == "application/vnd.github.raw" {
			// File content request — return raw bytes.
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(data)
			return
		}
		// Directory listing request — data is already JSON.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
}

// directoryJSON builds a JSON array for a GitHub Contents API directory listing.
func directoryJSON(t *testing.T, files []string) []byte {
	t.Helper()
	type entry struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	var entries []entry
	for _, f := range files {
		entries = append(entries, entry{
			Name: f,
			Path: ".github/workflows/" + f,
			Type: "file",
		})
	}
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("directoryJSON: %v", err)
	}
	return data
}

// newTestService creates a DiscoveryService pointing at the given test server.
func newTestService(t *testing.T, serverURL string) *DiscoveryService {
	t.Helper()
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token:   "test-token",
			BaseURL: serverURL,
		},
	}
	client := github.NewClient(cfg)
	svc, err := NewDiscoveryService(client, 5)
	if err != nil {
		t.Fatalf("NewDiscoveryService: %v", err)
	}
	return svc
}

// TestResolveTransitive_SingleLevel tests A (composite) → B where B is a node action.
// Expected: B appears in transitive results with via=A.
func TestResolveTransitive_SingleLevel(t *testing.T) {
	//   myorg/myrepo workflow uses: alpha/action-a@v1
	//   alpha/action-a is composite and uses: beta/action-b@v1
	//   beta/action-b is a node action (no further expansion)

	workflowYAML := []byte(`
name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: alpha/action-a@v1
`)

	compositeActionA := []byte(`
name: Action A
runs:
  using: composite
  steps:
    - uses: beta/action-b@v1
`)

	nodeActionB := []byte(`
name: Action B
runs:
  using: node20
  main: index.js
`)

	routes := map[string][]byte{
		"myorg/myrepo/.github/workflows":        directoryJSON(t, []string{"ci.yml"}),
		"myorg/myrepo/.github/workflows/ci.yml": workflowYAML,
		"alpha/action-a/action.yml":              compositeActionA,
		"beta/action-b/action.yml":               nodeActionB,
	}

	srv := fakeGitHubAPI(t, routes)
	defer srv.Close()

	svc := newTestService(t, srv.URL)
	directURLs, _, transitiveActions, errs, err := svc.DiscoverActions(
		context.Background(),
		[]string{"https://github.com/myorg/myrepo"},
		true,
	)
	if err != nil {
		t.Fatalf("DiscoverActions: %v", err)
	}

	// Direct: alpha/action-a
	if len(directURLs) != 1 {
		t.Fatalf("expected 1 direct URL, got %d: %v", len(directURLs), directURLs)
	}
	if directURLs[0] != "https://github.com/alpha/action-a" {
		t.Errorf("direct URL = %q, want %q", directURLs[0], "https://github.com/alpha/action-a")
	}

	// Transitive: beta/action-b via alpha/action-a
	if len(transitiveActions) != 1 {
		t.Fatalf("expected 1 transitive action, got %d: %v", len(transitiveActions), transitiveActions)
	}
	via, ok := transitiveActions["https://github.com/beta/action-b"]
	if !ok {
		t.Fatal("expected beta/action-b in transitive actions")
	}
	if via != "https://github.com/alpha/action-a" {
		t.Errorf("via = %q, want %q", via, "https://github.com/alpha/action-a")
	}

	// No critical errors (404 for action.yml fallback is acceptable).
	for src, e := range errs {
		if !strings.Contains(src, "action.yaml") {
			t.Errorf("unexpected error for %q: %v", src, e)
		}
	}
}

// TestResolveTransitive_MultiLevel tests A → B → C chain.
// Expected: both B and C appear in transitive; C's via is A (the root direct action).
func TestResolveTransitive_MultiLevel(t *testing.T) {
	workflowYAML := []byte(`
name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: alpha/action-a@v1
`)

	compositeA := []byte(`
name: Action A
runs:
  using: composite
  steps:
    - uses: beta/action-b@v1
`)

	compositeB := []byte(`
name: Action B
runs:
  using: composite
  steps:
    - uses: gamma/action-c@v1
`)

	nodeC := []byte(`
name: Action C
runs:
  using: node20
  main: index.js
`)

	routes := map[string][]byte{
		"myorg/myrepo/.github/workflows":        directoryJSON(t, []string{"ci.yml"}),
		"myorg/myrepo/.github/workflows/ci.yml": workflowYAML,
		"alpha/action-a/action.yml":              compositeA,
		"beta/action-b/action.yml":               compositeB,
		"gamma/action-c/action.yml":              nodeC,
	}

	srv := fakeGitHubAPI(t, routes)
	defer srv.Close()

	svc := newTestService(t, srv.URL)
	directURLs, _, transitiveActions, _, err := svc.DiscoverActions(
		context.Background(),
		[]string{"https://github.com/myorg/myrepo"},
		true,
	)
	if err != nil {
		t.Fatalf("DiscoverActions: %v", err)
	}

	if len(directURLs) != 1 || directURLs[0] != "https://github.com/alpha/action-a" {
		t.Fatalf("expected [alpha/action-a], got %v", directURLs)
	}

	// Should have 2 transitive: beta/action-b and gamma/action-c.
	if len(transitiveActions) != 2 {
		t.Fatalf("expected 2 transitive actions, got %d: %v", len(transitiveActions), transitiveActions)
	}

	// beta/action-b via alpha/action-a
	if via := transitiveActions["https://github.com/beta/action-b"]; via != "https://github.com/alpha/action-a" {
		t.Errorf("beta/action-b via = %q, want alpha/action-a", via)
	}

	// gamma/action-c via alpha/action-a (root direct, not intermediate beta/action-b)
	if via := transitiveActions["https://github.com/gamma/action-c"]; via != "https://github.com/alpha/action-a" {
		t.Errorf("gamma/action-c via = %q, want alpha/action-a", via)
	}
}

// TestResolveTransitive_CyclicDependency tests A → B → A cycle.
// Expected: no infinite loop, B appears in transitive.
func TestResolveTransitive_CyclicDependency(t *testing.T) {
	workflowYAML := []byte(`
name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: alpha/action-a@v1
`)

	compositeA := []byte(`
name: Action A
runs:
  using: composite
  steps:
    - uses: beta/action-b@v1
`)

	// B references A back — cycle.
	compositeB := []byte(`
name: Action B
runs:
  using: composite
  steps:
    - uses: alpha/action-a@v1
`)

	routes := map[string][]byte{
		"myorg/myrepo/.github/workflows":        directoryJSON(t, []string{"ci.yml"}),
		"myorg/myrepo/.github/workflows/ci.yml": workflowYAML,
		"alpha/action-a/action.yml":              compositeA,
		"beta/action-b/action.yml":               compositeB,
	}

	srv := fakeGitHubAPI(t, routes)
	defer srv.Close()

	svc := newTestService(t, srv.URL)
	directURLs, _, transitiveActions, _, err := svc.DiscoverActions(
		context.Background(),
		[]string{"https://github.com/myorg/myrepo"},
		true,
	)
	if err != nil {
		t.Fatalf("DiscoverActions: %v", err)
	}

	if len(directURLs) != 1 || directURLs[0] != "https://github.com/alpha/action-a" {
		t.Fatalf("expected [alpha/action-a], got %v", directURLs)
	}

	// beta/action-b is transitive; alpha/action-a should NOT appear again as transitive
	// because it's already a direct action (in seen set).
	if len(transitiveActions) != 1 {
		t.Fatalf("expected 1 transitive action, got %d: %v", len(transitiveActions), transitiveActions)
	}
	if _, ok := transitiveActions["https://github.com/beta/action-b"]; !ok {
		t.Error("expected beta/action-b in transitive actions")
	}
}

// TestResolveTransitive_NonCompositeNotExpanded tests that node/docker actions
// are not expanded (their action.yml is fetched but they produce no children).
func TestResolveTransitive_NonCompositeNotExpanded(t *testing.T) {
	workflowYAML := []byte(`
name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: alpha/node-action@v1
      - uses: beta/docker-action@v1
`)

	nodeAction := []byte(`
name: Node Action
runs:
  using: node20
  main: index.js
`)

	dockerAction := []byte(`
name: Docker Action
runs:
  using: docker
  image: Dockerfile
`)

	routes := map[string][]byte{
		"myorg/myrepo/.github/workflows":        directoryJSON(t, []string{"ci.yml"}),
		"myorg/myrepo/.github/workflows/ci.yml": workflowYAML,
		"alpha/node-action/action.yml":           nodeAction,
		"beta/docker-action/action.yml":          dockerAction,
	}

	srv := fakeGitHubAPI(t, routes)
	defer srv.Close()

	svc := newTestService(t, srv.URL)
	directURLs, _, transitiveActions, _, err := svc.DiscoverActions(
		context.Background(),
		[]string{"https://github.com/myorg/myrepo"},
		true,
	)
	if err != nil {
		t.Fatalf("DiscoverActions: %v", err)
	}

	sort.Strings(directURLs)
	wantDirect := []string{
		"https://github.com/alpha/node-action",
		"https://github.com/beta/docker-action",
	}
	if fmt.Sprintf("%v", directURLs) != fmt.Sprintf("%v", wantDirect) {
		t.Errorf("direct URLs = %v, want %v", directURLs, wantDirect)
	}

	if len(transitiveActions) != 0 {
		t.Errorf("expected 0 transitive actions for non-composite, got %d: %v", len(transitiveActions), transitiveActions)
	}
}

// TestResolveTransitive_Disabled verifies resolveTransitive=false skips BFS.
func TestResolveTransitive_Disabled(t *testing.T) {
	workflowYAML := []byte(`
name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: alpha/action-a@v1
`)

	compositeA := []byte(`
name: Action A
runs:
  using: composite
  steps:
    - uses: beta/action-b@v1
`)

	routes := map[string][]byte{
		"myorg/myrepo/.github/workflows":        directoryJSON(t, []string{"ci.yml"}),
		"myorg/myrepo/.github/workflows/ci.yml": workflowYAML,
		// action-a is composite, but we should never fetch it when resolveTransitive=false.
		"alpha/action-a/action.yml": compositeA,
	}

	srv := fakeGitHubAPI(t, routes)
	defer srv.Close()

	svc := newTestService(t, srv.URL)
	directURLs, _, transitiveActions, _, err := svc.DiscoverActions(
		context.Background(),
		[]string{"https://github.com/myorg/myrepo"},
		false, // disabled
	)
	if err != nil {
		t.Fatalf("DiscoverActions: %v", err)
	}

	if len(directURLs) != 1 || directURLs[0] != "https://github.com/alpha/action-a" {
		t.Fatalf("expected [alpha/action-a], got %v", directURLs)
	}

	if transitiveActions != nil {
		t.Errorf("expected nil transitive actions when disabled, got %v", transitiveActions)
	}
}

// TestLocalAction_SingleLevel tests workflow → local composite action → external action.
// External actions found inside local actions should appear in direct URLs.
func TestLocalAction_SingleLevel(t *testing.T) {
	workflowYAML := []byte(`
name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: ./.github/actions/build-frontend
`)

	localComposite := []byte(`
name: Build Frontend
runs:
  using: composite
  steps:
    - uses: actions/setup-node@v4
    - uses: actions/cache@v4
`)

	routes := map[string][]byte{
		"myorg/myrepo/.github/workflows":                         directoryJSON(t, []string{"ci.yml"}),
		"myorg/myrepo/.github/workflows/ci.yml":                  workflowYAML,
		"myorg/myrepo/.github/actions/build-frontend/action.yml": localComposite,
	}

	srv := fakeGitHubAPI(t, routes)
	defer srv.Close()

	svc := newTestService(t, srv.URL)
	directURLs, localActions, _, _, err := svc.DiscoverActions(
		context.Background(),
		[]string{"https://github.com/myorg/myrepo"},
		false, // transitive disabled — local resolution is part of Phase 1
	)
	if err != nil {
		t.Fatalf("DiscoverActions: %v", err)
	}

	// actions/checkout is direct (from workflow), not local.
	if len(directURLs) != 1 || directURLs[0] != "https://github.com/actions/checkout" {
		t.Errorf("direct URLs = %v, want [actions/checkout]", directURLs)
	}

	// actions/setup-node and actions/cache are local (from .github/actions/build-frontend).
	if len(localActions) != 2 {
		t.Fatalf("expected 2 local actions, got %d: %v", len(localActions), localActions)
	}
	for _, url := range []string{"https://github.com/actions/setup-node", "https://github.com/actions/cache"} {
		via, ok := localActions[url]
		if !ok {
			t.Errorf("expected %s in local actions", url)
			continue
		}
		if via != ".github/actions/build-frontend" {
			t.Errorf("local action %s via = %q, want %q", url, via, ".github/actions/build-frontend")
		}
	}
}

// TestLocalAction_NestedLocals tests local action → local action → external action chain.
func TestLocalAction_NestedLocals(t *testing.T) {
	workflowYAML := []byte(`
name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/wrapper
`)

	wrapperComposite := []byte(`
name: Wrapper
runs:
  using: composite
  steps:
    - uses: ./.github/actions/inner
    - uses: actions/checkout@v4
`)

	innerComposite := []byte(`
name: Inner
runs:
  using: composite
  steps:
    - uses: actions/setup-go@v5
`)

	routes := map[string][]byte{
		"myorg/myrepo/.github/workflows":                    directoryJSON(t, []string{"ci.yml"}),
		"myorg/myrepo/.github/workflows/ci.yml":             workflowYAML,
		"myorg/myrepo/.github/actions/wrapper/action.yml":   wrapperComposite,
		"myorg/myrepo/.github/actions/inner/action.yml":     innerComposite,
	}

	srv := fakeGitHubAPI(t, routes)
	defer srv.Close()

	svc := newTestService(t, srv.URL)
	directURLs, localActions, _, _, err := svc.DiscoverActions(
		context.Background(),
		[]string{"https://github.com/myorg/myrepo"},
		false,
	)
	if err != nil {
		t.Fatalf("DiscoverActions: %v", err)
	}

	// No direct actions (workflow only uses local ./ actions).
	if len(directURLs) != 0 {
		t.Errorf("expected 0 direct URLs, got %v", directURLs)
	}

	// Both external actions discovered via local composite actions.
	if len(localActions) != 2 {
		t.Fatalf("expected 2 local actions, got %d: %v", len(localActions), localActions)
	}
	// actions/checkout comes from wrapper (first-seen).
	if via := localActions["https://github.com/actions/checkout"]; via != ".github/actions/wrapper" {
		t.Errorf("actions/checkout via = %q, want %q", via, ".github/actions/wrapper")
	}
	// actions/setup-go comes from inner (via nested BFS).
	if via := localActions["https://github.com/actions/setup-go"]; via != ".github/actions/inner" {
		t.Errorf("actions/setup-go via = %q, want %q", via, ".github/actions/inner")
	}
}

// TestLocalAction_CyclicLocals tests local action A → local action B → local action A cycle.
func TestLocalAction_CyclicLocals(t *testing.T) {
	workflowYAML := []byte(`
name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/action-a
`)

	localA := []byte(`
name: Action A
runs:
  using: composite
  steps:
    - uses: ./.github/actions/action-b
    - uses: alpha/external-a@v1
`)

	localB := []byte(`
name: Action B
runs:
  using: composite
  steps:
    - uses: ./.github/actions/action-a
    - uses: beta/external-b@v1
`)

	routes := map[string][]byte{
		"myorg/myrepo/.github/workflows":                    directoryJSON(t, []string{"ci.yml"}),
		"myorg/myrepo/.github/workflows/ci.yml":             workflowYAML,
		"myorg/myrepo/.github/actions/action-a/action.yml":  localA,
		"myorg/myrepo/.github/actions/action-b/action.yml":  localB,
	}

	srv := fakeGitHubAPI(t, routes)
	defer srv.Close()

	svc := newTestService(t, srv.URL)
	_, localActions, _, _, err := svc.DiscoverActions(
		context.Background(),
		[]string{"https://github.com/myorg/myrepo"},
		false,
	)
	if err != nil {
		t.Fatalf("DiscoverActions: %v", err)
	}

	if len(localActions) != 2 {
		t.Fatalf("expected 2 local actions, got %d: %v", len(localActions), localActions)
	}
	if via := localActions["https://github.com/alpha/external-a"]; via != ".github/actions/action-a" {
		t.Errorf("alpha/external-a via = %q, want %q", via, ".github/actions/action-a")
	}
	if via := localActions["https://github.com/beta/external-b"]; via != ".github/actions/action-b" {
		t.Errorf("beta/external-b via = %q, want %q", via, ".github/actions/action-b")
	}
}

// TestLocalAction_WithTransitive tests that external actions found via local actions
// are also fed into transitive BFS resolution.
func TestLocalAction_WithTransitive(t *testing.T) {
	workflowYAML := []byte(`
name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/setup
`)

	localSetup := []byte(`
name: Setup
runs:
  using: composite
  steps:
    - uses: alpha/action-a@v1
`)

	// alpha/action-a is a composite action that uses beta/action-b.
	compositeA := []byte(`
name: Action A
runs:
  using: composite
  steps:
    - uses: beta/action-b@v1
`)

	nodeB := []byte(`
name: Action B
runs:
  using: node20
  main: index.js
`)

	routes := map[string][]byte{
		"myorg/myrepo/.github/workflows":                directoryJSON(t, []string{"ci.yml"}),
		"myorg/myrepo/.github/workflows/ci.yml":         workflowYAML,
		"myorg/myrepo/.github/actions/setup/action.yml": localSetup,
		"alpha/action-a/action.yml":                      compositeA,
		"beta/action-b/action.yml":                       nodeB,
	}

	srv := fakeGitHubAPI(t, routes)
	defer srv.Close()

	svc := newTestService(t, srv.URL)
	directURLs, localActions, transitiveActions, _, err := svc.DiscoverActions(
		context.Background(),
		[]string{"https://github.com/myorg/myrepo"},
		true, // Enable transitive resolution
	)
	if err != nil {
		t.Fatalf("DiscoverActions: %v", err)
	}

	// No direct actions (workflow only uses local ./ action).
	if len(directURLs) != 0 {
		t.Errorf("expected 0 direct URLs, got %v", directURLs)
	}

	// alpha/action-a is local (found via .github/actions/setup).
	if len(localActions) != 1 {
		t.Fatalf("expected 1 local action, got %d: %v", len(localActions), localActions)
	}
	if via := localActions["https://github.com/alpha/action-a"]; via != ".github/actions/setup" {
		t.Errorf("alpha/action-a via = %q, want %q", via, ".github/actions/setup")
	}

	// beta/action-b should be transitive (found via alpha/action-a BFS).
	if len(transitiveActions) != 1 {
		t.Fatalf("expected 1 transitive action, got %d: %v", len(transitiveActions), transitiveActions)
	}
	via, ok := transitiveActions["https://github.com/beta/action-b"]
	if !ok {
		t.Fatal("expected beta/action-b in transitive actions")
	}
	if via != "https://github.com/alpha/action-a" {
		t.Errorf("via = %q, want %q", via, "https://github.com/alpha/action-a")
	}
}

// TestLocalAction_NonCompositeSkipped tests that non-composite local actions are handled gracefully.
func TestLocalAction_NonCompositeSkipped(t *testing.T) {
	workflowYAML := []byte(`
name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/node-action
`)

	nodeAction := []byte(`
name: Node Action
runs:
  using: node20
  main: index.js
`)

	routes := map[string][]byte{
		"myorg/myrepo/.github/workflows":                     directoryJSON(t, []string{"ci.yml"}),
		"myorg/myrepo/.github/workflows/ci.yml":              workflowYAML,
		"myorg/myrepo/.github/actions/node-action/action.yml": nodeAction,
	}

	srv := fakeGitHubAPI(t, routes)
	defer srv.Close()

	svc := newTestService(t, srv.URL)
	directURLs, _, _, _, err := svc.DiscoverActions(
		context.Background(),
		[]string{"https://github.com/myorg/myrepo"},
		false,
	)
	if err != nil {
		t.Fatalf("DiscoverActions: %v", err)
	}

	if len(directURLs) != 0 {
		t.Errorf("expected 0 direct URLs for non-composite local action, got %v", directURLs)
	}
}

// TestResolveTransitive_DiamondDependency tests A→B, A→C, B→D, C→D.
// D should appear once in transitive, not duplicated.
func TestResolveTransitive_DiamondDependency(t *testing.T) {
	workflowYAML := []byte(`
name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: alpha/action-a@v1
`)

	compositeA := []byte(`
name: Action A
runs:
  using: composite
  steps:
    - uses: beta/action-b@v1
    - uses: gamma/action-c@v1
`)

	compositeB := []byte(`
name: Action B
runs:
  using: composite
  steps:
    - uses: delta/action-d@v1
`)

	compositeC := []byte(`
name: Action C
runs:
  using: composite
  steps:
    - uses: delta/action-d@v1
`)

	nodeD := []byte(`
name: Action D
runs:
  using: node20
  main: index.js
`)

	routes := map[string][]byte{
		"myorg/myrepo/.github/workflows":        directoryJSON(t, []string{"ci.yml"}),
		"myorg/myrepo/.github/workflows/ci.yml": workflowYAML,
		"alpha/action-a/action.yml":              compositeA,
		"beta/action-b/action.yml":               compositeB,
		"gamma/action-c/action.yml":              compositeC,
		"delta/action-d/action.yml":              nodeD,
	}

	srv := fakeGitHubAPI(t, routes)
	defer srv.Close()

	svc := newTestService(t, srv.URL)
	_, _, transitiveActions, _, err := svc.DiscoverActions(
		context.Background(),
		[]string{"https://github.com/myorg/myrepo"},
		true,
	)
	if err != nil {
		t.Fatalf("DiscoverActions: %v", err)
	}

	// B, C, D should all be transitive.
	if len(transitiveActions) != 3 {
		t.Fatalf("expected 3 transitive actions, got %d: %v", len(transitiveActions), transitiveActions)
	}

	// All should have via = alpha/action-a (the root direct action).
	for url, via := range transitiveActions {
		if via != "https://github.com/alpha/action-a" {
			t.Errorf("transitive %q via = %q, want alpha/action-a", url, via)
		}
	}

	// D should appear exactly once (dedup check).
	count := 0
	for url := range transitiveActions {
		if url == "https://github.com/delta/action-d" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected delta/action-d once, found %d times", count)
	}
}
