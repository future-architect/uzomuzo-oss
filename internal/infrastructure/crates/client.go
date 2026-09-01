// Package crates provides a minimal crates.io metadata client used for
// detecting yanked versions during EOL evaluation.
//
// DDD Layer: Infrastructure
// Responsibility: External HTTP call to https://crates.io/api/v1/crates/<name>/<version>
// with narrow field extraction (yanked flag) required by the EOL evaluator.
package crates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common/ttlcache"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// cratesUserAgent is the User-Agent sent on all crates.io HTTP requests.
// crates.io rejects requests without a descriptive User-Agent (returns 403).
const cratesUserAgent = "uzomuzo-crates-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)"

// maxJSONResponseSize caps the crates.io JSON API response body (1 MB).
// crates.io version responses are typically <10 KB.
const maxJSONResponseSize = 1 << 20

// Client fetches crates.io version metadata.
type Client struct {
	http       *httpclient.Client
	baseURL    string
	cache      ttlcache.Cache[*VersionInfo]
	crateCache ttlcache.Cache[*CrateInfo]
}

// CrateInfo is the crate-level subset of crates.io metadata we need.
type CrateInfo struct {
	Name string
	// Yanked mirrors the top-level crate.yanked field: every published version
	// of this crate is yanked. See ADR-0022.
	Yanked bool
}

// VersionInfo is the minimal subset of crates.io version metadata we need.
type VersionInfo struct {
	Name    string
	Version string
	Yanked  bool
}

// NewClient returns a crates.io client with sensible HTTP defaults.
func NewClient() *Client {
	hc := &http.Client{Timeout: 5 * time.Second}
	c := &Client{
		http:    httpclient.NewClient(hc, httpclient.RegistryRetryConfig()),
		baseURL: "https://crates.io",
	}
	c.cache.SetTTL(10 * time.Minute)
	c.crateCache.SetTTL(10 * time.Minute)
	return c
}

// SetHTTPClient overrides the underlying http.Client (tests).
func (c *Client) SetHTTPClient(h *http.Client) {
	if h == nil {
		return
	}
	c.http = httpclient.NewClient(h, httpclient.RegistryRetryConfig())
}

// SetBaseURL overrides the base host (tests).
func (c *Client) SetBaseURL(u string) { c.baseURL = strings.TrimRight(u, "/") }

// SetCacheTTL sets the in-memory cache TTL (<=0 disables caching) for both the
// version-level and the crate-level caches.
func (c *Client) SetCacheTTL(d time.Duration) {
	c.cache.SetTTL(d)
	c.crateCache.SetTTL(d)
}

// resolvedBaseURL returns the configured base URL or the default.
func (c *Client) resolvedBaseURL() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return "https://crates.io"
}

func (c *Client) cacheKey(name, version string) string {
	return strings.ToLower(name) + "@" + version
}

// GetVersion retrieves crate version metadata. Returns (info, found, err).
// On 404 -> (nil, false, nil). Other non-200 -> error.
func (c *Client) GetVersion(ctx context.Context, name, version string) (*VersionInfo, bool, error) {
	n := strings.TrimSpace(name)
	v := strings.TrimSpace(version)
	if n == "" || v == "" {
		return nil, false, nil
	}
	key := c.cacheKey(n, v)
	if info, ok := c.cache.Get(key); ok {
		slog.Debug("crates: cache hit", "name", n, "version", v)
		return info, true, nil
	}
	apiURL := fmt.Sprintf("%s/api/v1/crates/%s/%s", c.resolvedBaseURL(), url.PathEscape(n), url.PathEscape(v))
	var raw struct {
		Version struct {
			Crate  string `json:"crate"`
			Num    string `json:"num"`
			Yanked bool   `json:"yanked"`
		} `json:"version"`
	}
	found, err := c.getJSON(ctx, apiURL, "version", &raw)
	if err != nil || !found {
		return nil, found, err
	}
	info := &VersionInfo{
		Name:    raw.Version.Crate,
		Version: raw.Version.Num,
		Yanked:  raw.Version.Yanked,
	}
	c.cache.Set(key, info)
	return info, true, nil
}

// GetCrate retrieves crate-level metadata, whose yanked flag reports whether
// every published version of the crate is yanked. Returns (info, found, err).
// On 404 -> (nil, false, nil). Other non-200 -> error; callers must treat that
// as "unknown" and must not fall back to a heavier request shape.
//
// The empty include parameter suppresses the versions array, which keeps the
// response under a kilobyte for crates with hundreds of releases. Both the
// parameter and the crate-level yanked flag are crates.io's own API, not part
// of the Cargo Registry Web API. See ADR-0022.
func (c *Client) GetCrate(ctx context.Context, name string) (*CrateInfo, bool, error) {
	n := strings.TrimSpace(name)
	if n == "" {
		return nil, false, nil
	}
	key := strings.ToLower(n)
	if info, ok := c.crateCache.Get(key); ok {
		slog.Debug("crates: crate cache hit", "name", n)
		return info, true, nil
	}
	apiURL := fmt.Sprintf("%s/api/v1/crates/%s?include=", c.resolvedBaseURL(), url.PathEscape(n))
	var raw struct {
		Crate struct {
			Name   string `json:"name"`
			Yanked bool   `json:"yanked"`
		} `json:"crate"`
	}
	found, err := c.getJSON(ctx, apiURL, "crate", &raw)
	if err != nil || !found {
		return nil, found, err
	}
	// A 200 whose body carries no crate name is not an answer about the crate.
	// Reporting it as found would assert "nothing yanked" from an empty body.
	if strings.TrimSpace(raw.Crate.Name) == "" {
		return nil, false, fmt.Errorf("crates crate response for %q carried no crate name", n)
	}
	info := &CrateInfo{Name: raw.Crate.Name, Yanked: raw.Crate.Yanked}
	c.crateCache.Set(key, info)
	return info, true, nil
}

// getJSON issues a GET against apiURL and decodes the body into out.
// Returns (found, err): 404 -> (false, nil); other non-200 -> (false, error).
// Callers must treat an error as "unknown", never as a negative answer.
func (c *Client) getJSON(ctx context.Context, apiURL, what string, out any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return false, fmt.Errorf("crates %s request build failed: %w", what, err)
	}
	req.Header.Set("User-Agent", cratesUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return false, fmt.Errorf("crates %s http failed: %w", what, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("crates %s http status %d", what, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONResponseSize)).Decode(out); err != nil {
		return false, fmt.Errorf("crates %s decode failed: %w", what, err)
	}
	return true, nil
}
