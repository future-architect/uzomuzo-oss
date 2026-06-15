package nuget

import (
	"net/http"
	"strings"
	"sync"
	"time"

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

// cacheEntry stores a deprecation lookup result with its retrieval timestamp.
type cacheEntry struct {
	info      *DeprecationInfo
	found     bool
	fetchedAt time.Time
}

// remember stores a deprecation lookup result in the cache, respecting NoCacheNotFound.
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
