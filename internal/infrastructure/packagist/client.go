package packagist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// Client is a minimal Packagist API client for resolving repository URLs.
// It queries https://packagist.org/packages/{vendor}/{package}.json and extracts the repository field.
type Client struct {
	baseURL  string
	http     *httpclient.Client
	mu       sync.Mutex
	cache    map[string]cacheEntry
	cacheTTL time.Duration
}

// NewClient creates a Packagist client with sane defaults.
// Timeout defaults to 3 seconds.
func NewClient() *Client {
	return &Client{
		baseURL:  "https://packagist.org",
		http:     httpclient.NewClient(&http.Client{Timeout: 3 * time.Second}, httpclient.RetryConfig{MaxRetries: 2, BaseBackoff: 400 * time.Millisecond, MaxBackoff: 2 * time.Second, RetryOn5xx: true, RetryOnNetworkErr: true}),
		cache:    make(map[string]cacheEntry),
		cacheTTL: 5 * time.Minute,
	}
}

// SetBaseURL overrides the API base URL (for testing).
func (c *Client) SetBaseURL(u string) { c.baseURL = u }

// SetHTTPClient allows tests to inject a custom HTTP client.
func (c *Client) SetHTTPClient(h *http.Client) {
	c.http = httpclient.NewClient(h, httpclient.DefaultRetryConfig())
}

// SetCacheTTL adjusts in-memory cache TTL for package lookups.
// Zero or negative disables caching.
func (c *Client) SetCacheTTL(d time.Duration) {
	c.mu.Lock()
	c.cacheTTL = d
	c.mu.Unlock()
}

// packageResponse is the subset of the Packagist package JSON we care about.
type packageResponse struct {
	Package struct {
		Repository string `json:"repository"`
		// Abandoned reflects Packagist/Composer "abandoned" metadata.
		// Patterns (authoritative sources):
		//   - Packagist API: https://packagist.org/apidoc#package
		//   - Composer schema: https://getcomposer.org/doc/04-schema.md#abandoned
		// Values:
		//   - boolean true  => package is abandoned (no explicit successor)
		//   - string        => package is abandoned, string is the recommended successor (e.g. "vendor/name")
		//   - false/missing => not abandoned
		// Examples (live endpoints; responses may evolve over time):
		//   - String successor (zendframework → laminas):
		//       https://packagist.org/packages/zendframework/zend-diactoros.json
		//       (abandoned: "laminas/laminas-diactoros")
		//   - Boolean true (no successor):
		//       https://packagist.org/packages/ircmaxell/password-compat.json
		//       (abandoned: true)
		//   - Not abandoned:
		//       https://packagist.org/packages/monolog/monolog.json
		//       (no "abandoned" key or false)
		Abandoned any `json:"abandoned"`
		// Dependents is the number of packages that depend on this package.
		// Returned by Packagist API: GET /packages/{vendor}/{name}.json
		// Example: https://packagist.org/packages/monolog/monolog.json -> "dependents": 24851
		Dependents int `json:"dependents"`
	} `json:"package"`
}

// cacheEntry stores a decoded response and status with a timestamp.
type cacheEntry struct {
	data      *packageResponse
	status    int
	fetchedAt time.Time
}

