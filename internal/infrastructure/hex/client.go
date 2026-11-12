// Package hex is a minimal hex.pm API client (repository URL, release retirement, security advisories).
package hex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// Retirement describes a retired release (reason: deprecated, security, renamed, invalid, other).
type Retirement struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// SecurityAdvisory is one entry of hex.pm's security_advisories, which hex.pm
// mirrors from OSV. CVSSScore is a CVSS v4.0 base score; zero means none.
type SecurityAdvisory struct {
	ID          string   `json:"id"`
	Aliases     []string `json:"aliases"`
	Summary     string   `json:"summary"`
	CVSSScore   float64  `json:"cvss_score"`
	WithdrawnAt *string  `json:"withdrawn_at"`
	// Affected lists OR'd version ranges such as "< 1.2.0" or ">= 1.0.0 and < 1.2.0".
	Affected []string `json:"affected"`
}

// packageResponse is the subset of GET /api/packages/:name we use.
type packageResponse struct {
	Meta struct {
		Links map[string]string `json:"links"`
	} `json:"meta"`
	Retirements         map[string]Retirement `json:"retirements"`
	LatestStableVersion string                `json:"latest_stable_version"`
	SecurityAdvisories  []SecurityAdvisory    `json:"security_advisories"`
}

type cacheEntry struct {
	data      *packageResponse // nil => 404
	fetchedAt time.Time
}

// Client queries https://hex.pm/api/packages/:name. Responses are cached for 5 minutes.
type Client struct {
	baseURL string
	apiKey  string
	http    *httpclient.Client
	mu      sync.Mutex
	cache   map[string]cacheEntry
}

// NewClient creates a Hex client with a 3 second per-attempt timeout.
// HEX_API_KEY (optional) is sent as the Authorization header for a higher rate limit.
// 429 responses wait for Retry-After (up to 60s) before retrying; hex.pm limits per minute,
// so the shared 2s registry cap would exhaust retries.
func NewClient() *Client {
	rc := httpclient.RegistryRetryConfig()
	rc.MaxRetries = 3
	rc.MaxBackoff = 60 * time.Second
	return &Client{
		baseURL: "https://hex.pm",
		apiKey:  strings.TrimSpace(os.Getenv("HEX_API_KEY")),
		http:    httpclient.NewClient(&http.Client{Timeout: 3 * time.Second}, rc),
		cache:   make(map[string]cacheEntry),
	}
}

// SetBaseURL overrides the API base URL (for testing).
func (c *Client) SetBaseURL(u string) { c.baseURL = u }

func (c *Client) fetch(ctx context.Context, name string) (*packageResponse, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil, fmt.Errorf("name required")
	}
	c.mu.Lock()
	ce, ok := c.cache[name]
	c.mu.Unlock()
	if ok && time.Since(ce.fetchedAt) < 5*time.Minute {
		return ce.data, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/packages/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", c.apiKey)
	}
	req.Header.Set("User-Agent", "uzomuzo-hex-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var data *packageResponse
	switch resp.StatusCode {
	case http.StatusOK:
		data = &packageResponse{}
		if err := json.NewDecoder(resp.Body).Decode(data); err != nil {
			return nil, fmt.Errorf("decode failed: %w", err)
		}
	case http.StatusNotFound:
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	c.mu.Lock()
	c.cache[name] = cacheEntry{data: data, fetchedAt: time.Now()}
	c.mu.Unlock()
	return data, nil
}

// GetRepoURL returns a normalized GitHub URL from meta.links, or "" when the
// package is unknown or has no GitHub link. Link labels are free-form
// ("GitHub", "Github", ...), so every value is checked.
func (c *Client) GetRepoURL(ctx context.Context, name string) (string, error) {
	data, err := c.fetch(ctx, name)
	if err != nil || data == nil {
		return "", err
	}
	keys := make([]string, 0, len(data.Meta.Links))
	for k := range data.Meta.Links {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		link := data.Meta.Links[k]
		if norm := common.NormalizeRepositoryURL(link); norm != "" && common.IsValidGitHubURL(norm) {
			return norm, nil
		}
	}
	return "", nil
}

// GetRetirement returns the retirement of the given release, or nil when the
// package/release is unknown or not retired.
func (c *Client) GetRetirement(ctx context.Context, name, version string) (*Retirement, error) {
	data, err := c.fetch(ctx, name)
	if err != nil || data == nil {
		return nil, err
	}
	r, ok := data.Retirements[version]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

// GetStableAdvisories returns the latest stable release and the advisories that
// affect it, one per vulnerability. version is "" when the package is unknown or
// has no stable release.
//
// hex.pm lists one vulnerability under several IDs that alias each other (a
// GHSA and an EEF-CVE record). The GHSA entry is kept, because deps.dev reports
// GHSA IDs for the other ecosystems. Withdrawn advisories are dropped.
func (c *Client) GetStableAdvisories(ctx context.Context, name string) (version string, advisories []SecurityAdvisory, err error) {
	data, err := c.fetch(ctx, name)
	if err != nil || data == nil || data.LatestStableVersion == "" {
		return "", nil, err
	}
	v, _ := semver.NewVersion(data.LatestStableVersion)
	seen := map[string]bool{}
	for _, ghsaPass := range []bool{true, false} {
		for _, a := range data.SecurityAdvisories {
			if strings.HasPrefix(a.ID, "GHSA-") != ghsaPass || a.WithdrawnAt != nil || !affects(a.Affected, v) {
				continue
			}
			if seen[a.ID] || slices.ContainsFunc(a.Aliases, func(id string) bool { return seen[id] }) {
				continue
			}
			seen[a.ID] = true
			for _, id := range a.Aliases {
				seen[id] = true
			}
			advisories = append(advisories, a)
		}
	}
	return data.LatestStableVersion, advisories, nil
}

// affects reports whether v falls in any of the affected ranges.
// ponytail: an unparseable version or range, or no ranges, counts as affected, so a
// format change over-reports instead of hiding advisories; parse Elixir requirements fully if it misfires.
func affects(ranges []string, v *semver.Version) bool {
	if v == nil || len(ranges) == 0 {
		return true
	}
	for _, r := range ranges {
		c, err := semver.NewConstraint(strings.ReplaceAll(r, " and ", ", "))
		if err != nil || c.Check(v) {
			return true
		}
	}
	return false
}
