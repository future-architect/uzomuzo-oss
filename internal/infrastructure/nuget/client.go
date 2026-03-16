package nuget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// Defaults for nuget.org
const (
	defaultRegistrationBase = "https://api.nuget.org/v3/registration5-semver2"
	defaultServiceIndexURL  = "https://api.nuget.org/v3/index.json"
)

// Client is a minimal NuGet V3 client focused on deprecation metadata via Registration API.
//
// DDD Layer: Infrastructure
// Responsibility: External HTTP to api.nuget.org registration endpoints to retrieve deprecation reasons.
//
// Authoritative docs and behaviors:
//   - Registration base resource (index/pages/leaves):
//     https://learn.microsoft.com/nuget/api/registration-base-url-resource
//   - Package deprecation schema and reasons list:
//     https://learn.microsoft.com/nuget/api/registration-base-url-resource#package-deprecation
//   - Lowercase path requirement for registration index (LOWER_ID):
//     https://learn.microsoft.com/nuget/api/registration-base-url-resource#request-parameters
//   - Multiple registration hives (registration5-semver2 and registration5-gz-semver2) and SemVer2 inclusion:
//     https://learn.microsoft.com/nuget/api/registration-base-url-resource#versioning
//
// Implementation notes:
//   - Requests the registration index at {base}/{lower_id}/index.json; falls back between semver2 and gz-semver2 hives.
//   - Extracts deprecation from embedded page leaves or page documents; no per-leaf crawling required.
//
// Design note: Service Index Auto-Discovery (future enhancement)
//   - Today, this client uses a fixed Registration Base URL with a sibling-variant fallback (semver2 <-> gz-semver2).
//   - A robust next step is reading the NuGet V3 service index (https://api.nuget.org/v3/index.json) at startup
//     and selecting the Registration Base URL resources advertised there. This enables:
//   - Dynamic discovery in non-nuget.org registries
//   - Automatic resource updates without code changes
//   - Suggested approach:
//     1) Add a discovery method: Fetch service index JSON once (with retry/backoff) and cache the resource list.
//     2) Filter resources by @type: "RegistrationsBaseUrl" variants (including semver2 and gz-semver2).
//     3) Use the discovered set as candidates for GetDeprecation lookups.
//     This improves resiliency and aligns with NuGet client guidance while keeping the domain layer unaffected.
type Client struct {
	baseURL string
	http    *httpclient.Client

	mu       sync.Mutex
	cache    map[string]cacheEntry
	cacheTTL time.Duration
	// If true, do not cache negative results (found=false). Default false (cache both).
	NoCacheNotFound bool

	// Service index discovery cache
	serviceIndexURL string
	serviceIndexTTL time.Duration
	discoveredBases []string
	discoveredAt    time.Time

	// HTML base for nuget.org UI (used by fallback scraper). Overridable for tests.
	htmlBase string
}

// NewClient creates a NuGet client with sane defaults.
func NewClient() *Client {
	return &Client{
		baseURL: defaultRegistrationBase,
		// Increase timeout: registration indexes for large packages (e.g., Azure SDKs) can be sizable
		// and nuget.org may respond slowly at times. 12s strikes a balance between resiliency and UX.
		http:            httpclient.NewClient(&http.Client{Timeout: 12 * time.Second}, httpclient.RetryConfig{MaxRetries: 2, BaseBackoff: 500 * time.Millisecond, MaxBackoff: 3 * time.Second, RetryOn5xx: true, RetryOnNetworkErr: true}),
		cache:           make(map[string]cacheEntry),
		cacheTTL:        5 * time.Minute,
		serviceIndexURL: defaultServiceIndexURL,
		serviceIndexTTL: 30 * time.Minute,
		htmlBase:        "https://www.nuget.org",
	}
}

// SetHTTPClient allows injecting a custom HTTP client (for tests).
func (c *Client) SetHTTPClient(h *http.Client) {
	c.http = httpclient.NewClient(h, httpclient.DefaultRetryConfig())
}

// SetBaseURL allows overriding the registration base URL (for tests).
func (c *Client) SetBaseURL(u string) { c.baseURL = strings.TrimRight(u, "/") }