// fetchPackage performs the HTTP request and JSON decoding once, with a small TTL cache.
// Returns the decoded response and HTTP status code (for 404 handling).
func (c *Client) fetchPackage(ctx context.Context, vendor, name string) (*packageResponse, int, error) {
	vendor = strings.TrimSpace(vendor)
	name = strings.TrimSpace(name)
	if vendor == "" || name == "" {
		return nil, 0, fmt.Errorf("vendor/name required")
	}

	key := vendor + "/" + name
	// Cache lookup
	if c.cacheTTL > 0 {
		c.mu.Lock()
		if ce, ok := c.cache[key]; ok {
			if time.Since(ce.fetchedAt) < c.cacheTTL {
				data := ce.data
				status := ce.status
				c.mu.Unlock()
				return data, status, nil
			}
		}
		c.mu.Unlock()
	}

	endpoint := fmt.Sprintf("%s/packages/%s/%s.json", c.baseURL, vendor, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("User-Agent", "uzomuzo-packagist-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, 0, fmt.Errorf("http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		// Cache negative result to avoid repeated lookups
		if c.cacheTTL > 0 {
			c.mu.Lock()
			c.cache[key] = cacheEntry{data: nil, status: http.StatusNotFound, fetchedAt: time.Now()}
			c.mu.Unlock()
		}
		return nil, http.StatusNotFound, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var data packageResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode failed: %w", err)
	}

	if c.cacheTTL > 0 {
		c.mu.Lock()
		c.cache[key] = cacheEntry{data: &data, status: resp.StatusCode, fetchedAt: time.Now()}
		c.mu.Unlock()
	}
	return &data, resp.StatusCode, nil
}

// GetRepoURL resolves repository URL for composer vendor/name.
// Returns a normalized GitHub URL or empty string if not found or not GitHub.
func (c *Client) GetRepoURL(ctx context.Context, vendor, name, version string) (string, error) {
	vendor = strings.TrimSpace(vendor)
	name = strings.TrimSpace(name)
	if vendor == "" || name == "" {
		// Preserve old behavior: empty input is not an error here.
		return "", nil
	}
	resp, status, err := c.fetchPackage(ctx, vendor, name)
	if err != nil {
		return "", err
	}
	if status == http.StatusNotFound || resp == nil {
		return "", nil
	}
	norm := common.NormalizeRepositoryURL(resp.Package.Repository)
	if norm == "" || !common.IsValidGitHubURL(norm) {
		return "", nil
	}
	return norm, nil
}

// GetDependentCount returns the number of packages that depend on this package.
// Uses the same Packagist API as GetRepoURL/GetAbandoned (cached via fetchPackage).
// Returns 0, nil when the package is not found (404).
//
// DDD Layer: Infrastructure
// Endpoint: GET /packages/{vendor}/{name}.json -> package.dependents
// Docs: https://packagist.org/apidoc#package
func (c *Client) GetDependentCount(ctx context.Context, vendor, name string) (int, error) {
	vendor = strings.TrimSpace(vendor)
	name = strings.TrimSpace(name)
	if vendor == "" || name == "" {
		return 0, nil
	}
	resp, status, err := c.fetchPackage(ctx, vendor, name)
	if err != nil {
		return 0, fmt.Errorf("packagist dependents lookup failed for %s/%s: %w", vendor, name, err)
	}
	if status == http.StatusNotFound || resp == nil {
		return 0, nil
	}
	return resp.Package.Dependents, nil
}

// GetAbandoned returns whether a composer package is marked abandoned and optional successor.
// Mapping and references for maintainability:
//   - Packagist API (packages/{vendor}/{name}.json): https://packagist.org/apidoc#package
//   - Composer schema (composer.json "abandoned"):  https://getcomposer.org/doc/04-schema.md#abandoned
//
// Interpretation:
//   - abandoned: true        => (abandoned=true, successor="")
//   - abandoned: "vendor/pkg" => (abandoned=true, successor="vendor/pkg")
//   - missing/false/other    => (abandoned=false, successor="")
//
// Successor is empty when the registry only provides a boolean flag.
func (c *Client) GetAbandoned(ctx context.Context, vendor, name string) (bool, string, error) {
	vendor = strings.TrimSpace(vendor)
	name = strings.TrimSpace(name)
	if vendor == "" || name == "" {
		// Preserve old behavior: this method returned an error for empty input.
		return false, "", fmt.Errorf("vendor/name required")
	}
	resp, status, err := c.fetchPackage(ctx, vendor, name)
	if err != nil {
		return false, "", err
	}
	if status == http.StatusNotFound || resp == nil {
		return false, "", nil
	}
	switch v := resp.Package.Abandoned.(type) {
	case bool:
		return v, "", nil
	case string:
		if strings.TrimSpace(v) == "" {
			return true, "", nil
		}
		return true, v, nil
	default:
		return false, "", nil
	}
}
