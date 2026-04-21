// Package govanityresolve resolves Go vanity import URLs
// (gopkg.in, go.uber.org, k8s.io, etc.) to their canonical
// GitHub repository URLs by fetching the Go module's HTML
// discovery endpoint (`?go-get=1`) and parsing the
// `<meta name="go-import">` / `<meta name="go-source">` tags
// defined by https://go.dev/ref/mod#vcs-find.
//
// DDD Layer: Infrastructure (outbound HTTP + HTML scraping).
// Domain / Application layers must not import this package directly;
// instead use it via IntegrationService enrichment steps.
package govanityresolve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// maxBodyBytes caps the size of the fetched HTML response. Vanity discovery
// pages are tiny (a handful of <meta> tags); anything larger is almost
// certainly a misconfigured upstream or an attempt to exhaust memory.
const maxBodyBytes = 64 * 1024

// defaultTimeout is applied per HTTP request. Vanity resolution is a
// best-effort enrichment — failures silently leave the caller's URL
// unchanged, so a short timeout is safer than a long one.
const defaultTimeout = 8 * time.Second

// maxRedirects caps how deep an HTTP redirect chain the resolver will
// follow. Legitimate vanity hosts resolve with zero or one hop; anything
// deeper is almost certainly an attempt to smuggle the fetch to a private
// destination through the resolver.
const maxRedirects = 3

// drainLimitBytes caps how much of an unused response body we consume
// before closing the connection, to keep it reusable by the HTTP
// transport's keep-alive pool without reading megabytes of error pages.
const drainLimitBytes = 1024

// Resolver resolves Go vanity import URLs to canonical GitHub URLs.
// Safe for concurrent use. Successful lookups and authoritative negative
// results are cached in-process for the lifetime of the Resolver.
// Network failures and context cancellations are NOT cached, so a
// transient outage cannot permanently poison the cache. The cache grows
// unbounded; Resolver is intended for short-lived CLI-scoped processes.
type Resolver struct {
	http           *http.Client
	allowNonPublic bool
	cache          sync.Map // canonical vanity URL (string) → result (string, "" for negative)
}

// NewResolver constructs a Resolver with sane defaults. The HTTP client
// enforces the full hardening profile described in ResolveRepoURL:
// redirects are bounded and each hop must be an HTTPS URL to a public
// Internet host.
func NewResolver() *Resolver {
	r := &Resolver{}
	r.http = &http.Client{
		Timeout:       defaultTimeout,
		CheckRedirect: r.checkRedirect,
	}
	return r
}

// NewResolverForTest constructs a Resolver using the provided HTTP client and
// relaxes the public-host / https-only restrictions so tests can stub the
// network via httptest (which binds to 127.0.0.1 over HTTP). The name makes
// the relaxation explicit so it cannot be mistaken for a generic "inject a
// custom transport" constructor — production callers MUST use NewResolver.
//
// The resolver's hardened CheckRedirect is installed onto the client if the
// client does not already have one set, so test code gets production-parity
// redirect handling (bounded hops, host re-validation) without needing to
// reach into internal fields.
//
// Panics if c is nil: an unconstrained default client here would silently
// grant Internet access to a test-mode resolver, which is exactly the
// surprise the name change is designed to prevent.
func NewResolverForTest(c *http.Client) *Resolver {
	if c == nil {
		panic("govanityresolve: NewResolverForTest requires a non-nil http.Client")
	}
	r := &Resolver{allowNonPublic: true, http: c}
	if r.http.CheckRedirect == nil {
		r.http.CheckRedirect = r.checkRedirect
	}
	return r
}

// ResolveRepoURL returns the canonical GitHub repository URL for the given
// repository URL if and only if:
//
//   - The URL parses, uses the `https` scheme, carries no userinfo, and has a
//     non-empty public host that is not `github.com`.
//   - Fetching `<url>?go-get=1` succeeds (following at most maxRedirects
//     hops, each of which must also be HTTPS to a public host) and returns
//     a `<meta name="go-import">` (or `<meta name="go-source">`) whose target
//     host is `github.com`.
//
// Returns the canonical URL (e.g. `https://github.com/go-yaml/yaml`) and ok=true
// on success. Returns empty string and ok=false otherwise. The input URL is
// never returned as the output; callers are expected to keep the original
// value when ok=false.
//
// Authoritative negative results (server responded, no GitHub target found)
// are cached so repeat lookups do not hit the network. Transient failures —
// network errors, HTTP 5xx, and context cancellation / deadline — are NOT
// cached, so the resolver recovers when the upstream becomes reachable again.
func (r *Resolver) ResolveRepoURL(ctx context.Context, repoURL string) (string, bool) {
	if r == nil {
		return "", false
	}
	normalized, host, ok := normalizeVanityURL(repoURL, r.allowNonPublic)
	if !ok {
		return "", false
	}
	// Nothing to resolve: already a GitHub URL.
	if strings.EqualFold(host, "github.com") {
		return "", false
	}

	if cached, found := r.cache.Load(normalized); found {
		if s, _ := cached.(string); s != "" {
			return s, true
		}
		return "", false
	}

	resolved, authoritative := r.fetchAndParse(ctx, normalized)
	// Only cache when we actually heard from the server. Caching transient
	// failures (ctx cancellation, TCP resets, 5xx) would permanently poison
	// the cache for the Resolver's lifetime.
	if authoritative {
		r.cache.Store(normalized, resolved)
	}
	if resolved == "" {
		return "", false
	}
	return resolved, true
}

