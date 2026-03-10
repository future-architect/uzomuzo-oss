package golangresolve

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo/internal/common/purl"
	"github.com/future-architect/uzomuzo/internal/infrastructure/goproxy"
)

// githubRawBase is the base raw content host for GitHub. Overridden in tests.
var githubRawBase = "https://raw.githubusercontent.com"

// NormalizePathToModuleRoot resolves a raw Go import path (which may point to a subpackage)
// to the module root using the Go proxy. It returns the discovered module path
// (raw, not URL-escaped), the latest version observed at the proxy (may be empty),
// and ok=false if resolution failed.
//
// DDD Layer: Infrastructure (depends on goproxy implementation)
func NormalizePathToModuleRoot(ctx context.Context, gp *goproxy.Client, rawPath string) (module string, latest string, ok bool) {
	if gp == nil {
		return "", "", false
	}
	p := strings.TrimSpace(rawPath)
	if p == "" {
		return "", "", false
	}
	if m, l, err := gp.ResolveModuleRoot(ctx, p); err == nil && m != "" {
		return m, l, true
	}
	return "", "", false
}

// NormalizePURLToModuleRoot resolves a parsed golang PURL to its module root.
// It returns the raw module path and a URL-escaped module name suitable for deps.dev
// systems/{system}/packages/{name} endpoint. ok=false for non-golang PURLs or failure.
//
// Example: pkg:golang/github.com/org/repo/sub/pkg@v1.2.3 → ("github.com/org/repo", "github.com%2Forg%2Frepo", true)
// NormalizePURLToModuleRoot resolves a parsed golang PURL to its module root.
// defaultBranch is the repository's known default branch (if already fetched via GitHub GraphQL).
// When provided, it is tried first when falling back to fetching go.mod from the raw content host.
// Empty defaultBranch preserves legacy behavior (tries main, then master).
func NormalizePURLToModuleRoot(ctx context.Context, gp *goproxy.Client, pr *purl.ParsedPURL, defaultBranch string) (rawModule string, escapedName string, ok bool) {
	if pr == nil || !strings.EqualFold(pr.GetEcosystem(), "golang") {
		return "", "", false
	}
	// Use the full import path (namespace + name) and unescape before passing to resolvers.
	rawFull := strings.TrimSpace(pr.GetPackageName())
	if un, err := url.PathUnescape(rawFull); err == nil && un != "" {
		rawFull = un
	}
	// 1) Authoritative via Go proxy
	if mod, _, okp := NormalizePathToModuleRoot(ctx, gp, rawFull); okp && mod != "" {
		return mod, url.PathEscape(mod), true
	}
	// 2) GitHub Raw fallback: read go.mod module directive when path is a GitHub repo/subpath
	if strings.HasPrefix(rawFull, "github.com/") {
		if mod, okgh := inferModuleFromGitHubRaw(ctx, rawFull, defaultBranch); okgh && mod != "" {
			return mod, url.PathEscape(mod), true
		}
	}
	// 3) Not normalized; return original escaped with ok=false so callers can apply coarse fallback
	return rawFull, url.PathEscape(rawFull), false
}

// inferModuleFromGitHubRaw tries to fetch go.mod from GitHub default branches and parse module directive.
// rawPath must start with github.com/<owner>/<repo> (subpaths allowed). Returns (module, true) if found.
func inferModuleFromGitHubRaw(ctx context.Context, rawPath, defaultBranch string) (string, bool) {
	s := strings.Trim(strings.TrimSpace(rawPath), "/")
	if !strings.HasPrefix(s, "github.com/") {
		return "", false
	}
	rest := strings.TrimPrefix(s, "github.com/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		return "", false
	}
	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	branches := buildBranchCandidates(defaultBranch)
	httpc := &http.Client{Timeout: 8 * time.Second}
	for _, br := range branches {
		var foundMod string
		rawURL := githubRawBase + "/" + owner + "/" + repo + "/" + br + "/go.mod"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "uzomuzo-golangresolve/1.0 (+https://github.com/future-architect/uzomuzo)")
		resp, err := httpc.Do(req)
		if err != nil {
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			b, _ := io.ReadAll(resp.Body)
			if m := parseModuleDirective(string(b)); m != "" {
				// Found authoritative module path
				foundMod = m
			}
		}()
		if foundMod != "" {
			return foundMod, true
		}
	}
	return "", false
}

// buildBranchCandidates builds an ordered list of branch names to query for go.mod.
// Order: provided defaultBranch (if non-empty) first, then "main", then "master", with
// duplicates removed case-insensitively while preserving first occurrence casing.
func buildBranchCandidates(defaultBranch string) []string {
	base := []string{"main", "master"}
	if strings.TrimSpace(defaultBranch) != "" {
		base = append([]string{defaultBranch}, base...)
	}
	seen := make(map[string]struct{}, len(base))
	out := make([]string, 0, len(base))
	for _, b := range base {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		lb := strings.ToLower(b)
		if _, ok := seen[lb]; ok {
			continue
		}
		seen[lb] = struct{}{}
		out = append(out, b)
	}
	return out
}

// parseModuleDirective extracts the module path from a go.mod file content.
func parseModuleDirective(goModContents string) string {
	for _, line := range strings.Split(goModContents, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "//") {
			continue
		}
		if strings.HasPrefix(l, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(l, "module "))
		}
	}
	return ""
}
