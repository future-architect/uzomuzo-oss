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

// assertURLsEqual compares two URL slices as unordered sets (YAML map iteration is non-deterministic).
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
