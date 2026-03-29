package goproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client provides minimal access to the public Go module proxy for metadata.
//
// DDD Layer: Infrastructure
// Responsibilities: HTTP I/O, parsing lightweight JSON/text responses.
// No business logic (decisions) here.
type Client struct {
	http *http.Client
	base string
}

// NewClient constructs a Client with sane defaults.
func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 10 * time.Second},
		base: "https://proxy.golang.org",
	}
}

// NewClientWith constructs a Client with custom http client and base URL (used in tests).
func NewClientWith(httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if baseURL == "" {
		baseURL = "https://proxy.golang.org"
	}
	return &Client{http: httpClient, base: baseURL}
}

// LatestVersion returns the latest version known to the proxy for a module.
// Reference is the URL used for the query.
func (c *Client) LatestVersion(ctx context.Context, module string) (version string, reference string, err error) {
	if c == nil {
		return "", "", fmt.Errorf("nil goproxy client")
	}
	// Module paths are URL-escaped path segments per the proxy contract.
	modEscaped := pathEscapeModule(module)
	u := c.base + "/" + modEscaped + "/@latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", u, fmt.Errorf("build latest request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", u, fmt.Errorf("latest request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", u, fmt.Errorf("latest request status %d", resp.StatusCode)
	}
	var payload struct {
		Version string    `json:"Version"`
		Time    time.Time `json:"Time"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", u, fmt.Errorf("decode latest json: %w", err)
	}
	return payload.Version, u, nil
}

// GoMod fetches the go.mod content for a specific module@version.
// Returns the raw bytes and the reference URL.
func (c *Client) GoMod(ctx context.Context, module, version string) (mod []byte, reference string, err error) {
	if c == nil {
		return nil, "", fmt.Errorf("nil goproxy client")
	}
	modEscaped := pathEscapeModule(module)
	verEscaped := url.PathEscape(version)
	u := c.base + "/" + modEscaped + "/@v/" + verEscaped + ".mod"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, u, fmt.Errorf("build mod request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, u, fmt.Errorf("mod request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, u, fmt.Errorf("mod request status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, u, fmt.Errorf("read mod body: %w", err)
	}
	return b, u, nil
}

// ResolveModuleRoot attempts to find a valid Go module by walking up parent paths.
//
// Given a possibly non-module package path (e.g., "github.com/org/repo/pkg/sub"), this method tries
// the full path first and then repeatedly trims the last path segment until a module is found
// (LatestVersion succeeds). Returns the discovered module path and its latest version.
//
// Example tries in order:
//
//	github.com/org/repo/pkg/sub
//	github.com/org/repo/pkg
//	github.com/org/repo
//
// If no module is found, returns an error.
func (c *Client) ResolveModuleRoot(ctx context.Context, path string) (module string, latest string, err error) {
	if c == nil {
		return "", "", fmt.Errorf("nil goproxy client")
	}
	p := strings.TrimSuffix(path, "/")
	for p != "" && p != "/" {
		if v, _, e := c.LatestVersion(ctx, p); e == nil && v != "" {
			return p, v, nil
		}
		idx := strings.LastIndexByte(p, '/')
		if idx < 0 {
			break
		}
		p = p[:idx]
	}
	return "", "", fmt.Errorf("no module found for path %q", path)
}

// pathEscapeModule escapes a module path for insertion into a proxy URL path.
func pathEscapeModule(module string) string {
	// Each path segment must be path-escaped. Module paths use '/' separators.
	if module == "" {
		return ""
	}
	segs := strings.Split(module, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}
