package ghaworkflow_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/ghaworkflow"
)

func TestParseGitHubURLs(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		wantURLs []string
	}{
		{
			name: "standard CI workflow",
			file: "ci.yml",
			wantURLs: []string{
				"https://github.com/actions/checkout",
				"https://github.com/actions/setup-go",
				"https://github.com/golangci/golangci-lint-action",
				"https://github.com/github/codeql-action",
			},
		},
		{
			name:     "docker-only references are skipped",
			file:     "docker-only.yml",
			wantURLs: nil,
		},
		{
			name:     "empty jobs",
			file:     "empty.yml",
			wantURLs: nil,
		},
		{
			name: "reusable workflows and local actions",
			file: "reusable.yml",
			wantURLs: []string{
				"https://github.com/owner/shared-workflows",
				"https://github.com/actions/checkout",
			},
		},
		{
			name: "mixed references",
			file: "mixed.yml",
			wantURLs: []string{
				"https://github.com/actions/checkout",
				"https://github.com/actions/setup-node",
				"https://github.com/owner/repo",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tt.file))
			if err != nil {
				t.Fatalf("failed to read testdata: %v", err)
			}

			got, err := ghaworkflow.ParseGitHubURLs(data)
			if err != nil {
				t.Fatalf("ParseGitHubURLs() error = %v", err)
			}

			assertURLsEqual(t, got, tt.wantURLs)
		})
	}
}

