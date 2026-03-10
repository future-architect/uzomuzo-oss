package depsdev

import (
	"context"
	"errors"
	"testing"
)

// testResolver is a lightweight test double for moduleRootResolver.
type testResolver struct {
	fn func(ctx context.Context, path string) (string, string, error)
}

func (r testResolver) ResolveModuleRoot(ctx context.Context, path string) (string, string, error) {
	return r.fn(ctx, path)
}

func TestSynthesizeGoGitHubRepoURL(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name       string
		importPath string
		resolver   moduleRootResolver
		want       string
	}{
		{
			name:       "empty input",
			importPath: "",
			resolver:   nil,
			want:       "",
		},
		{
			name:       "whitespace only",
			importPath: "   \t  ",
			resolver:   nil,
			want:       "",
		},
		{
			name:       "direct github path without resolver",
			importPath: "github.com/owner/repo/sub/pkg",
			resolver:   nil,
			want:       "https://github.com/owner/repo",
		},
		{
			name:       "github path with major version suffix",
			importPath: "github.com/owner/repo/v2/feature/x",
			resolver:   nil,
			want:       "https://github.com/owner/repo",
		},
		{
			name:       "resolver returns github module root (preferred over coarse path)",
			importPath: "github.com/owner/repo/v2/internal/thing",
			resolver: testResolver{fn: func(ctx context.Context, path string) (string, string, error) {
				// Simulate proxy resolving canonical module root WITH major version.
				return "github.com/owner/repo/v2", "v2.1.0", nil
			}},
			want: "https://github.com/owner/repo",
		},
		{
			name:       "resolver returns non-github module root, no coarse fallback",
			importPath: "golang.org/x/tools/go/packages",
			resolver: testResolver{fn: func(ctx context.Context, path string) (string, string, error) {
				return "golang.org/x/tools", "v0.21.0", nil
			}},
			want: "", // only GitHub repositories are supported currently
		},
		{
			name:       "resolver error then coarse github fallback",
			importPath: "github.com/errowner/errrepo/sub/path",
			resolver: testResolver{fn: func(ctx context.Context, path string) (string, string, error) {
				return "", "", errors.New("network failure")
			}},
			want: "https://github.com/errowner/errrepo",
		},
		{
			name:       "trailing .git removed by coarse fallback",
			importPath: "github.com/owner/repo.git/sub",
			resolver:   nil,
			want:       "https://github.com/owner/repo",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := synthesizeGoGitHubRepoURL(ctx, tc.resolver, tc.importPath)
			if got != tc.want {
				t.Fatalf("unexpected repo URL: got %q want %q", got, tc.want)
			}
		})
	}
}
