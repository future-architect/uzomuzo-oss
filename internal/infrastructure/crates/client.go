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
	// Yanked mirrors the top-level crate.yanked field, which crates.io defines
	// as "every published version of this crate is yanked". It is a
	// distribution-withdrawal fact, not end-of-life; see ADR-0022.
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("crates request build failed: %w", err)
	}
	req.Header.Set("User-Agent", cratesUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, false, fmt.Errorf("crates http failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("crates http status %d", resp.StatusCode)
	}
	var raw struct {
		Version struct {
			Crate  string `json:"crate"`
			Num    string `json:"num"`
			Yanked bool   `json:"yanked"`
		} `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONResponseSize)).Decode(&raw); err != nil {
		return nil, false, fmt.Errorf("crates decode failed: %w", err)
	}
	info := &VersionInfo{
		Name:    raw.Version.Crate,
		Version: raw.Version.Num,
		Yanked:  raw.Version.Yanked,
	}
	c.cache.Set(key, info)
	return info, true, nil
}

// GetCrate retrieves crate-level metadata. Returns (info, found, err).
// On 404 -> (nil, false, nil). Other non-200 -> error; callers must treat that
// as "unknown" and must not fall back to a heavier request shape.
//
// The request carries an empty include parameter (?include=), which crates.io
// accepts to mean "no sub-resources": it suppresses the versions array, cutting
// a popular crate's response from hundreds of KB to under 1 KB. This is a
// crates.io-specific API, not part of the Cargo Registry Web API.
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("crates crate request build failed: %w", err)
	}
	req.Header.Set("User-Agent", cratesUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, false, fmt.Errorf("crates crate http failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("crates crate http status %d", resp.StatusCode)
	}
	var raw struct {
		Crate struct {
			Name   string `json:"name"`
			Yanked bool   `json:"yanked"`
		} `json:"crate"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONResponseSize)).Decode(&raw); err != nil {
		return nil, false, fmt.Errorf("crates crate decode failed: %w", err)
	}
	info := &CrateInfo{Name: raw.Crate.Name, Yanked: raw.Crate.Yanked}
	c.crateCache.Set(key, info)
	return info, true, nil
}