func TestParseGitHubURLs_InvalidYAML(t *testing.T) {
	_, err := ghaworkflow.ParseGitHubURLs([]byte("{{invalid yaml"))
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestExtractGitHubURL_EdgeCases(t *testing.T) {
	// Test via ParseGitHubURLs with inline YAML to cover edge cases.
	tests := []struct {
		name     string
		yaml     string
		wantURLs []string
	}{
		{
			name:     "SHA-pinned action",
			yaml:     "on: push\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd",
			wantURLs: []string{"https://github.com/actions/checkout"},
		},
		{
			name:     "action with deep subpath",
			yaml:     "on: push\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: owner/repo/deep/nested/path@v1",
			wantURLs: []string{"https://github.com/owner/repo"},
		},
		{
			name:     "whitespace in uses value",
			yaml:     "on: push\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: \"  actions/checkout@v4  \"",
			wantURLs: []string{"https://github.com/actions/checkout"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ghaworkflow.ParseGitHubURLs([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("ParseGitHubURLs() error = %v", err)
			}
			assertURLsEqual(t, got, tt.wantURLs)
		})
	}
}

func TestIsWorkflowYAML(t *testing.T) {
	ciData, err := os.ReadFile(filepath.Join("testdata", "ci.yml"))
	if err != nil {
		t.Fatalf("failed to read testdata: %v", err)
	}
	prefix := ciData
	if len(prefix) > 1024 {
		prefix = prefix[:1024]
	}

	tests := []struct {
		name     string
		filePath string
		prefix   []byte
		want     bool
	}{
		{
			name:     "path-based detection",
			filePath: "/repo/.github/workflows/ci.yml",
			prefix:   nil,
			want:     true,
		},
		{
			name:     "path-based with yaml extension",
			filePath: "/repo/.github/workflows/deploy.yaml",
			prefix:   nil,
			want:     true,
		},
		{
			name:     "content-based detection",
			filePath: "/tmp/workflow.yml",
			prefix:   prefix,
			want:     true,
		},
		{
			name:     "non-yaml extension",
			filePath: "/repo/.github/workflows/ci.json",
			prefix:   prefix,
			want:     false,
		},
		{
			name:     "yaml without workflow markers",
			filePath: "/tmp/config.yml",
			prefix:   []byte("apiVersion: v1\nkind: Service\n"),
			want:     false,
		},
		{
			name:     "non-workflow yaml",
			filePath: "/tmp/docker-compose.yml",
			prefix:   []byte("version: '3'\nservices:\n  web:\n    image: nginx\n"),
			want:     false,
		},
		{
			name:     "quoted on key",
			filePath: "/tmp/workflow.yml",
			prefix:   []byte("\"on\":\n  push:\njobs:\n  build:\n"),
			want:     true,
		},
		{
			name:     "false positive path without segment boundary",
			filePath: "/tmp/not.github/workflows/foo.yml",
			prefix:   nil,
			want:     false,
		},
		{
			name:     "relative github workflows path",
			filePath: ".github/workflows/ci.yml",
			prefix:   nil,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ghaworkflow.IsWorkflowYAML(tt.filePath, tt.prefix)
			if got != tt.want {
				t.Errorf("IsWorkflowYAML(%q) = %v, want %v", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestExtractActionRef(t *testing.T) {
	tests := []struct {
		name    string
		uses    string
		wantRef ghaworkflow.ActionRef
		wantOK  bool
	}{
		{
			name:    "standard action",
			uses:    "actions/checkout@v4",
			wantRef: ghaworkflow.ActionRef{Owner: "actions", Repo: "checkout", Ref: "v4"},
			wantOK:  true,
		},
		{
			name:    "subdirectory action",
			uses:    "actions/cache/restore@v4",
			wantRef: ghaworkflow.ActionRef{Owner: "actions", Repo: "cache", Path: "restore", Ref: "v4"},
			wantOK:  true,
		},
		{
			name:    "SHA-pinned",
			uses:    "actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd",
			wantRef: ghaworkflow.ActionRef{Owner: "actions", Repo: "checkout", Ref: "de0fac2e4500dabe0009e67214ff5f5447ce83dd"},
			wantOK:  true,
		},
		{
			name:    "deep subpath",
			uses:    "owner/repo/deep/nested/path@v1",
			wantRef: ghaworkflow.ActionRef{Owner: "owner", Repo: "repo", Path: "deep/nested/path", Ref: "v1"},
			wantOK:  true,
		},
		{
			name:   "local action",
			uses:   "./local-action",
			wantOK: false,
		},
		{
			name:   "docker reference",
			uses:   "docker://alpine:3.18",
			wantOK: false,
		},
		{
			name:   "empty string",
			uses:   "",
			wantOK: false,
		},
		{
			name:   "whitespace only",
			uses:   "   ",
			wantOK: false,
		},
		{
			name:    "no ref suffix",
			uses:    "owner/repo",
			wantRef: ghaworkflow.ActionRef{Owner: "owner", Repo: "repo"},
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ghaworkflow.ExtractActionRef(tt.uses)
			if ok != tt.wantOK {
				t.Fatalf("ExtractActionRef(%q) ok = %v, want %v", tt.uses, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got != tt.wantRef {
				t.Errorf("ExtractActionRef(%q) = %+v, want %+v", tt.uses, got, tt.wantRef)
			}
		})
	}
}

func TestParseCompositeActionURLs(t *testing.T) {
	tests := []struct {
		name          string
		file          string
		wantComposite bool
		wantRefs      []ghaworkflow.ActionRef
	}{
		{
			name:          "composite action with uses",
			file:          "composite-action.yml",
			wantComposite: true,
			wantRefs: []ghaworkflow.ActionRef{
				{Owner: "actions", Repo: "checkout", Ref: "v4"},
				{Owner: "actions", Repo: "cache", Path: "restore", Ref: "v4"},
				{Owner: "owner", Repo: "some-action", Ref: "abc123"},
			},
		},
		{
			name:          "node action (not composite)",
			file:          "node-action.yml",
			wantComposite: false,
			wantRefs:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tt.file))
			if err != nil {
				t.Fatalf("failed to read testdata: %v", err)
			}

			refs, isComposite, err := ghaworkflow.ParseCompositeActionURLs(data)
			if err != nil {
				t.Fatalf("ParseCompositeActionURLs() error = %v", err)
			}
			if isComposite != tt.wantComposite {
				t.Errorf("isComposite = %v, want %v", isComposite, tt.wantComposite)
			}
			if len(refs) != len(tt.wantRefs) {
				t.Fatalf("got %d refs, want %d\ngot:  %+v\nwant: %+v", len(refs), len(tt.wantRefs), refs, tt.wantRefs)
			}
			for i, got := range refs {
				if got != tt.wantRefs[i] {
					t.Errorf("refs[%d] = %+v, want %+v", i, got, tt.wantRefs[i])
				}
			}
		})
	}
}

func TestParseCompositeActionURLs_InvalidYAML(t *testing.T) {
	_, _, err := ghaworkflow.ParseCompositeActionURLs([]byte("{{invalid yaml"))
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestActionRef_GitHubURL(t *testing.T) {
	ref := ghaworkflow.ActionRef{Owner: "actions", Repo: "checkout"}
	if got := ref.GitHubURL(); got != "https://github.com/actions/checkout" {
		t.Errorf("GitHubURL() = %q, want %q", got, "https://github.com/actions/checkout")
	}
}

func TestActionRef_ActionYAMLPath(t *testing.T) {
	tests := []struct {
		name     string
		ref      ghaworkflow.ActionRef
		filename string
		want     string
	}{
		{
			name:     "root action",
			ref:      ghaworkflow.ActionRef{Owner: "actions", Repo: "checkout"},
			filename: "action.yml",
			want:     "action.yml",
		},
		{
			name:     "subdirectory action",
			ref:      ghaworkflow.ActionRef{Owner: "actions", Repo: "cache", Path: "restore"},
			filename: "action.yml",
			want:     "restore/action.yml",
		},
		{
			name:     "yaml fallback",
			ref:      ghaworkflow.ActionRef{Owner: "actions", Repo: "cache", Path: "save"},
			filename: "action.yaml",
			want:     "save/action.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ref.ActionYAMLPath(tt.filename); got != tt.want {
				t.Errorf("ActionYAMLPath(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestExtractLocalActionPath(t *testing.T) {
	tests := []struct {
		name string
		uses string
		want string
	}{
		{name: "standard local action", uses: "./.github/actions/foo", want: ".github/actions/foo"},
		{name: "local action with trailing slash", uses: "./.github/actions/bar/", want: ".github/actions/bar"},
		{name: "deeply nested local", uses: "./actions/setup/build", want: "actions/setup/build"},
		{name: "external action", uses: "actions/checkout@v4", want: ""},
		{name: "docker reference", uses: "docker://alpine:3.18", want: ""},
		{name: "empty string", uses: "", want: ""},
		{name: "whitespace only", uses: "   ", want: ""},
		{name: "dot-slash only", uses: "./", want: ""},
		{name: "whitespace around local", uses: "  ./.github/actions/foo  ", want: ".github/actions/foo"},
		{name: "parent relative", uses: "../some-action", want: ""},
		{name: "traversal via dot-dot prefix", uses: "./../some-action", want: ""},
		{name: "embedded dot-dot normalized within repo", uses: "./.github/actions/../secrets", want: ".github/secrets"},
		{name: "traversal resolved to parent", uses: "./foo/../../../etc", want: ""},
		{name: "clean normalizes redundant slashes", uses: "./.github/actions//foo", want: ".github/actions/foo"},
		{name: "backslash rejected", uses: ".\\.github\\actions\\foo", want: ""},
		{name: "dot-dot in middle resolved safely", uses: "./foo/../bar", want: "bar"},
		{name: "extra slashes produce absolute path rejected", uses: ".///.github/actions/foo", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ghaworkflow.ExtractLocalActionPath(tt.uses)
			if got != tt.want {
				t.Errorf("ExtractLocalActionPath(%q) = %q, want %q", tt.uses, got, tt.want)
			}
		})
	}
}

func TestParseLocalActionPaths(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantPaths []string
	}{
		{
			name: "workflow with local actions",
			yaml: `
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: ./.github/actions/build-frontend
      - uses: ./.github/actions/deploy
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/build-frontend
      - uses: ./.github/actions/test-e2e
`,
			wantPaths: []string{
				".github/actions/build-frontend",
				".github/actions/deploy",
				".github/actions/test-e2e",
			},
		},
		{
			name: "workflow with no local actions",
			yaml: `
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
`,
			wantPaths: nil,
		},
		{
			name: "workflow with only local actions",
			yaml: `
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/foo
`,
			wantPaths: []string{".github/actions/foo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ghaworkflow.ParseLocalActionPaths([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("ParseLocalActionPaths() error = %v", err)
			}
			if len(got) != len(tt.wantPaths) {
				t.Fatalf("got %d paths, want %d\ngot:  %v\nwant: %v", len(got), len(tt.wantPaths), got, tt.wantPaths)
			}
			// Sort for comparison since job iteration is sorted but cross-job dedup order may vary.
			sortedGot := make([]string, len(got))
			copy(sortedGot, got)
			sort.Strings(sortedGot)
			sortedWant := make([]string, len(tt.wantPaths))
			copy(sortedWant, tt.wantPaths)
			sort.Strings(sortedWant)
			for i := range sortedGot {
				if sortedGot[i] != sortedWant[i] {
					t.Errorf("path mismatch:\ngot:  %v\nwant: %v", sortedGot, sortedWant)
					return
				}
			}
		})
	}
}

func TestParseWorkflowAll(t *testing.T) {
	tests := []struct {
		name           string
		yaml           string
		wantURLs       []string
		wantLocalPaths []string
	}{
		{
			name: "mixed workflow with external and local actions",
			yaml: `
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: ./.github/actions/build-frontend
      - uses: actions/setup-node@v4
      - uses: ./.github/actions/deploy
`,
			wantURLs: []string{
				"https://github.com/actions/checkout",
				"https://github.com/actions/setup-node",
			},
			wantLocalPaths: []string{
				".github/actions/build-frontend",
				".github/actions/deploy",
			},
		},
		{
			name: "no local actions",
			yaml: `
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
`,
			wantURLs:       []string{"https://github.com/actions/checkout"},
			wantLocalPaths: nil,
		},
		{
			name: "only local actions",
			yaml: `
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/foo
`,
			wantURLs:       nil,
			wantLocalPaths: []string{".github/actions/foo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urls, locals, err := ghaworkflow.ParseWorkflowAll([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("ParseWorkflowAll() error = %v", err)
			}
			assertURLsEqual(t, urls, tt.wantURLs)
			if len(locals) != len(tt.wantLocalPaths) {
				t.Fatalf("got %d local paths, want %d\ngot:  %v\nwant: %v",
					len(locals), len(tt.wantLocalPaths), locals, tt.wantLocalPaths)
			}
			sortedGot := make([]string, len(locals))
			copy(sortedGot, locals)
			sort.Strings(sortedGot)
			sortedWant := make([]string, len(tt.wantLocalPaths))
			copy(sortedWant, tt.wantLocalPaths)
			sort.Strings(sortedWant)
			for i := range sortedGot {
				if sortedGot[i] != sortedWant[i] {
					t.Errorf("local path mismatch:\ngot:  %v\nwant: %v", sortedGot, sortedWant)
					return
				}
			}
		})
	}
}

func TestParseWorkflowAllWithRefs(t *testing.T) {
	type refExpect struct {
		owner string
		repo  string
		path  string
		ref   string
	}
	tests := []struct {
		name     string
		file     string
		wantRefs []refExpect
	}{
		{
			name: "deprecated pins fixture captures distinct refs per action",
			file: "deprecated-pins.yml",
			wantRefs: []refExpect{
				{owner: "actions", repo: "checkout", ref: "v2"},
				{owner: "actions", repo: "setup-node", ref: "v2"},
				{owner: "actions", repo: "upload-artifact", ref: "v3"},
				{owner: "actions", repo: "checkout", ref: "v4"},
				{owner: "actions", repo: "setup-python", ref: "v5"},
				{owner: "actions", repo: "cache", ref: "v3"},
				{owner: "actions", repo: "cache", ref: "v4"},
				{owner: "actions", repo: "checkout", ref: "main"},
				{owner: "actions", repo: "checkout", ref: "de0fac2e4500dabe0009e67214ff5f5447ce83dd"},
			},
		},
		{
			name: "standard CI workflow (ref preserved)",
			file: "ci.yml",
			wantRefs: []refExpect{
				{owner: "actions", repo: "checkout", ref: "v4"},
				{owner: "actions", repo: "setup-go", ref: "v5"},
				{owner: "golangci", repo: "golangci-lint-action", ref: "v6"},
				{owner: "actions", repo: "checkout", ref: "de0fac2e4500dabe0009e67214ff5f5447ce83dd"},
				{owner: "github", repo: "codeql-action", path: "init", ref: "v4"},
				{owner: "github", repo: "codeql-action", path: "analyze", ref: "v4"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tt.file))
			if err != nil {
				t.Fatalf("failed to read testdata: %v", err)
			}

			refs, _, err := ghaworkflow.ParseWorkflowAllWithRefs(data)
			if err != nil {
				t.Fatalf("ParseWorkflowAllWithRefs() error = %v", err)
			}

			if len(refs) != len(tt.wantRefs) {
				t.Fatalf("got %d refs, want %d\ngot:  %+v\nwant: %+v", len(refs), len(tt.wantRefs), refs, tt.wantRefs)
			}

			seen := make(map[refExpect]bool, len(tt.wantRefs))
			for _, r := range refs {
				key := refExpect{owner: r.Owner, repo: r.Repo, path: r.Path, ref: r.Ref}
				seen[key] = true
			}
			for _, want := range tt.wantRefs {
				if !seen[want] {
					t.Errorf("missing ref %+v in %+v", want, refs)
				}
			}
		})
	}
}

func TestParseWorkflowAllWithRefs_MultiPinSameAction(t *testing.T) {
	yaml := `
on: push
jobs:
  a:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - uses: actions/checkout@v4
  b:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
`
	refs, _, err := ghaworkflow.ParseWorkflowAllWithRefs([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseWorkflowAllWithRefs() error = %v", err)
	}
	// job b duplicates v2 — should be deduplicated. v2 and v4 remain as distinct refs.
	if len(refs) != 2 {
		t.Fatalf("want 2 distinct refs (v2,v4), got %d: %+v", len(refs), refs)
	}
	gotRefs := map[string]bool{refs[0].Ref: true, refs[1].Ref: true}
	if !gotRefs["v2"] || !gotRefs["v4"] {
		t.Errorf("expected both v2 and v4, got %+v", refs)
	}
}

func TestParseCompositeAll_DistinctRefsForSameAction(t *testing.T) {
	// A composite action that pins the same upstream action at two versions
	// must surface both refs so pinned-version deprecation detection sees
	// both. The dedup key therefore includes @ref.
	yaml := `
name: Build
runs:
  using: composite
  steps:
    - uses: actions/checkout@v2
    - uses: actions/checkout@v4
`
	refs, _, isComposite, err := ghaworkflow.ParseCompositeAll([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseCompositeAll() error = %v", err)
	}
	if !isComposite {
		t.Fatal("expected composite action")
	}
	if len(refs) != 2 {
		t.Fatalf("want 2 distinct refs (v2,v4), got %d: %+v", len(refs), refs)
	}
	got := map[string]bool{refs[0].Ref: true, refs[1].Ref: true}
	if !got["v2"] || !got["v4"] {
		t.Errorf("expected both v2 and v4, got %+v", refs)
	}
}

func TestParseCompositeLocalActionPaths(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		wantComposite bool
		wantPaths     []string
	}{
		{
			name: "composite with local and external refs",
			yaml: `
name: Build
runs:
  using: composite
  steps:
    - uses: actions/checkout@v4
    - uses: ./.github/actions/setup-tools
    - uses: ./.github/actions/lint
`,
			wantComposite: true,
			wantPaths:     []string{".github/actions/setup-tools", ".github/actions/lint"},
		},
		{
			name: "composite with no local refs",
			yaml: `
name: Build
runs:
  using: composite
  steps:
    - uses: actions/checkout@v4
`,
			wantComposite: true,
			wantPaths:     nil,
		},
		{
			name: "node action (not composite)",
			yaml: `
name: Node Action
runs:
  using: node20
  main: index.js
`,
			wantComposite: false,
			wantPaths:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, isComposite, err := ghaworkflow.ParseCompositeLocalActionPaths([]byte(tt.yaml))
			if err != nil {
				t.Fatalf("ParseCompositeLocalActionPaths() error = %v", err)
			}
			if isComposite != tt.wantComposite {
				t.Errorf("isComposite = %v, want %v", isComposite, tt.wantComposite)
			}
			if len(paths) != len(tt.wantPaths) {
				t.Fatalf("got %d paths, want %d\ngot:  %v\nwant: %v", len(paths), len(tt.wantPaths), paths, tt.wantPaths)
			}
			for i, got := range paths {
				if got != tt.wantPaths[i] {
					t.Errorf("paths[%d] = %q, want %q", i, got, tt.wantPaths[i])
				}
			}
		})
	}
}

// assertURLsEqual compares two URL slices as unordered sets because URL order is not part of the parser contract.
func assertURLsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d URLs, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	sortedGot := make([]string, len(got))
	copy(sortedGot, got)
	sort.Strings(sortedGot)

	sortedWant := make([]string, len(want))
	copy(sortedWant, want)
	sort.Strings(sortedWant)

	for i := range sortedGot {
		if sortedGot[i] != sortedWant[i] {
			t.Errorf("URL mismatch:\ngot:  %v\nwant: %v", sortedGot, sortedWant)
			return
		}
	}
}
