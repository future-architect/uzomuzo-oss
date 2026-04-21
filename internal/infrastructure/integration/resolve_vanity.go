package integration

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"sync"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// vanityResolveConcurrency caps parallel vanity lookups. Vanity hosts are
// varied and unauthenticated, so the per-host rate-limit concern is
// naturally diffused; a small fan-out is plenty.
const vanityResolveConcurrency = 8

// resolveVanityRepoURLs rewrites Analysis.RepoURL / Repository.URL from Go
// vanity hosts (gopkg.in, go.uber.org, k8s.io, google.golang.org, …) to their
// canonical `github.com/<owner>/<repo>` URL, so the downstream GitHub
// enrichment step can populate Description / Summary / Topics that are
// otherwise empty (issue #322).
//
// Scope:
//   - Only `Package.Ecosystem == "golang"` analyses are eligible. Non-golang
//     ecosystems derive repo URLs from package-registry metadata and never
//     hit this path.
//   - Analyses whose RepoURL is already on github.com (or empty) are skipped.
//
// DDD Layer: Infrastructure (best-effort enrichment with fan-out bounded at
// vanityResolveConcurrency). The resolver keeps an in-process cache, so
// duplicate vanity URLs across analyses result in a single HTTP round-trip.
//
// Ordering: must run BEFORE enhanceAnalysesWithGitHubBatch so the rewritten
// URL enters the GitHub GraphQL path normally.
//
// Preconditions: the IntegrationService must have been constructed via
// NewIntegrationService (which eagerly installs a default resolver).
// Zero-value struct-literal construction leaves vanityResolver nil, in
// which case this step is a no-op — guarding that way also lets tests
// exercise the "no resolver wired" path without a network stub.
func (s *IntegrationService) resolveVanityRepoURLs(ctx context.Context, analyses map[string]*domain.Analysis) {
	if len(analyses) == 0 || s.vanityResolver == nil {
		return
	}

	// Collect unique vanity URLs mapped to the analyses that share each URL.
	// A single gopkg.in path often appears across many versions/consumers,
	// so deduplicating here avoids redundant HTTP lookups even before the
	// resolver's own cache kicks in. Keys are normalized so case-variant
	// inputs (`GOPKG.IN/...` vs `gopkg.in/...`) collapse into one group;
	// any remaining mismatch (trailing slash, path-case differences) is
	// still caught by the resolver's internal cache.
	jobs := make(map[string][]*domain.Analysis)
	for _, a := range analyses {
		if !isVanityCandidate(a) {
			continue
		}
		key := vanityDedupKey(a.RepoURL)
		jobs[key] = append(jobs[key], a)
	}
	if len(jobs) == 0 {
		return
	}

	sem := make(chan struct{}, vanityResolveConcurrency)
	var wg sync.WaitGroup
	for _, targets := range jobs {
		// Every target in this group carries the same RepoURL modulo case
		// normalization; any representative is fetch-equivalent because the
		// resolver normalizes internally before hitting the wire. Which
		// one we pick for logging is intentionally non-deterministic
		// (Go map iteration order); the resolver's normalized URL is
		// logged in the `canonical`/`original` slog fields of
		// vanity_resolve_success, which is the stable identifier.
		vanityURL := targets[0].RepoURL
		// Select on both the semaphore acquire and ctx.Done so early
		// cancellation does not block forever when every slot is in use.
		// Already-launched goroutines observe the same ctx and unwind
		// quickly; this just stops dispatching new work.
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(vanityURL string, targets []*domain.Analysis) {
			defer wg.Done()
			defer func() { <-sem }()

			canonical, ok := s.vanityResolver.ResolveRepoURL(ctx, vanityURL)
			if !ok {
				return
			}
			for _, a := range targets {
				a.RepoURL = canonical
				if a.Repository == nil {
					a.Repository = &domain.Repository{}
				}
				a.Repository.URL = canonical
			}
			slog.Debug("vanity_repo_url_rewritten",
				"original", vanityURL, "canonical", canonical, "affected", len(targets))
		}(vanityURL, targets)
	}
	wg.Wait()
}

// isVanityCandidate reports whether an analysis is a Go package whose repo
// URL is a non-GitHub host that may expose a go-import / go-source meta tag.
func isVanityCandidate(a *domain.Analysis) bool {
	if a == nil || a.Package == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(a.Package.Ecosystem), "golang") {
		return false
	}
	host := hostOf(a.RepoURL)
	if host == "" {
		return false
	}
	// Exact-match host comparison is deliberate: common.IsValidGitHubURL
	// uses `strings.Contains(host, "github.com")` which would also match
	// decoy hosts like `not-github.com` and silently skip them as
	// "already GitHub", robbing them of vanity resolution.
	return !strings.EqualFold(host, "github.com")
}

// vanityDedupKey returns a canonical form of raw that collapses case-variant
// inputs into a single map key. Host is lowercased; path/query are preserved
// as-is. Not a full URL normalizer — the resolver's internal cache is the
// authoritative dedup layer; this helper is only defense-in-depth against
// the most common variance (host casing) observed in deps.dev output.
func vanityDedupKey(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	probe := s
	lower := strings.ToLower(probe)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		probe = "https://" + probe
	}
	u, err := url.Parse(probe)
	if err != nil || u.Host == "" {
		return s
	}
	u.Host = strings.ToLower(u.Host)
	u.Scheme = strings.ToLower(u.Scheme)
	return u.String()
}

// hostOf returns the lowercase host of raw, adding https:// if raw has no
// scheme. Returns "" when raw is unparseable or hostless — callers treat that
// the same as "not a vanity candidate".
func hostOf(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