// SetCacheTTL sets the in-memory cache TTL. Zero or negative disables caching.
func (c *Client) SetCacheTTL(d time.Duration) {
	c.mu.Lock()
	c.cacheTTL = d
	c.mu.Unlock()
}

// SetServiceIndexURL overrides the service index URL (for tests or custom registries).
func (c *Client) SetServiceIndexURL(u string) {
	c.mu.Lock()
	c.serviceIndexURL = strings.TrimSpace(u)
	// Invalidate discovery cache to force refresh against the new index URL
	c.discoveredBases = nil
	c.discoveredAt = time.Time{}
	c.mu.Unlock()
}

// SetServiceIndexTTL sets the TTL used for caching service index discovery results.
func (c *Client) SetServiceIndexTTL(d time.Duration) {
	c.mu.Lock()
	c.serviceIndexTTL = d
	c.mu.Unlock()
}

// SetHTMLBase overrides the base host for nuget.org HTML (used by fallback scraper and tests).
func (c *Client) SetHTMLBase(u string) {
	c.mu.Lock()
	c.htmlBase = strings.TrimRight(strings.TrimSpace(u), "/")
	c.mu.Unlock()
}

// DeprecationInfo carries NuGet deprecation metadata (if any).
type DeprecationInfo struct {
	Reasons            []string
	Message            string
	AlternatePackageID string
}

type cacheEntry struct {
	info      *DeprecationInfo
	found     bool
	fetchedAt time.Time
}

// GetDeprecation fetches deprecation information for a package id.
// Returns (info, found, error). When found=false, info is nil.
func (c *Client) GetDeprecation(ctx context.Context, packageID string) (*DeprecationInfo, bool, error) {
	id := strings.TrimSpace(packageID)
	if id == "" {
		return nil, false, fmt.Errorf("package id is required")
	}

	// Cache
	if c.cacheTTL > 0 {
		c.mu.Lock()
		if ce, ok := c.cache[id]; ok {
			if time.Since(ce.fetchedAt) < c.cacheTTL {
				info := ce.info
				found := ce.found
				c.mu.Unlock()
				slog.Debug("nuget: cache hit for deprecation", "id", id, "found", found)
				return info, found, nil
			}
		}
		c.mu.Unlock()
	}

	// Registration index endpoint (with fallback base variants)
	idLower := strings.ToLower(id) // NuGet registration path requires lowercase package ID
	candidates := c.getRegistrationCandidates(ctx)

	var last404 bool
	var anyOK bool
	var decodeErrors int
	for idx, b := range candidates {
		endpoint := fmt.Sprintf("%s/%s/index.json", b, url.PathEscape(idLower))
		slog.Debug("nuget: request registration index", "id", id, "endpoint", endpoint, "attempt", idx+1)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, false, fmt.Errorf("failed to build NuGet request: %w", err)
		}
		// Be a good citizen: set a descriptive User-Agent
		req.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
		resp, err := c.http.Do(ctx, req)
		if err != nil {
			return nil, false, fmt.Errorf("NuGet HTTP error: %w", err)
		}
		if resp.StatusCode == http.StatusNotFound {
			slog.Debug("nuget: registration index not found", "id", id, "endpoint", endpoint)
			last404 = true
			resp.Body.Close()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			slog.Debug("nuget: non-OK status from registration index", "id", id, "status", resp.StatusCode, "endpoint", endpoint)
			resp.Body.Close()
			// Non-OK other than 404: treat as error for now
			return nil, false, fmt.Errorf("NuGet HTTP %d", resp.StatusCode)
		}

		anyOK = true
		var reg registrationIndex
		if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
			// Do not abort on decode errors (e.g., slow network/timeouts while reading body).
			// Try next candidate (gz/non-gz) or fallback scraper.
			slog.Debug("nuget: decode failed for registration index", "id", id, "error", err)
			resp.Body.Close()
			decodeErrors++
			continue
		}
		resp.Body.Close()

		// Iterate pages; items may be embedded or referenced.
		for _, page := range reg.Items {
			// Embedded leaves
			if len(page.Items) > 0 {
				if info, ok := extractFirstDeprecation(page.Items); ok {
					slog.Debug("nuget: deprecation found (embedded)", "id", id, "reasons", info.Reasons, "alt", info.AlternatePackageID)
					c.remember(id, info)
					return info, true, nil
				}
				continue
			}
			// Need to fetch page
			if page.ID == "" {
				continue
			}
			slog.Debug("nuget: fetching registration page", "id", id, "page", page.ID)
			piReq, err := http.NewRequestWithContext(ctx, http.MethodGet, page.ID, nil)
			if err != nil {
				return nil, false, fmt.Errorf("NuGet request (page) failed: %w", err)
			}
			piReq.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
			piResp, err := c.http.Do(ctx, piReq)
			if err != nil {
				return nil, false, fmt.Errorf("NuGet HTTP (page) error: %w", err)
			}
			if piResp.StatusCode != http.StatusOK {
				piResp.Body.Close()
				slog.Debug("nuget: non-OK page status", "id", id, "status", piResp.StatusCode, "page", page.ID)
				continue
			}
			var leaf registrationPage
			if err := json.NewDecoder(piResp.Body).Decode(&leaf); err != nil {
				piResp.Body.Close()
				slog.Debug("nuget: page decode failed", "id", id, "error", err)
				return nil, false, fmt.Errorf("NuGet page decode failed: %w", err)
			}
			piResp.Body.Close()
			if info, ok := extractFirstDeprecation(leaf.Items); ok {
				slog.Debug("nuget: deprecation found (page)", "id", id, "reasons", info.Reasons, "alt", info.AlternatePackageID)
				c.remember(id, info)
				return info, true, nil
			}
		}
		// No deprecation in this variant, try next
		slog.Debug("nuget: no deprecation in this index variant", "id", id, "endpoint", endpoint)
	}

	// No deprecation found across all attempts
	if !anyOK && last404 {
		slog.Debug("nuget: no deprecation found (all variants 404)", "id", id)
	} else {
		slog.Debug("nuget: no deprecation found (checked variants)", "id", id)
	}
	// Best-effort HTML fallback for nuget.org: scrape the package page for a deprecation banner
	if info, ok := c.scrapeDeprecationFromNuGetHTML(ctx, id); ok {
		slog.Debug("nuget: deprecation found via HTML fallback", "id", id, "alt", info.AlternatePackageID)
		c.remember(id, info)
		return info, true, nil
	}
	c.remember(id, nil)
	return nil, false, nil
}