// fetchAndParse issues the `?go-get=1` request and extracts a canonical
// GitHub URL. It returns (canonical, authoritative) where:
//   - canonical is the resolved GitHub URL, or "" when resolution failed.
//   - authoritative is true only when the server produced a conclusive
//     response (2xx with parseable body, or 4xx). Transient failures
//     (network errors, context cancellation, 5xx) return authoritative=false
//     so ResolveRepoURL can retry on a future call instead of caching the
//     failure.
func (r *Resolver) fetchAndParse(ctx context.Context, canonicalURL string) (string, bool) {
	requestURL := canonicalURL + "?go-get=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		slog.Debug("vanity_resolve_request_build_failed", "url", requestURL, "error", err)
		return "", false
	}
	req.Header.Set("User-Agent", "uzomuzo-govanityresolve/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := r.http.Do(req)
	if err != nil {
		// errUnsafeRedirect is returned from checkRedirect for untrusted
		// hops. Treat it as an authoritative negative so attackers cannot
		// force us to retry the same SSRF attempt on every batch.
		if errors.Is(err, errUnsafeRedirect) {
			slog.Debug("vanity_resolve_unsafe_redirect", "url", requestURL, "error", err)
			return "", true
		}
		slog.Debug("vanity_resolve_fetch_failed", "url", requestURL, "error", err)
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusInternalServerError {
		_, _ = io.CopyN(io.Discard, resp.Body, drainLimitBytes)
		slog.Debug("vanity_resolve_server_error", "url", requestURL, "status", resp.StatusCode)
		return "", false
	}
	// HTTP 408 (Request Timeout) and 429 (Too Many Requests) are transient
	// conditions — treat them the same as 5xx so they are not negative-cached.
	if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests {
		_, _ = io.CopyN(io.Discard, resp.Body, drainLimitBytes)
		slog.Debug("vanity_resolve_transient_status", "url", requestURL, "status", resp.StatusCode)
		return "", false
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.CopyN(io.Discard, resp.Body, drainLimitBytes)
		slog.Debug("vanity_resolve_non_200", "url", requestURL, "status", resp.StatusCode)
		return "", true
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		slog.Debug("vanity_resolve_read_failed", "url", requestURL, "error", err)
		return "", false
	}
	if len(body) > maxBodyBytes {
		slog.Debug("vanity_resolve_body_too_large", "url", requestURL, "limit", maxBodyBytes)
		return "", true
	}
	html := string(body)

	// Extract the import-path prefix from the vanity URL (host + path) so
	// we can select the most specific matching go-import / go-source entry,
	// as required by https://go.dev/ref/mod#vcs-find.
	importPath := importPathFromURL(canonicalURL)

	canonical := parseGoImport(html, importPath)
	if canonical == "" {
		// Some hosts (notably gopkg.in) emit `go-import` pointing back at
		// themselves and expose the canonical GitHub URL only via `go-source`.
		canonical = parseGoSource(html, importPath)
	}
	if canonical == "" {
		slog.Debug("vanity_resolve_no_github_target", "url", requestURL)
		return "", true
	}
	slog.Debug("vanity_resolve_success", "original", canonicalURL, "canonical", canonical)
	return canonical, true
}

// normalizeVanityURL parses repoURL, upgrades scheme-less inputs to https,
// and returns the cleaned URL along with its lowercase host. It rejects:
//
//   - Empty or unparseable inputs.
//   - Hostless URLs (`https:///path`).
//   - Non-HTTPS schemes. Plain HTTP to attacker-influenced hosts on an
//     internal network is an SSRF vector. Relaxed in test mode.
//   - URLs carrying userinfo (`https://user@host/...`). A valid vanity
//     repo URL never has credentials; a userinfo component is almost
//     always an attempt to visually disguise the real host. This check
//     is NOT relaxed in test mode.
//   - URLs whose host resolves to a loopback / link-local / private-
//     range address (see isPublicHost). Relaxed in test mode so
//     httptest servers (bound to 127.0.0.1) are dial-able.
func normalizeVanityURL(repoURL string, allowNonPublic bool) (canonical, host string, ok bool) {
	s := strings.TrimSpace(repoURL)
	if s == "" {
		return "", "", false
	}
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", "", false
	}
	if !allowNonPublic && !strings.EqualFold(u.Scheme, "https") {
		return "", "", false
	}
	if u.User != nil {
		return "", "", false
	}
	h := strings.ToLower(u.Hostname())
	if !allowNonPublic && !isPublicHost(h) {
		return "", "", false
	}
	// Drop fragments and queries; keep the path as provided. Leading vs
	// trailing slash differences still produce distinct cache keys, which
	// is fine — the set of vanity URLs a caller emits is small.
	u.Fragment = ""
	u.RawQuery = ""
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host) // canonicalize host for cache-key dedup
	return u.String(), h, true
}

