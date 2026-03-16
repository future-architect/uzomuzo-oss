package depsdev

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	packageurl "github.com/package-url/packageurl-go"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/golangresolve"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/goproxy"
)

// moduleRootResolver provides the minimal capability needed to resolve a Go import path
// to its module root. It is intentionally small to allow simple fakes in tests.
// Implemented by *goproxy.Client.
type moduleRootResolver interface {
	ResolveModuleRoot(ctx context.Context, path string) (module string, latest string, err error)
}

// synthesizeGoGitHubRepoURL attempts to derive a canonical GitHub repository URL from a Go
// import path using (in order):
//  1. Module root resolution via moduleRootResolver (network / proxy lookup)
//  2. Coarse static trimming if the raw import path starts with github.com/
//
// Returned URL form (on success): https://github.com/<owner>/<repo>
// Returns empty string when no GitHub repository can be inferred.
//
// Notes:
//   - Major version suffix segments (/v2, /v3, ...) in module paths are ignored for repo identity.
//   - Only GitHub hosts are currently supported; future hosts can extend this logic.
func synthesizeGoGitHubRepoURL(ctx context.Context, r moduleRootResolver, importPath string) string {
	ip := strings.TrimSpace(importPath)
	if ip == "" {
		return ""
	}
	// Step 1: Try authoritative module root resolution first.
	if r != nil {
		if mod, _, err := r.ResolveModuleRoot(ctx, ip); err == nil && strings.HasPrefix(mod, "github.com/") {
			parts := strings.Split(strings.TrimPrefix(mod, "github.com/"), "/")
			if len(parts) >= 2 {
				repoURL := "https://github.com/" + parts[0] + "/" + parts[1]
				slog.Debug("deps.dev: synthesized repo URL from golang module root", "module", mod, "repo", repoURL)
				return repoURL
			}
		}
	}
	// Step 2: Fallback from the raw import path (static heuristic).
	if strings.HasPrefix(ip, "github.com/") {
		if gh := fallbackGitHubOwnerRepo(ip); gh != "" {
			repoURL := "https://github.com/" + gh
			slog.Debug("deps.dev: fallback-synthesized repo URL from golang import path", "import_path", ip, "repo", repoURL)
			return repoURL
		}
	}
	return ""
}

// attemptGoRepoURLFromPackageName unescapes a raw Go module/import path (if percent-encoded)
// and delegates to synthesizeGoGitHubRepoURL for authoritative+fallback inference.
// Returns empty string when no GitHub repository can be inferred.
func attemptGoRepoURLFromPackageName(ctx context.Context, gp *goproxy.Client, raw string) string {
	if raw == "" {
		return ""
	}
	if u, err := url.PathUnescape(raw); err == nil && u != "" {
		raw = u
	}
	return synthesizeGoGitHubRepoURL(ctx, gp, raw)
}

// ================= Additional Go-specific normalization helpers =================

// GoModuleNormalization captures the outcome of preparing a Go module name for the
// deps.dev versions endpoint (systems/{system}/packages/{name}). It is focused on
// producing a stable module root path (optionally containing a /vN major suffix)
// and its escaped form, not on repository URL identity (which intentionally drops
// /vN). Repository URL synthesis remains the responsibility of synthesizeGoGitHubRepoURL.
//
// Invariants:
//   - When Strategy != "none": ModuleRootRaw != "" and EscapedName != ""
//   - Strategy ∈ {"proxy", "fallback", "fallback-no-proxy", "none"}
//   - Changed indicates the escaped name differs (case-insensitive) from the original package name
type GoModuleNormalization struct {
	ModuleRootRaw string // e.g. github.com/owner/repo[/v2]
	EscapedName   string // URL-escaped form used as {name} in deps.dev endpoint
	Changed       bool   // true if normalization altered the name
	Strategy      string // proxy | fallback | fallback-no-proxy | none
}