// fetchLeafDeprecation removed: leaf-by-leaf crawling is unnecessary after embedded/page extraction handles deprecation.

func (c *Client) remember(id string, info *DeprecationInfo) {
	if c.cacheTTL <= 0 {
		return
	}
	c.mu.Lock()
	// Optionally skip caching negative results to reduce staleness when deprecation is newly added
	if info == nil && c.NoCacheNotFound {
		c.mu.Unlock()
		return
	}
	c.cache[id] = cacheEntry{info: info, found: info != nil, fetchedAt: time.Now()}
	c.mu.Unlock()
}

// Minimal JSON shapes for Registration API
type registrationIndex struct {
	Items []registrationPageRef `json:"items"`
}

type registrationPageRef struct {
	ID    string             `json:"@id"`
	Items []registrationLeaf `json:"items"`
}

type registrationPage struct {
	Items []registrationLeaf `json:"items"`
}

type registrationLeaf struct {
	ID string `json:"@id"`
	// CatalogEntry can be either an object or a string URL depending on embedding.
	// We don't need its contents for deprecation checks, so accept any JSON.
	CatalogEntry json.RawMessage          `json:"catalogEntry"`
	Deprecation  *registrationDeprecation `json:"deprecation"`
}

type registrationDeprecation struct {
	Reasons          []string                `json:"reasons"`
	Message          string                  `json:"message"`
	AlternatePackage *registrationAltPackage `json:"alternatePackage"`
}

type registrationAltPackage struct {
	ID    string `json:"id"`
	Range string `json:"range"`
}

