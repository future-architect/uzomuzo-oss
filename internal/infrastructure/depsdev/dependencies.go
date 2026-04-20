package depsdev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	neturl "net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	commonpurl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

// stripGoIncompatibleSuffix returns the version with any trailing
// "+incompatible" / "+<anything>" marker removed for Go modules. deps.dev's
// version paths don't recognize these suffixes. No-op for non-Go ecosystems.
// Called from both FetchDependencies and fetchDependenciesVersionFallback —
// kept in one place so future suffix rules (e.g., "+dirty") land once.
func stripGoIncompatibleSuffix(ecosystem, version string) string {
	if strings.ToLower(ecosystem) != "golang" {
		return version
	}
	if idx := strings.Index(version, "+"); idx >= 0 {
		return version[:idx]
	}
	return version
}

// maxDependencyFallbackVersions bounds the number of additional :dependencies
// requests we issue when the primary resolved version returns 404.
// Rationale (issue #319): 2 retries balances recovery rate against batch
// wall-clock — one extra /packages/{name} lookup plus up to two /dependencies
// calls per affected PURL. Keep this small; a larger window risks masking real
// "package genuinely has no published graph" signals behind noisy success.
const maxDependencyFallbackVersions = 2

// FetchDependencies fetches the dependency graph for a single versioned PURL.
// Returns nil, nil for unsupported ecosystems or 404 responses.
//
// DDD Layer: Infrastructure (external API call)
func (c *DepsDevClient) FetchDependencies(ctx context.Context, purlStr string) (*DependenciesResponse, error) {
	parser := commonpurl.NewParser()
	parsed, err := parser.Parse(purlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PURL for dependencies: %w", err)
	}

	version := strings.TrimSpace(parsed.Version())
	if version == "" {
		slog.Debug("dependencies: skipping versionless PURL", "purl", purlStr)
		return nil, nil
	}
	version = stripGoIncompatibleSuffix(parsed.GetEcosystem(), version)
	if version == "" {
		slog.Debug("dependencies: skipping empty version after suffix strip", "purl", purlStr)
		return nil, nil
	}

	system, name := toDepsDevSystemAndName(parsed)
	escapedVersion := neturl.PathEscape(version)
	endpoint := fmt.Sprintf("%s/systems/%s/packages/%s/versions/%s:dependencies", c.baseURL, system, name, escapedVersion)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create dependencies request (url=%s): %w", endpoint, err)
	}
	req.Header.Set("User-Agent", "uzomuzo-depsdev-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")

	resp, err := c.client.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("dependencies HTTP request failed (url=%s): %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		slog.Debug("dependencies: 404 not found", "purl", purlStr, "url", endpoint)
		return nil, nil // unsupported system or unknown package version
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		snippet := truncateString(string(body), 1024)
		return nil, fmt.Errorf("dependencies HTTP %d (url=%s): %s", resp.StatusCode, endpoint, snippet)
	}

	var result DependenciesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("dependencies JSON decode failed (url=%s): %w", endpoint, err)
	}
	return &result, nil
}

// FetchDependenciesBatch fetches dependency graphs for multiple PURLs in parallel.
// Returns a map of canonical (versionless) PURL -> DependenciesResponse.
// PURLs that fail or return 404 are silently omitted from the result.
//
// DDD Layer: Infrastructure (parallel processing)
func (c *DepsDevClient) FetchDependenciesBatch(ctx context.Context, purls []string) map[string]*DependenciesResponse {
	if len(purls) == 0 {
		return make(map[string]*DependenciesResponse)
	}

	const maxWorkers = 10
	results := make(map[string]*DependenciesResponse)
	var mu sync.Mutex
	semaphore := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, p := range purls {
		wg.Add(1)
		go func(purl string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			resp, err := c.FetchDependencies(ctx, purl)
			if err != nil {
				slog.Debug("Failed to fetch dependencies", "purl", purl, "error", err)
				return
			}
			// Only the 404 / versionless path returns (nil, nil); genuine transport
			// or decode errors take the err branch above. Guard on (resp == nil &&
			// err == nil) per the "Gate Fallback Logic on Error, Not Result Nilness"
			// rule — a zero-node response (leaf) has resp != nil and should NOT
			// trigger the retry. Skip the fallback entirely for versionless PURLs
			// to avoid redundant parsing in the helper.
			if resp == nil {
				if !commonpurl.HasVersion(purl) {
					return
				}
				resp = c.fetchDependenciesVersionFallback(ctx, purl)
				if resp == nil {
					return
				}
			}

			// Normalize to versionless canonical key for map consistency
			key := commonpurl.CanonicalKey(purl)
			if key == "" {
				key = purl
			}

			mu.Lock()
			results[key] = resp
			mu.Unlock()
		}(p)
	}

	wg.Wait()
	slog.Debug("Dependencies batch completed", "requested", len(purls), "successful", len(results))
	return results
}