// errUnsafeRedirect signals that a redirect was rejected because it
// violated the resolver's SSRF-hardening policy. Returned wrapped inside
// http.Client.Do's err, so callers use errors.Is to detect it.
var errUnsafeRedirect = errors.New("unsafe redirect rejected")

// checkRedirect enforces the resolver's redirect policy on every hop:
//
//   - At most maxRedirects hops are followed.
//   - Each hop must target an HTTPS URL with no userinfo (relaxed in
//     test mode; see NewResolverForTest).
//   - The resolved host must not be a loopback / link-local / private-
//     range address, which would expose internal services via SSRF
//     (relaxed in test mode).
func (r *Resolver) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return fmt.Errorf("%w: exceeded %d hops", errUnsafeRedirect, maxRedirects)
	}
	if req.URL == nil {
		return fmt.Errorf("%w: nil URL", errUnsafeRedirect)
	}
	if req.URL.User != nil {
		return fmt.Errorf("%w: userinfo in redirect", errUnsafeRedirect)
	}
	if !r.allowNonPublic {
		if !strings.EqualFold(req.URL.Scheme, "https") {
			return fmt.Errorf("%w: non-https scheme %q", errUnsafeRedirect, req.URL.Scheme)
		}
		if !isPublicHost(strings.ToLower(req.URL.Hostname())) {
			return fmt.Errorf("%w: non-public host %q", errUnsafeRedirect, req.URL.Hostname())
		}
	}
	return nil
}

// isPublicHost reports whether host looks safe to dial over the public
// Internet. Hostnames are kept (DNS resolution would be expensive and
// racy), but literal IP addresses are classified and loopback,
// link-local, multicast, unspecified, and private ranges are rejected.
// Common hostnames that trivially map to loopback (`localhost` and the
// cloud metadata service) are also refused by name.
func isPublicHost(host string) bool {
	if host == "" {
		return false
	}
	// Strip trailing dot (FQDN form) so that "localhost." and
	// "metadata.google.internal." are rejected the same as their
	// non-FQDN equivalents — a common SSRF bypass vector.
	host = strings.TrimRight(host, ".")
	if host == "" {
		return false
	}
	switch host {
	case "localhost",
		"ip6-localhost",
		"ip6-loopback",
		"metadata.google.internal":
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() {
			return false
		}
	}
	return true
}

// metaContentRE matches `<meta>` tag bodies. `[^>]` matches newlines, so
// multi-line attribute values (as emitted by k8s.io) are captured
// correctly without an (?s) flag.
var metaContentRE = regexp.MustCompile(`<meta\b([^>]*)>`)

// attrRE matches a single HTML attribute inside a tag body.
var attrRE = regexp.MustCompile(`([a-zA-Z][\w-]*)\s*=\s*"([^"]*)"`)

// parseGoImport extracts the canonical GitHub URL from the `go-import`
// meta tag. Returns the empty string if no suitable tag is present or
// the target host is not github.com.
//
// Spec: https://go.dev/ref/mod#vcs-find — content is whitespace-separated
// "<prefix> <vcs> <url>". When importPath is non-empty and multiple entries
// are present, the most specific prefix that matches the import path wins.
// When importPath is empty or no prefix matches, the first github.com/git
// entry wins (best-effort fallback for single-entry pages).
func parseGoImport(html, importPath string) string {
	var firstCanonical string
	var bestCanonical string
	var bestPrefix string
	for _, content := range metaContents(html, "go-import") {
		fields := strings.Fields(content)
		if len(fields) < 3 {
			continue
		}
		prefix := fields[0]
		vcs := strings.ToLower(fields[1])
		if vcs != "git" {
			continue
		}
		canonical := githubRepoFromURL(fields[2])
		if canonical == "" {
			continue
		}
		if firstCanonical == "" {
			firstCanonical = canonical
		}
		if importPath != "" && prefixMatches(importPath, prefix) && len(prefix) > len(bestPrefix) {
			bestCanonical = canonical
			bestPrefix = prefix
		}
	}
	if bestCanonical != "" {
		return bestCanonical
	}
	return firstCanonical
}