// catalogEntryDoc represents the minimal shape of a catalog entry when embedded or fetched.
type catalogEntryDoc struct {
	Deprecation *registrationDeprecation `json:"deprecation"`
}

func extractFirstDeprecation(items []registrationLeaf) (*DeprecationInfo, bool) {
	for _, it := range items {
		// 1) Leaf-level deprecation
		if it.Deprecation != nil {
			altID := ""
			if it.Deprecation.AlternatePackage != nil {
				altID = strings.TrimSpace(it.Deprecation.AlternatePackage.ID)
			}
			info := &DeprecationInfo{
				Reasons:            append([]string(nil), it.Deprecation.Reasons...),
				Message:            strings.TrimSpace(it.Deprecation.Message),
				AlternatePackageID: altID,
			}
			return info, true
		}
		// 2) catalogEntry may be embedded as an object and include deprecation
		if len(it.CatalogEntry) > 0 && len(it.CatalogEntry) >= 1 && it.CatalogEntry[0] == '{' {
			var ce catalogEntryDoc
			if err := json.Unmarshal(it.CatalogEntry, &ce); err == nil && ce.Deprecation != nil {
				altID := ""
				if ce.Deprecation.AlternatePackage != nil {
					altID = strings.TrimSpace(ce.Deprecation.AlternatePackage.ID)
				}
				info := &DeprecationInfo{
					Reasons:            append([]string(nil), ce.Deprecation.Reasons...),
					Message:            strings.TrimSpace(ce.Deprecation.Message),
					AlternatePackageID: altID,
				}
				return info, true
			}
		}
	}
	return nil, false
}

// GetRepoURL attempts to extract a repository URL (preferably GitHub) for the given NuGet package ID.
//
// DDD Layer: Infrastructure
// Behavior:
//   - Queries the NuGet Registration index like GetDeprecation
//   - Scans embedded page leaves for an embedded catalogEntry and extracts repository/project URL fields
//   - If needed, fetches registration pages to inspect embedded catalogEntry entries
//   - Returns a normalized URL or an empty string when not determinable
func (c *Client) GetRepoURL(ctx context.Context, packageID string, _ string) (string, error) {
	id := strings.TrimSpace(packageID)
	if id == "" {
		return "", fmt.Errorf("package id is required")
	}

	idLower := strings.ToLower(id)
	candidates := c.getRegistrationCandidates(ctx)

	for idx, b := range candidates {
		endpoint := fmt.Sprintf("%s/%s/index.json", b, url.PathEscape(idLower))
		slog.Debug("nuget: request registration index (repo)", "id", id, "endpoint", endpoint, "attempt", idx+1)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return "", fmt.Errorf("failed to build NuGet request: %w", err)
		}
		req.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
		resp, err := c.http.Do(ctx, req)
		if err != nil {
			return "", fmt.Errorf("NuGet HTTP error: %w", err)
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return "", fmt.Errorf("NuGet HTTP %d", resp.StatusCode)
		}
		var reg registrationIndex
		if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
			resp.Body.Close()
			return "", fmt.Errorf("NuGet decode failed: %w", err)
		}
		resp.Body.Close()

		// 1) Inspect embedded leaves for catalogEntry object
		for _, page := range reg.Items {
			if len(page.Items) > 0 {
				if repo := extractRepoURLFromLeaves(page.Items); repo != "" {
					if resolved := c.resolveRepoURLHeuristics(ctx, repo); resolved != "" {
						return resolved, nil
					}
					return repo, nil
				}
				continue
			}
			if page.ID == "" {
				continue
			}
			// 2) Fetch page document and inspect its items
			piReq, err := http.NewRequestWithContext(ctx, http.MethodGet, page.ID, nil)
			if err != nil {
				return "", fmt.Errorf("NuGet request (page) failed: %w", err)
			}
			piReq.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
			piResp, err := c.http.Do(ctx, piReq)
			if err != nil {
				return "", fmt.Errorf("NuGet HTTP (page) error: %w", err)
			}
			if piResp.StatusCode != http.StatusOK {
				piResp.Body.Close()
				continue
			}
			var leaf registrationPage
			if err := json.NewDecoder(piResp.Body).Decode(&leaf); err != nil {
				piResp.Body.Close()
				return "", fmt.Errorf("NuGet page decode failed: %w", err)
			}
			piResp.Body.Close()
			if repo := extractRepoURLFromLeaves(leaf.Items); repo != "" {
				if resolved := c.resolveRepoURLHeuristics(ctx, repo); resolved != "" {
					return resolved, nil
				}
				return repo, nil
			}
		}
	}
	return "", nil
}