// normalizeGoModuleForVersions normalizes a parsed Go PURL to its module root to
// maximize hit probability on the deps.dev versions endpoint. It prefers authoritative
// proxy-based normalization, falling back to a coarse syntactic guess. It does NOT
// decide repository identity; that logic is separate so that repo identity can drop
// major suffixes while version listing can retain them.
func normalizeGoModuleForVersions(ctx context.Context, gp *goproxy.Client, parsed *purl.ParsedPURL) GoModuleNormalization {
	rawPkg := strings.TrimSpace(parsed.GetPackageName())
	if rawPkg == "" {
		return GoModuleNormalization{Strategy: "none"}
	}

	// Authoritative (proxy) normalization first.
	if gp != nil {
		if mod, esc, ok := golangresolve.NormalizePURLToModuleRoot(ctx, gp, parsed, ""); ok && esc != "" {
			return GoModuleNormalization{
				ModuleRootRaw: mod,
				EscapedName:   esc,
				Changed:       !strings.EqualFold(esc, rawPkg),
				Strategy:      "proxy",
			}
		}
	}

	// Fallback (static guess independent of proxy availability).
	if fbRaw, fbEsc := fallbackGoModuleCandidate(rawPkg); fbEsc != "" {
		strat := "fallback"
		if gp == nil {
			strat = "fallback-no-proxy"
		}
		return GoModuleNormalization{
			ModuleRootRaw: fbRaw,
			EscapedName:   fbEsc,
			Changed:       !strings.EqualFold(fbEsc, rawPkg),
			Strategy:      strat,
		}
	}

	return GoModuleNormalization{Strategy: "none"}
}

// reconstructGoVersionPURL rebuilds a Go PURL string using a normalized module root
// and a specific version. Returns (newPURL, true) on success, or ("", false) if
// reconstruction fails (caller should fallback to simple version substitution).
func reconstructGoVersionPURL(basePURL, moduleRoot, version string) (string, bool) {
	if moduleRoot == "" {
		return "", false
	}
	p, err := packageurl.FromString(basePURL)
	if err != nil {
		return "", false
	}
	// Go: namespace unused; full module path resides in Name
	p.Namespace = ""
	p.Name = moduleRoot
	p.Version = version
	return p.ToString(), true
}

// --- Go-specific fallback helpers (moved from depsdev.go) ---

// fallbackGoModuleCandidate returns a best-effort module root candidate from a raw
// Go import path (purely syntactic) and its URL-escaped form. Keeps github.com/owner/repo
// and optionally a trailing /vN major segment; leaves other hosts unchanged except trimming.
func fallbackGoModuleCandidate(raw string) (rawModule string, escapedName string) {
	ip := strings.Trim(strings.TrimSpace(raw), "/")
	if ip == "" {
		return "", ""
	}
	parts := strings.Split(ip, "/")
	if len(parts) >= 3 && parts[0] == "github.com" { // github.com/owner/repo[/vN]
		owner := parts[1]
		repo := strings.TrimSuffix(parts[2], ".git")
		mod := "github.com/" + owner + "/" + repo
		if len(parts) >= 4 && isGoMajorVersionSuffix(parts[3]) {
			mod += "/" + parts[3]
		}
		return mod, url.PathEscape(mod)
	}
	return ip, url.PathEscape(ip)
}

// fallbackGitHubOwnerRepo extracts owner/repo (ignoring an optional /vN major suffix) from a github.com path.
func fallbackGitHubOwnerRepo(raw string) string {
	ip := strings.Trim(strings.TrimSpace(raw), "/")
	if !strings.HasPrefix(ip, "github.com/") {
		return ""
	}
	rest := strings.TrimPrefix(ip, "github.com/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		return ""
	}
	if len(parts) >= 3 && isGoMajorVersionSuffix(parts[2]) { // strip /vN
		parts = parts[:2]
	}
	repo := strings.TrimSuffix(parts[1], ".git")
	return parts[0] + "/" + repo
}

// isGoMajorVersionSuffix reports whether seg matches v<digits> (module major version suffix style).
func isGoMajorVersionSuffix(seg string) bool {
	if len(seg) < 2 || seg[0] != 'v' {
		return false
	}
	for i := 1; i < len(seg); i++ {
		if seg[i] < '0' || seg[i] > '9' {
			return false
		}
	}
	return true
}