// fetchDependenciesVersionFallback attempts to recover a dependency graph when
// the primary version returned 404. It lists published versions from
// /systems/{system}/packages/{name}, picks the most recent non-deprecated
// releases other than the one already tried, and issues up to
// maxDependencyFallbackVersions additional :dependencies calls.
//
// Returns nil if: the input is versionless, the package-versions endpoint
// fails, no fallback candidates exist, or every retry also returns 404. In
// all cases the caller preserves the "HasDependencyGraph=false" semantics from
// PR #315 because this helper only overrides nil when a retry succeeds.
//
// DDD Layer: Infrastructure (external API call + bounded retry)
func (c *DepsDevClient) fetchDependenciesVersionFallback(ctx context.Context, purlStr string) *DependenciesResponse {
	parser := commonpurl.NewParser()
	parsed, err := parser.Parse(purlStr)
	if err != nil {
		return nil
	}
	origVersion := strings.TrimSpace(parsed.Version())
	if origVersion == "" {
		return nil // versionless: the primary call already logged and skipped
	}
	// Normalize the primary version via the same rules FetchDependencies uses
	// so the skipVersion dedup key matches what deps.dev publishes in the
	// package listing. This is defense-in-depth: the current integration
	// pipeline (enrichDependencyCounts) filters Go PURLs out before they
	// reach FetchDependenciesBatch, but callers that invoke the exported
	// FetchDependenciesBatch directly (tests, library consumers) would
	// otherwise silently re-attempt the primary through the retry loop.
	origVersion = stripGoIncompatibleSuffix(parsed.GetEcosystem(), origVersion)
	if origVersion == "" {
		return nil
	}

	candidates, err := c.listFallbackVersions(ctx, parsed, origVersion)
	if err != nil {
		slog.Debug("dependencies_version_fallback_list_failed", "purl", purlStr, "error", err)
		return nil
	}
	if len(candidates) == 0 {
		return nil
	}

	for _, v := range candidates {
		if ctx.Err() != nil {
			slog.Debug("dependencies_version_fallback_ctx_cancelled", "purl", purlStr, "error", ctx.Err())
			return nil
		}
		retryPURL, werr := commonpurl.WithVersion(purlStr, v)
		if werr != nil {
			slog.Debug("dependencies_version_fallback_rewrite_failed", "purl", purlStr, "candidate", v, "error", werr)
			continue
		}
		resp, rerr := c.FetchDependencies(ctx, retryPURL)
		if rerr != nil {
			slog.Debug("dependencies_version_fallback_request_failed", "purl", purlStr, "candidate", v, "error", rerr)
			continue
		}
		if resp == nil {
			slog.Debug("dependencies_version_fallback_candidate_404", "purl", purlStr, "candidate", v)
			continue
		}
		slog.Debug("dependencies_version_fallback_recovered",
			"purl", purlStr, "primary_version", origVersion, "fallback_version", v)
		return resp
	}
	return nil
}