// extractRepoURLFromLeaves inspects registration leaves for an embedded catalogEntry object
// and attempts to extract a repository or project URL from it. Returns a normalized URL or empty string.
func extractRepoURLFromLeaves(items []registrationLeaf) string {
	for _, it := range items {
		if len(it.CatalogEntry) == 0 || it.CatalogEntry[0] != '{' {
			continue
		}
		// Use a permissive map to handle variations: repository (object or string), projectUrl, repositoryUrl
		var m map[string]any
		if err := json.Unmarshal(it.CatalogEntry, &m); err != nil {
			continue
		}
		// Try repository (object with url)
		if v, ok := m["repository"]; ok {
			switch rv := v.(type) {
			case map[string]any:
				if u, ok := rv["url"].(string); ok && strings.TrimSpace(u) != "" {
					if norm := normalizeRepoURL(u); norm != "" {
						return norm
					}
				}
			case string:
				if norm := normalizeRepoURL(rv); norm != "" {
					return norm
				}
			}
		}
		// Try repositoryUrl (string)
		if u, ok := m["repositoryUrl"].(string); ok && strings.TrimSpace(u) != "" {
			if norm := normalizeRepoURL(u); norm != "" {
				return norm
			}
		}
		// Fallback to projectUrl (string)
		if u, ok := m["projectUrl"].(string); ok && strings.TrimSpace(u) != "" {
			if norm := normalizeRepoURL(u); norm != "" {
				return norm
			}
		}
	}
	return ""
}

// normalizeRepoURL trims and returns the URL as-is; the consumer will further normalize
// to GitHub project keys if needed. Keep it minimal here.
func normalizeRepoURL(s string) string {
	return strings.TrimSpace(s)
}

// getRegistrationCandidates returns a list of Registration Base URLs to try.
// Order of precedence:
//  1. Discovered bases from the service index (cached with TTL)
//  2. Configured baseURL
//  3. Sibling variant on nuget.org (semver2 <-> gz-semver2)
//  4. Fallback to known nuget.org gz-semver2 base
func (c *Client) getRegistrationCandidates(ctx context.Context) []string {
	base := strings.TrimRight(c.baseURL, "/")
	// If baseURL is overridden (not the default), honor it and skip network discovery
	if base != strings.TrimRight(defaultRegistrationBase, "/") {
		candidates := []string{base}
		if strings.Contains(base, "registration5-semver2") && !strings.Contains(base, "registration5-gz-semver2") {
			candidates = append(candidates, strings.Replace(base, "registration5-semver2", "registration5-gz-semver2", 1))
		} else if strings.Contains(base, "registration5-gz-semver2") {
			candidates = append(candidates, strings.Replace(base, "registration5-gz-semver2", "registration5-semver2", 1))
		} else {
			candidates = append(candidates, "https://api.nuget.org/v3/registration5-gz-semver2")
		}
		return candidates
	}

	// Otherwise, try discovery first
	discovered := c.discoverRegistrationBases(ctx)
	if len(discovered) > 0 {
		return discovered
	}
	candidates := []string{base}
	if strings.Contains(base, "registration5-semver2") && !strings.Contains(base, "registration5-gz-semver2") {
		candidates = append(candidates, strings.Replace(base, "registration5-semver2", "registration5-gz-semver2", 1))
	} else if strings.Contains(base, "registration5-gz-semver2") {
		candidates = append(candidates, strings.Replace(base, "registration5-gz-semver2", "registration5-semver2", 1))
	} else {
		candidates = append(candidates, "https://api.nuget.org/v3/registration5-gz-semver2")
	}
	return candidates
}

