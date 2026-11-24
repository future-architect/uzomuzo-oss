package ghaworkflow_test

import (
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/ghaworkflow"
)

func FuzzExtractLocalActionPath(f *testing.F) {
	for _, uses := range []string{
		"./a", "$/a", "$/a/", "$/actions/@scope/foo", "$/a@v1", "$/a?b",
		"$/a#b", "$/a%2e", "$/../x", "./../x", "./foo/../bar", "$/",
		"$/a\\b", " $/a ", "docker://x", "owner/repo@v1", "",
	} {
		f.Add(uses)
	}
	f.Fuzz(func(t *testing.T, uses string) {
		p := ghaworkflow.ExtractLocalActionPath(uses)
		if p == "" {
			return
		}
		assertLocalActionPath(t, p)
		if strings.HasPrefix(strings.TrimSpace(uses), "$/") && strings.ContainsAny(p, "@?#%") {
			t.Fatalf("same-repo uses %q returned path with reserved character %q", uses, p)
		}
	})
}

func FuzzParseCompositeSteps(f *testing.F) {
	f.Add([]byte("runs:\n  using: composite\n  steps:\n    - uses: $/sub\n    - uses: $/bad@v1\n    - uses: ./local\n    - uses: other/y@v1\n    - uses: $/../secret\n"))
	f.Add([]byte("runs:\n  using: composite\n  steps:\n    - uses: other/y@v1\n    - uses: other/y@v2\n    - uses: other/y/sub@v1\n    - uses: $/sub\n    - uses: $/sub/\n    - uses: $/bad@v1\n    - uses: $/bad@v1\n"))
	f.Add([]byte("runs:\n  using: node20\n"))
	f.Add([]byte(""))
	f.Add([]byte("{{invalid yaml"))
	f.Fuzz(func(t *testing.T, data []byte) {
		steps, isComposite, err := ghaworkflow.ParseCompositeSteps(data)
		if err != nil && isComposite {
			t.Fatalf("parse error %v returned isComposite=true", err)
		}
		if !isComposite {
			if !reflect.DeepEqual(steps, ghaworkflow.CompositeSteps{}) {
				t.Fatalf("non-composite result has steps: %+v", steps)
			}
			return
		}

		seenPaths := make(map[string]struct{}, len(steps.SameRepoPaths))
		for _, p := range steps.SameRepoPaths {
			assertLocalActionPath(t, p)
			if strings.ContainsAny(p, "@?#%") {
				t.Fatalf("same-repo path contains reserved character: %q", p)
			}
			if _, exists := seenPaths[p]; exists {
				t.Fatalf("duplicate same-repo path: %q", p)
			}
			seenPaths[p] = struct{}{}
		}

		seenInvalid := make(map[string]struct{}, len(steps.InvalidSameRepoUses))
		for _, uses := range steps.InvalidSameRepoUses {
			if !strings.HasPrefix(strings.TrimSpace(uses), "$/") {
				t.Fatalf("invalid same-repo uses has wrong prefix: %q", uses)
			}
			if _, exists := seenInvalid[uses]; exists {
				t.Fatalf("duplicate invalid same-repo uses: %q", uses)
			}
			seenInvalid[uses] = struct{}{}
		}

		seenRefs := make(map[string]struct{}, len(steps.Refs))
		for _, ref := range steps.Refs {
			if ref.Owner == "" || ref.Repo == "" {
				t.Fatalf("action ref has empty owner or repo: %+v", ref)
			}
			key := ref.GitHubURL()
			if ref.Path != "" {
				key += "/" + ref.Path
			}
			if _, exists := seenRefs[key]; exists {
				t.Fatalf("duplicate action ref: %q", key)
			}
			seenRefs[key] = struct{}{}
		}
	})
}

func assertLocalActionPath(t *testing.T, p string) {
	t.Helper()
	if p == "" || path.Clean(p) != p || p == "." || p == ".." ||
		strings.HasPrefix(p, "../") || path.IsAbs(p) || strings.ContainsRune(p, '\\') {
		t.Fatalf("invalid local action path: %q", p)
	}
}