// listFallbackVersions returns up to maxDependencyFallbackVersions candidate
// version strings from the deps.dev /packages/{name} endpoint, excluding
// deprecated releases and skipVersion. Sort tiers: stable>prerelease, then
// highest semver within each tier, then publishedAt desc for non-semver ties.
//
// Endpoint URL construction intentionally mirrors fetchLatestRelease in
// depsdev.go (same system/name mapping, same /packages/{name} shape). We do
// not extract a shared helper because fetchLatestRelease additionally performs
// Go-module proxy normalization and computes full ReleaseInfo semantics, which
// are unused here — the fallback only needs the raw version list.
func (c *DepsDevClient) listFallbackVersions(ctx context.Context, parsed *commonpurl.ParsedPURL, skipVersion string) ([]string, error) {
	system, name := toDepsDevSystemAndName(parsed)
	endpoint := fmt.Sprintf("%s/systems/%s/packages/%s", c.baseURL, system, name)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create fallback versions request (url=%s): %w", endpoint, err)
	}
	req.Header.Set("User-Agent", "uzomuzo-depsdev-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")

	resp, err := c.client.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("fallback versions HTTP request failed (url=%s): %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		slog.Debug("dependencies_version_fallback_package_not_found", "system", system, "name", name)
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) // best-effort: surface status code regardless of body read outcome
		return nil, fmt.Errorf("fallback versions HTTP %d (url=%s): %s", resp.StatusCode, endpoint, truncateString(string(body), 512))
	}

	var payload struct {
		Versions []struct {
			VersionKey struct {
				Version string `json:"version"`
			} `json:"versionKey"`
			PublishedAt  string `json:"publishedAt"`
			IsDeprecated bool   `json:"isDeprecated"`
		} `json:"versions"`
	}
	if derr := json.NewDecoder(resp.Body).Decode(&payload); derr != nil {
		return nil, fmt.Errorf("fallback versions JSON decode failed (url=%s): %w", endpoint, derr)
	}

	type candidate struct {
		version     string
		publishedAt time.Time
		hasDate     bool
		isStable    bool
		semver      *semver.Version // nil when the version is not semver-parseable
	}
	eligible := make([]candidate, 0, len(payload.Versions))
	for _, v := range payload.Versions {
		ver := strings.TrimSpace(v.VersionKey.Version)
		if ver == "" || ver == skipVersion || v.IsDeprecated {
			continue
		}
		// Parse semver once and derive both stability and sort key from the
		// single result to avoid the overhead of a second NewVersion call
		// inside isStableReleaseForFallback.
		c := candidate{version: ver}
		if sv, perr := semver.NewVersion(ver); perr == nil {
			c.semver = sv
			c.isStable = sv.Prerelease() == ""
		} else {
			c.isStable = commonpurl.IsStableVersion(ver)
		}
		if v.PublishedAt != "" {
			if t, terr := time.Parse(time.RFC3339, v.PublishedAt); terr == nil {
				c.publishedAt = t
				c.hasDate = true
			}
		}
		eligible = append(eligible, c)
	}
	// Sort order:
	//  1. Stable releases before pre-release / canary / beta tags. deps.dev's
	//     :dependencies endpoint is consistently populated for stable
	//     releases in npm/pypi/cargo/maven — pre-release tags very often 404
	//     (observed on npm react canary/experimental tags), so preferring
	//     stable maximizes fallback recovery rate within the tight call
	//     budget.
	//  2. Highest semver first within each stability tier. This is a better
	//     neighbor for "current dependency surface" than publishedAt-desc
	//     because deps.dev's publishedAt is the index time (when deps.dev
	//     crawled the version), not the upstream release time — batch
	//     re-indexing can reorder semantically-older patches ahead of newer
	//     ones. Semver desc on the same package gives the closest version to
	//     the primary (e.g., for primary=19.2.5, tries 19.2.4 before 19.1.x).
	//  3. PublishedAt desc as a tie-break when both candidates are non-semver
	//     within the same stability tier (e.g., Maven calendar-style release
	//     strings, arbitrary upstream tags).
	sort.SliceStable(eligible, func(i, j int) bool {
		if eligible[i].isStable != eligible[j].isStable {
			return eligible[i].isStable
		}
		if (eligible[i].semver != nil) != (eligible[j].semver != nil) {
			return eligible[i].semver != nil
		}
		if eligible[i].semver != nil && eligible[j].semver != nil {
			return eligible[i].semver.GreaterThan(eligible[j].semver)
		}
		if eligible[i].hasDate != eligible[j].hasDate {
			return eligible[i].hasDate
		}
		return eligible[i].publishedAt.After(eligible[j].publishedAt)
	})

	limit := min(maxDependencyFallbackVersions, len(eligible))
	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, eligible[i].version)
	}
	return out, nil
}