// ServiceIndex represents the minimal shape of the NuGet v3 service index.
type ServiceIndex struct {
	Resources []struct {
		ID   string `json:"@id"`
		Type string `json:"@type"`
	} `json:"resources"`
}

// discoverRegistrationBases loads the service index (if TTL expired) and extracts Registration Base URLs.
// It caches the discovered list and returns it for use by callers.
func (c *Client) discoverRegistrationBases(ctx context.Context) []string {
	c.mu.Lock()
	idxURL := c.serviceIndexURL
	ttl := c.serviceIndexTTL
	// If cache is fresh, return it immediately.
	if len(c.discoveredBases) > 0 && time.Since(c.discoveredAt) < ttl {
		bases := append([]string(nil), c.discoveredBases...)
		c.mu.Unlock()
		return bases
	}
	c.mu.Unlock()

	// No fresh cache, attempt discovery.
	if strings.TrimSpace(idxURL) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, idxURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var idx ServiceIndex
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return nil
	}

	// Collect registration base URLs. Prefer semver2, then gz-semver2, then any RegistrationsBaseUrl.
	var semver2 []string
	var gzSemver2 []string
	var generic []string
	for _, r := range idx.Resources {
		t := strings.ToLower(r.Type)
		if r.ID == "" || t == "" {
			continue
		}
		switch {
		case strings.Contains(t, "registrationsbaseurl/3.6.0") || strings.Contains(t, "registrationsbaseurl/3.5.0") || strings.Contains(t, "registrationsbaseurl/3.0.0"):
			// Generic match; we will still sort by content of the URL for semver2 preference later
			if strings.Contains(strings.ToLower(r.ID), "registration5-semver2") {
				semver2 = append(semver2, strings.TrimRight(r.ID, "/"))
			} else if strings.Contains(strings.ToLower(r.ID), "registration5-gz-semver2") {
				gzSemver2 = append(gzSemver2, strings.TrimRight(r.ID, "/"))
			} else {
				generic = append(generic, strings.TrimRight(r.ID, "/"))
			}
		case strings.Contains(t, "registrationsbaseurl"):
			// Fallback broad match
			if strings.Contains(strings.ToLower(r.ID), "registration5-semver2") {
				semver2 = append(semver2, strings.TrimRight(r.ID, "/"))
			} else if strings.Contains(strings.ToLower(r.ID), "registration5-gz-semver2") {
				gzSemver2 = append(gzSemver2, strings.TrimRight(r.ID, "/"))
			} else {
				generic = append(generic, strings.TrimRight(r.ID, "/"))
			}
		}
	}
	// Merge with precedence: semver2 first, then gz-semver2, then generic.
	dedup := make(map[string]bool)
	var bases []string
	for _, list := range [][]string{semver2, gzSemver2, generic} {
		for _, b := range list {
			if !dedup[b] {
				dedup[b] = true
				bases = append(bases, b)
			}
		}
	}
	if len(bases) == 0 {
		return nil
	}
	c.mu.Lock()
	c.discoveredBases = bases
	c.discoveredAt = time.Now()
	c.mu.Unlock()
	return append([]string(nil), bases...)
}

// resolveRepoURLHeuristics attempts to improve a non-GitHub repository URL returned by NuGet metadata.
// For Microsoft packages, NuGet often provides go.microsoft.com/aka.ms short links or docs pages.
// This function will try to follow redirects and, if landing on a docs page, scrape a GitHub repo URL.
func (c *Client) resolveRepoURLHeuristics(ctx context.Context, raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Host)

	// Only attempt network heuristics for Microsoft shorteners/docs
	if host == "aka.ms" || strings.HasSuffix(host, ".microsoft.com") {
		if final := c.followRedirect(ctx, s); final != "" {
			fu, _ := url.Parse(final)
			if fu != nil {
				if strings.Contains(strings.ToLower(fu.Host), "github.com") {
					// Normalize and validate before returning
					if norm := common.NormalizeRepositoryURL(final); norm != "" && common.IsValidGitHubURL(norm) {
						return norm
					}
					if common.IsValidGitHubURL(final) {
						return final
					}
				}
				// If we landed on a docs page, attempt to find a GitHub link within the HTML
				if strings.Contains(strings.ToLower(fu.Host), "docs.microsoft.com") || strings.Contains(strings.ToLower(fu.Host), "learn.microsoft.com") {
					if gh := c.scrapeFirstGitHubFromHTML(ctx, final); gh != "" {
						return gh
					}
				}
			}
		}
	}
	return ""
}

