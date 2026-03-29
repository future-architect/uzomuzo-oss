package pypi

// Package pypi provides a minimal PyPI project metadata client used for
// explicit EOL / deprecation detection (description phrase + successor).
//
// DDD Layer: Infrastructure
// Responsibility: External HTTP call to https://pypi.org/pypi/<project>/json
// with narrow field extraction required by successor/EOL evaluator.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// Client fetches PyPI project JSON metadata.
type Client struct {
	http    *httpclient.Client
	baseURL string

	mu    sync.RWMutex
	cache map[string]cacheEntry
	ttl   time.Duration
}

type cacheEntry struct {
	info *ProjectInfo
	ts   time.Time
}

// NewClient returns a PyPI client with sensible HTTP defaults.
func NewClient() *Client { // nolint: revive
	hc := &http.Client{Timeout: 5 * time.Second}
	return &Client{
		http:    httpclient.NewClient(hc, httpclient.RetryConfig{MaxRetries: 2, BaseBackoff: 400 * time.Millisecond, MaxBackoff: 2 * time.Second, RetryOn5xx: true, RetryOnNetworkErr: true}),
		baseURL: "https://pypi.org",
		cache:   make(map[string]cacheEntry),
		ttl:     10 * time.Minute,
	}
}

// SetHTTPClient overrides the underlying http.Client (tests).
func (c *Client) SetHTTPClient(h *http.Client) { // replaces underlying client preserving retry defaults
	if h == nil {
		return
	}
	c.http = httpclient.NewClient(h, httpclient.RetryConfig{MaxRetries: 2, BaseBackoff: 400 * time.Millisecond, MaxBackoff: 2 * time.Second, RetryOn5xx: true, RetryOnNetworkErr: true})
}

// SetRetryConfig overrides retry configuration (tests / tuning).
func (c *Client) SetRetryConfig(cfg httpclient.RetryConfig) {
	if c.http == nil {
		c.http = httpclient.NewClient(&http.Client{Timeout: 5 * time.Second}, cfg)
		return
	}
	// Rebuild new client with same underlying timeout / transport.
	underlying := c.http
	// Cannot extract original http.Client safely; create new.
	c.http = httpclient.NewClient(&http.Client{Timeout: 5 * time.Second}, cfg)
	_ = underlying // kept to clarify intent; GC handles old instance
}

// SetBaseURL overrides the base host (tests).
func (c *Client) SetBaseURL(u string) { c.baseURL = strings.TrimRight(u, "/") }

// SetCacheTTL sets the in-memory cache TTL (<=0 disables caching).
func (c *Client) SetCacheTTL(d time.Duration) { c.ttl = d }

func (c *Client) getCached(name string) (*ProjectInfo, bool) {
	if c.ttl <= 0 {
		return nil, false
	}
	c.mu.RLock()
	ent, ok := c.cache[name]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Since(ent.ts) > c.ttl {
		return nil, false
	}
	return ent.info, true
}

func (c *Client) setCache(name string, info *ProjectInfo) {
	if c.ttl <= 0 || info == nil {
		return
	}
	c.mu.Lock()
	c.cache[name] = cacheEntry{info: info, ts: time.Now()}
	c.mu.Unlock()
}

// ProjectInfo is the minimal subset of PyPI project metadata we need.
type ProjectInfo struct {
	Name        string
	Summary     string
	Description string
	Classifiers []string
	ProjectURLs map[string]string // e.g. "Repository" -> "https://github.com/..."
	HomePage    string
}

// GetProject retrieves project info. Returns (info, found, err).
// On 404 -> (nil, false, nil). Other non-200 -> error.
func (c *Client) GetProject(ctx context.Context, name string) (*ProjectInfo, bool, error) {
	n := strings.TrimSpace(name)
	if n == "" {
		return nil, false, nil
	}
	lower := strings.ToLower(n)
	if info, ok := c.getCached(lower); ok {
		slog.Debug("pypi: cache hit", "name", lower)
		return info, true, nil
	}
	base := c.baseURL
	if base == "" {
		base = "https://pypi.org"
	}
	url := fmt.Sprintf("%s/pypi/%s/json", base, n)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("pypi request build failed: %w", err)
	}
	req.Header.Set("User-Agent", "uzomuzo-pypi-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, false, fmt.Errorf("pypi http failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("pypi http status %d", resp.StatusCode)
	}
	var raw struct {
		Info struct {
			Name        string            `json:"name"`
			Summary     string            `json:"summary"`
			Description string            `json:"description"`
			Classifiers []string          `json:"classifiers"`
			ProjectURLs map[string]string `json:"project_urls"`
			HomePage    string            `json:"home_page"`
		} `json:"info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, false, fmt.Errorf("pypi decode failed: %w", err)
	}
	info := &ProjectInfo{
		Name:        raw.Info.Name,
		Summary:     raw.Info.Summary,
		Description: raw.Info.Description,
		Classifiers: raw.Info.Classifiers,
		ProjectURLs: raw.Info.ProjectURLs,
		HomePage:    raw.Info.HomePage,
	}
	c.setCache(lower, info)
	return info, true, nil
}

// GetRepoURL resolves a GitHub repository URL for a PyPI package by inspecting
// the project_urls and home_page fields from the PyPI JSON API.
func (c *Client) GetRepoURL(ctx context.Context, name string) (string, error) {
	info, found, err := c.GetProject(ctx, name)
	if err != nil {
		return "", fmt.Errorf("pypi GetRepoURL failed for %s: %w", name, err)
	}
	if !found || info == nil {
		return "", nil
	}
	return extractRepoURL(info), nil
}

// extractRepoURL picks the best GitHub URL from PyPI project metadata.
func extractRepoURL(info *ProjectInfo) string {
	if info == nil {
		return ""
	}

	// Priority 1: project_urls with repository-like keys
	repoKeys := []string{
		"Repository", "Source", "Source Code", "GitHub", "Code",
		"Homepage", "Home", "Project",
	}
	for _, key := range repoKeys {
		for k, v := range info.ProjectURLs {
			if strings.EqualFold(k, key) && isGitHubURL(v) {
				return normalizeGitHub(v)
			}
		}
	}

	// Priority 2: any project_url that points to GitHub
	for _, v := range info.ProjectURLs {
		if isGitHubURL(v) {
			return normalizeGitHub(v)
		}
	}

	// Priority 3: home_page field
	if isGitHubURL(info.HomePage) {
		return normalizeGitHub(info.HomePage)
	}

	return ""
}

// isGitHubURL checks if a URL points to github.com.
func isGitHubURL(u string) bool {
	u = strings.ToLower(strings.TrimSpace(u))
	return strings.Contains(u, "github.com/")
}

// normalizeGitHub trims a GitHub URL to https://github.com/owner/repo form.
func normalizeGitHub(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Strip common prefixes
	for _, prefix := range []string{"git+", "git://"} {
		raw = strings.TrimPrefix(raw, prefix)
	}
	if !strings.HasPrefix(raw, "http") {
		raw = "https://" + raw
	}

	// Extract github.com/owner/repo, dropping deeper paths
	idx := strings.Index(strings.ToLower(raw), "github.com/")
	if idx < 0 {
		return ""
	}
	path := raw[idx+len("github.com/"):]
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return "https://github.com/" + parts[0] + "/" + parts[1]
}
