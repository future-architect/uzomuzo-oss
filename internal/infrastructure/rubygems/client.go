package rubygems

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// Client is a minimal RubyGems API client for metadata lookups.
// It is intentionally small and only implements the fields we actually need.
//
// DDD Layer: Infrastructure
// Responsibility: Resolve repository/homepage URLs for a gem via RubyGems API.
//
// Authoritative docs and endpoints:
//   - API v1 gem (latest): GET https://rubygems.org/api/v1/gems/<name>.json
//     Fields: source_code_uri, homepage_uri, project_uri
//     Docs:   https://guides.rubygems.org/rubygems-org-api/#gem-methods
//   - API v2 gem version: GET https://rubygems.org/api/v2/rubygems/<name>/versions/<version>.json
//     Fields: source_code_uri, homepage_uri, project_uri
//     Docs:   https://guides.rubygems.org/rubygems-org-api/#versions-methods
//
// Concrete examples (gem names are typically lowercase in API paths):
//   - v1 (latest metadata):
//     https://rubygems.org/api/v1/gems/rails.json
//   - v2 (specific version metadata):
//     https://rubygems.org/api/v2/rubygems/rails/versions/5.0.0.json
//     Note: If the specified version does not exist, RubyGems returns HTTP 404; this client treats it as ("", nil).
//
// Resolution policy implemented here (in order):
//  1. Prefer version-specific v2 endpoint when a version is provided
//  2. Fallback to v1 endpoint (latest metadata)
//  3. Prefer source_code_uri, then homepage_uri, then project_uri
//     - Only GitHub URLs are retained after normalization via common.NormalizeRepositoryURL.
type Client struct {
	baseURL string
	http    *httpclient.Client
}

// NewClient creates a RubyGems client with sane defaults.
// Timeout defaults to 3 seconds.
func NewClient() *Client {
	return &Client{
		baseURL: "https://rubygems.org",
		http:    httpclient.NewClient(&http.Client{Timeout: 3 * time.Second}, httpclient.RetryConfig{MaxRetries: 2, BaseBackoff: 400 * time.Millisecond, MaxBackoff: 2 * time.Second, RetryOn5xx: true, RetryOnNetworkErr: true}),
	}
}

// SetBaseURL overrides the API base URL (for testing).
func (c *Client) SetBaseURL(u string) { c.baseURL = u }

// SetHTTPClient allows tests to inject a custom HTTP client.
func (c *Client) SetHTTPClient(h *http.Client) {
	c.http = httpclient.NewClient(h, httpclient.DefaultRetryConfig())
}

// GetRepoURL attempts to resolve a repository URL for a gem name@version.
// Priority:
//  1. source_code_uri (must normalize to a valid GitHub URL)
//  2. homepage_uri (same validation/normalization)
//
// Returns an empty string if nothing usable is found.
func (c *Client) GetRepoURL(ctx context.Context, name, version string) (string, error) {
	if name == "" {
		return "", nil
	}

	// Prefer version-specific metadata (v2). If version is empty, fall back to v1 (latest).
	if version != "" {
		if url, err := c.fetchRepoURLV2(ctx, name, version); err != nil {
			// network/HTTP errors are surfaced; 404-like empty responses return ("", nil)
			return "", fmt.Errorf("rubygems v2 lookup failed: %w", err)
		} else if url != "" {
			return url, nil
		}
	}

	// Try latest (v1)
	if url, err := c.fetchRepoURLV1(ctx, name); err != nil {
		return "", fmt.Errorf("rubygems v1 lookup failed: %w", err)
	} else {
		return url, nil
	}
}

type v1Gem struct {
	SourceCodeURI string `json:"source_code_uri"`
	HomepageURI   string `json:"homepage_uri"`
	ProjectURI    string `json:"project_uri"`
}

func (c *Client) fetchRepoURLV1(ctx context.Context, name string) (string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/gems/%s.json", c.baseURL, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "uzomuzo-rubygems-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var g v1Gem
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		return "", err
	}
	return pickGitRepo(g.SourceCodeURI, g.HomepageURI, g.ProjectURI), nil
}

type v2GemVersion struct {
	SourceCodeURI string `json:"source_code_uri"`
	HomepageURI   string `json:"homepage_uri"`
	ProjectURI    string `json:"project_uri"`
}

func (c *Client) fetchRepoURLV2(ctx context.Context, name, version string) (string, error) {
	endpoint := fmt.Sprintf("%s/api/v2/rubygems/%s/versions/%s.json", c.baseURL, name, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "uzomuzo-rubygems-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var g v2GemVersion
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		return "", err
	}
	return pickGitRepo(g.SourceCodeURI, g.HomepageURI, g.ProjectURI), nil
}

// pickGitRepo normalizes and validates potential repository URLs, preferring source_code_uri.
func pickGitRepo(source, homepage, project string) string {
	for _, cand := range []string{source, homepage, project} {
		if cand == "" {
			continue
		}
		// Normalize and keep only GitHub URLs for now
		norm := common.NormalizeRepositoryURL(cand)
		if common.IsValidGitHubURL(norm) {
			return norm
		}
	}
	return ""
}

// GetReverseDependencyCount fetches the number of reverse dependencies for a gem.
// It calls the RubyGems reverse_dependencies API and returns the count.
//
// DDD Layer: Infrastructure
// Endpoint: GET /api/v1/gems/{name}/reverse_dependencies.json
// Docs: https://guides.rubygems.org/rubygems-org-api/#gem-methods
//
// Returns 0, nil when the gem is not found (404).
func (c *Client) GetReverseDependencyCount(ctx context.Context, name string) (int, error) {
	if name == "" {
		return 0, nil
	}
	endpoint := fmt.Sprintf("%s/api/v1/gems/%s/reverse_dependencies.json", c.baseURL, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create reverse_dependencies request: %w", err)
	}
	req.Header.Set("User-Agent", "uzomuzo-rubygems-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("reverse_dependencies HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return 0, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("reverse_dependencies HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var deps []string
	if err := json.NewDecoder(resp.Body).Decode(&deps); err != nil {
		return 0, fmt.Errorf("reverse_dependencies JSON decode failed: %w", err)
	}
	return len(deps), nil
}
