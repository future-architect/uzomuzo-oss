// Package hex is a minimal hex.pm API client (repository URL and release retirement).
package hex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// Retirement describes a retired release (reason: deprecated, security, renamed, invalid, other).
type Retirement struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// packageResponse is the subset of GET /api/packages/:name we use.
type packageResponse struct {
	Meta struct {
		Links map[string]string `json:"links"`
	} `json:"meta"`
	Retirements map[string]Retirement `json:"retirements"`
}

type cacheEntry struct {
	data      *packageResponse // nil => 404
	fetchedAt time.Time
}

// Client queries https://hex.pm/api/packages/:name. Responses are cached for 5 minutes.
type Client struct {
	baseURL string
	http    *httpclient.Client
	mu      sync.Mutex
	cache   map[string]cacheEntry
}

// NewClient creates a Hex client with a 3 second timeout.
func NewClient() *Client {
	return &Client{
		baseURL: "https://hex.pm",
		http:    httpclient.NewClient(&http.Client{Timeout: 3 * time.Second}, httpclient.RegistryRetryConfig()),
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