// followRedirect performs a GET request and returns the final URL after redirects.
func (c *Client) followRedirect(ctx context.Context, startURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, startURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	// Drain small body to allow re-use
	io.CopyN(io.Discard, resp.Body, 1024)
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String()
	}
	return ""
}

// scrapeFirstGitHubFromHTML fetches the page and returns the first GitHub repository URL found in the HTML.
func (c *Client) scrapeFirstGitHubFromHTML(ctx context.Context, pageURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return ""
	}
	// Read up to 512KB to avoid large downloads
	const maxRead = 512 * 1024
	limited := io.LimitReader(resp.Body, maxRead)
	body, err := io.ReadAll(limited)
	if err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	// Simple regex to find a GitHub URL
	re := regexp.MustCompile(`https?://github\.com/[^"'\s<>]+`)
	if m := re.Find(body); len(m) > 0 {
		// Normalize candidate URL and validate it's a GitHub repo URL
		candidate := string(m)
		normalized := common.NormalizeRepositoryURL(candidate)
		if normalized != "" && common.IsValidGitHubURL(normalized) {
			return normalized
		}
		// Fallback to raw candidate if normalization empties, but still looks valid
		if common.IsValidGitHubURL(candidate) {
			return candidate
		}
	}
	return ""
}

// scrapeDeprecationFromNuGetHTML performs a best-effort parse of the nuget.org package page
// to determine if the package is deprecated and to extract a suggested successor.
// It returns (info, true) when a deprecation banner is detected. Message and successor are optional.
func (c *Client) scrapeDeprecationFromNuGetHTML(ctx context.Context, id string) (*DeprecationInfo, bool) {
	base := c.htmlBase
	if strings.TrimSpace(base) == "" {
		base = "https://www.nuget.org"
	}
	// Use the provided ID as-is; nuget.org routing is case-insensitive.
	pageURL := fmt.Sprintf("%s/packages/%s", strings.TrimRight(base, "/"), url.PathEscape(id))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, false
	}
	// Read up to 512KB
	const maxRead = 512 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRead))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false
	}
	lower := strings.ToLower(string(body))
	if !strings.Contains(lower, "this package has been deprecated") && !strings.Contains(lower, "deprecated") {
		return nil, false
	}
	// Try to extract a successor from a Suggested Alternatives section link
	// Example snippet:
	//   Suggested Alternatives
	//   <a href="/packages/Azure.Messaging.EventHubs">Azure.Messaging.EventHubs</a>
	reAlt := regexp.MustCompile(`(?is)suggested\s+alternatives?.{0,400}?<a[^>]*href="/packages/([A-Za-z0-9_.\-]+)/?"[^>]*>\s*([^<]+)\s*</a>`)
	var altID string
	if m := reAlt.FindSubmatch(body); len(m) >= 2 {
		altID = strings.TrimSpace(string(m[1]))
	}
	// Extract a brief message from the deprecation area if present
	// Heuristic: grab up to 200 chars around the first occurrence of "deprecated"
	msg := ""
	if idx := strings.Index(lower, "deprecated"); idx >= 0 {
		start := idx - 80
		if start < 0 {
			start = 0
		}
		end := idx + 200
		if end > len(body) {
			end = len(body)
		}
		snippet := string(body[start:end])
		// Collapse whitespace
		snippet = regexp.MustCompile(`\s+`).ReplaceAllString(snippet, " ")
		msg = strings.TrimSpace(snippet)
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
	}
	// We do not know the NuGet machine reason (Legacy/CriticalBugs/Other) from HTML alone.
	// Provide a neutral reason to indicate deprecation was detected via HTML.
	reasons := []string{"Other"}
	return &DeprecationInfo{Reasons: reasons, Message: msg, AlternatePackageID: altID}, true
}
