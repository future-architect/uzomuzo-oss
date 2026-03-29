package depsdev

import (
	"context"
	"net/url"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

// fakeGoproxyClient implements only ResolveModuleRoot used in normalizeGoModuleForVersions.
// Note: We do not test the 'proxy' strategy here because normalizeGoModuleForVersions delegates
// to golangresolve.NormalizePURLToModuleRoot which requires a real *goproxy.Client for integration
// behavior. Unit tests focus on fallback & fallback-no-proxy logic and structural guarantees.

func TestNormalizeGoModuleForVersions(t *testing.T) {
	parser := purl.NewParser()
	mustParse := func(s string) *purl.ParsedPURL {
		p, err := parser.Parse(s)
		if err != nil {
			t.Fatalf("parse failed: %v", err)
		}
		return p
	}

	ctx := context.Background()

	cases := []struct {
		name      string
		purlStr   string
		wantStrat string
		wantRoot  string
		wantEsc   bool // expect EscapedName non-empty
	}{
		{
			name:      "no proxy fallback-no-proxy keeps full path (no trimming) when pattern not matched",
			purlStr:   "pkg:golang/github.com/acme/project/v2/internal/x@v2.0.0",
			wantStrat: "fallback-no-proxy",
			wantRoot:  "github.com/acme/project/v2/internal/x",
			wantEsc:   true,
		},
		{
			name:      "no proxy fallback-no-proxy",
			purlStr:   "pkg:golang/github.com/acme/lib@v0.1.0",
			wantStrat: "fallback-no-proxy",
			wantRoot:  "github.com/acme/lib",
			wantEsc:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pr := mustParse(tc.purlStr)
			// adapt fake to *goproxy.Client? we only need ResolveModuleRoot; interface satisfied
			// but normalizeGoModuleForVersions expects *goproxy.Client; use type conversion via embedding not possible here.
			// Instead we pass nil for proxy-driven cases except those where we want success; for proxy success create a wrapper? Simplify: when proxy != nil and proxy.module != "", treat as success by manual override.
			norm := normalizeGoModuleForVersions(ctx, nil, pr)
			if norm.Strategy != tc.wantStrat {
				t.Fatalf("strategy: got %s want %s", norm.Strategy, tc.wantStrat)
			}
			// ModuleRootRaw should be the raw (unescaped) module path. If it looks escaped attempt unescape for comparison.
			gotRoot := norm.ModuleRootRaw
			if u, err := url.PathUnescape(gotRoot); err == nil {
				gotRoot = u
			}
			if gotRoot != tc.wantRoot {
				t.Fatalf("module root: got %s want %s", norm.ModuleRootRaw, tc.wantRoot)
			}
			if tc.wantEsc && norm.EscapedName == "" {
				t.Fatalf("expected escaped name, got empty")
			}
			if !tc.wantEsc && norm.EscapedName != "" {
				t.Fatalf("expected empty escaped name, got %s", norm.EscapedName)
			}
		})
	}
}

func TestReconstructGoVersionPURL(t *testing.T) {
	base := "pkg:golang/github.com/acme/project/sub@v1.2.3"
	newP, ok := reconstructGoVersionPURL(base, "github.com/acme/project", "v1.3.0")
	if !ok || newP == "" {
		t.Fatalf("expected success reconstructing, got ok=%v p=%q", ok, newP)
	}
}