// parseGoSource extracts the canonical GitHub URL from the `go-source`
// meta tag. Handles two shapes emitted in the wild:
//
//  1. `<prefix> <home> <dir> <file>` where `<home>` is an explicit
//     https://github.com/... URL (e.g. go.uber.org).
//  2. `<prefix> _ <dir> <file>` where `<home>` is `_` (unset) and the
//     `<dir>` template embeds `https://github.com/owner/repo/tree/...` —
//     this is how gopkg.in advertises the canonical source location.
//
// When importPath is non-empty and multiple entries are present, the most
// specific prefix that matches the import path wins (same as parseGoImport).
func parseGoSource(html, importPath string) string {
	var firstCanonical string
	var bestCanonical string
	var bestPrefix string
	for _, content := range metaContents(html, "go-source") {
		fields := strings.Fields(content)
		if len(fields) < 3 {
			continue
		}
		prefix := fields[0]
		canonical := githubRepoFromURL(fields[1])
		if canonical == "" {
			// Extract owner/repo from the dir template (field index 2) by
			// trimming everything from `/tree/` or `/blob/` onward, which
			// are the two GitHub URL shapes used in go-source templates.
			canonical = githubRepoFromURL(trimTemplateTail(fields[2]))
		}
		if canonical == "" {
			continue
		}
		if firstCanonical == "" {
			firstCanonical = canonical
		}
		if importPath != "" && prefixMatches(importPath, prefix) && len(prefix) > len(bestPrefix) {
			bestCanonical = canonical
			bestPrefix = prefix
		}
	}
	if bestCanonical != "" {
		return bestCanonical
	}
	return firstCanonical
}

// metaContents returns the `content` attribute value of every `<meta>` tag
// whose `name` attribute equals target (case-insensitive).
func metaContents(html, target string) []string {
	var out []string
	for _, m := range metaContentRE.FindAllStringSubmatch(html, -1) {
		if len(m) < 2 {
			continue
		}
		body := m[1]
		var name, content string
		for _, a := range attrRE.FindAllStringSubmatch(body, -1) {
			if len(a) < 3 {
				continue
			}
			key := strings.ToLower(a[1])
			switch key {
			case "name":
				name = a[2]
			case "content":
				content = a[2]
			}
		}
		if strings.EqualFold(name, target) && content != "" {
			out = append(out, content)
		}
	}
	return out
}

// githubRepoFromURL returns `https://github.com/<owner>/<repo>` if raw parses
// into a URL whose host is github.com (case-insensitive) and whose path
// contains at least `<owner>/<repo>`. Returns the empty string otherwise.
func githubRepoFromURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || s == "_" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return ""
	}
	if !strings.EqualFold(u.Hostname(), "github.com") {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	return fmt.Sprintf("https://github.com/%s/%s", owner, repo)
}

// importPathFromURL extracts the Go import path (host + path) from a
// canonical vanity URL like "https://go.uber.org/zap" → "go.uber.org/zap".
// Returns "" if the URL is unparseable.
func importPathFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Hostname()) + strings.TrimRight(u.Path, "/")
}

// prefixMatches reports whether prefix is an exact match or a path-element
// prefix of importPath, per the go-import spec: the client selects the
// entry whose prefix matches the import path or is a path-element prefix
// of it, preferring the longest match.
// Both values are compared case-insensitively (hosts are case-insensitive
// per RFC 3986, and Go module paths are conventionally lowercase).
func prefixMatches(importPath, prefix string) bool {
	ip := strings.ToLower(importPath)
	p := strings.ToLower(prefix)
	if ip == p {
		return true
	}
	return strings.HasPrefix(ip, p+"/")
}

// trimTemplateTail strips `/tree/...` or `/blob/...` suffixes from a
// go-source directory template so the remaining URL points at a repo root.
func trimTemplateTail(raw string) string {
	s := raw
	for _, marker := range []string{"/tree/", "/blob/"} {
		if idx := strings.Index(s, marker); idx >= 0 {
			s = s[:idx]
		}
	}
	return s
}
