package npmjs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo/internal/common"
	"github.com/future-architect/uzomuzo/internal/infrastructure/httpclient"
)

// Client is a minimal npm registry client used to resolve repository URLs.
//
// DDD Layer: Infrastructure
// Responsibility: External HTTP call to registry.npmjs.org to fetch package metadata.
//
// Authoritative docs and behavior references:
//   - npm package metadata endpoint (GET): https://registry.npmjs.org/<package>
//     Shape: top-level "versions" object keyed by version, "dist-tags" (e.g., latest), and metadata fields
//     Reference (metadata schema examples):
//     https://github.com/npm/registry/blob/master/docs/responses/package-metadata.md
//   - package.json fields used for repository inference:
//     repository: https://docs.npmjs.com/cli/v10/configuring-npm/package-json#repository
//     homepage:   https://docs.npmjs.com/cli/v10/configuring-npm/package-json#homepage
//     bugs:       https://docs.npmjs.com/cli/v10/configuring-npm/package-json#bugs
//
// Concrete examples:
//   - Unscoped package (lodash):
//     Endpoint: https://registry.npmjs.org/lodash
//     Possible repository field in versions[x].repository:
//     { "type": "git", "url": "git+https://github.com/lodash/lodash.git" }
//     Normalization keeps: https://github.com/lodash/lodash
//   - Scoped package (@types/node):
//     Endpoint (URL-encoded): https://registry.npmjs.org/%40types%2Fnode
//     Often publishes repository/homepage/bugs at top-level metadata (not only per-version).
//     This client first checks top-level repository/homepage/bugs, then version-specific data.
//
// Resolution policy implemented here (in order):
//  1. Top-level metadata: repository.url -> homepage -> bugs.url (some packages publish repo only at top-level)
//  2. Requested version's metadata (if a version is provided): repository -> homepage -> bugs.url
//  3. dist-tags.latest version's metadata (repository -> homepage -> bugs.url)
//  4. Any version's metadata (first hit) as a last resort
//     - Only GitHub URLs are retained after normalization via common.NormalizeRepositoryURL.
type Client struct {
	baseURL string
	http    *httpclient.Client
}

// NewClient creates a new npmjs Client with sane defaults.
func NewClient() *Client {
	return &Client{
		baseURL: "https://registry.npmjs.org",
		http:    httpclient.NewClient(&http.Client{Timeout: 3 * time.Second}, httpclient.RetryConfig{MaxRetries: 2, BaseBackoff: 400 * time.Millisecond, MaxBackoff: 2 * time.Second, RetryOn5xx: true, RetryOnNetworkErr: true}),
	}
}

// SetHTTPClient overrides the underlying HTTP client (useful for tests).
func (c *Client) SetHTTPClient(h *http.Client) {
	c.http = httpclient.NewClient(h, httpclient.DefaultRetryConfig())
}

// GetRepoURL attempts to resolve a GitHub repository URL from npm registry metadata.
// It prefers version-specific repository/homepage/bugs fields when a version is provided,
// then falls back to top-level metadata.
// Returns an empty string when nothing usable is found or the URL is not a GitHub repo.
func (c *Client) GetRepoURL(ctx context.Context, namespace, name, version string) (string, error) {
	pkg := strings.TrimSpace(name)
	ns := strings.TrimSpace(namespace)
	if pkg == "" {
		return "", nil
	}
	if ns != "" && !strings.HasPrefix(ns, "@") {
		ns = "@" + ns
	}
	full := pkg
	if ns != "" {
		full = ns + "/" + pkg
	}
	endpoint := fmt.Sprintf("%s/%s", c.baseURL, url.PathEscape(full))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("npm request build failed: %w", err)
	}
	slog.Debug("npmjs request", "endpoint", endpoint, "namespace", ns, "name", pkg, "version", version)
	req.Header.Set("User-Agent", "uzomuzo-npmjs-client/1.0 (+https://github.com/future-architect/uzomuzo)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return "", fmt.Errorf("npm http failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm HTTP %d", resp.StatusCode)
	}

	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", fmt.Errorf("npm decode failed: %w", err)
	}

	// Try top-level fields first (common for meta packages like @types/*)
	if u := extractRepoHomeBugs(doc); u != "" {
		slog.Debug("npmjs resolved repo (top-level)", "url", u)
		return u, nil
	}
	// Fallback to version-specific fields: preferred version, then latest tag, then any version
	if u := repoFromVersion(doc, strings.TrimSpace(version)); u != "" {
		slog.Debug("npmjs resolved repo (version-specific: requested)", "url", u)
		return u, nil
	}
	if u := repoFromLatestTag(doc); u != "" {
		slog.Debug("npmjs resolved repo (version-specific: dist-tags.latest)", "url", u)
		return u, nil
	}
	if u := repoFromAnyVersion(doc); u != "" {
		slog.Debug("npmjs resolved repo (version-specific: any)", "url", u)
		return u, nil
	}
	return "", nil
}

func repoFromVersion(doc map[string]any, ver string) string {
	if ver == "" {
		return ""
	}
	versions, _ := doc["versions"].(map[string]any)
	if versions == nil {
		return ""
	}
	vdoc, _ := versions[ver].(map[string]any)
	if vdoc == nil {
		return ""
	}
	// repository
	if u := extractRepoURL(vdoc["repository"]); u != "" {
		return u
	}
	// homepage / bugs.url
	if u := extractHomeBugs(vdoc); u != "" {
		return u
	}
	return ""
}

func repoFromLatestTag(doc map[string]any) string {
	distTags, _ := doc["dist-tags"].(map[string]any)
	if distTags == nil {
		return ""
	}
	latest, _ := distTags["latest"].(string)
	if latest == "" {
		return ""
	}
	return repoFromVersion(doc, latest)
}

func repoFromAnyVersion(doc map[string]any) string {
	versions, _ := doc["versions"].(map[string]any)
	if versions == nil {
		return ""
	}
	for _, v := range versions {
		vdoc, _ := v.(map[string]any)
		if vdoc == nil {
			continue
		}
		if u := extractRepoURL(vdoc["repository"]); u != "" {
			return u
		}
		if u := extractHomeBugs(vdoc); u != "" {
			return u
		}
	}
	return ""
}

func extractRepoHomeBugs(doc map[string]any) string {
	if u := extractRepoURL(doc["repository"]); u != "" {
		return u
	}
	if u := extractHomeBugs(doc); u != "" {
		return u
	}
	return ""
}

func extractRepoURL(v any) string {
	switch t := v.(type) {
	case string:
		return normalizeGitHub(t)
	case map[string]any:
		if u, _ := t["url"].(string); u != "" {
			return normalizeGitHub(u)
		}
	}
	return ""
}

func extractHomeBugs(doc map[string]any) string {
	if hp, _ := doc["homepage"].(string); hp != "" {
		if u := normalizeGitHub(hp); u != "" {
			return u
		}
	}
	if bugs, _ := doc["bugs"].(map[string]any); bugs != nil {
		if u, _ := bugs["url"].(string); u != "" {
			if n := normalizeGitHub(u); n != "" {
				return n
			}
		}
	}
	return ""
}

func normalizeGitHub(raw string) string {
	norm := common.NormalizeRepositoryURL(raw)
	if norm == "" || !common.IsValidGitHubURL(norm) {
		return ""
	}
	return norm
}

// DeprecationInfo holds npm registry deprecation/unpublished signals for a package version.
type DeprecationInfo struct {
	Deprecated  bool   // true if the version is marked deprecated
	Message     string // deprecated message (if any)
	Unpublished bool   // true if the version is unpublished
	Successor   string // extracted successor package name (if any)
}

// GetDeprecation queries npm registry for the given package and version.
// Returns info, found=true if package exists, else found=false.
func (c *Client) GetDeprecation(ctx context.Context, namespace, name, version string) (*DeprecationInfo, bool, error) {
	pkg := strings.TrimSpace(name)
	ns := strings.TrimSpace(namespace)
	if pkg == "" {
		return nil, false, nil
	}
	if ns != "" && !strings.HasPrefix(ns, "@") {
		ns = "@" + ns
	}
	full := pkg
	if ns != "" {
		full = ns + "/" + pkg
	}
	endpoint := fmt.Sprintf("%s/%s", c.baseURL, url.PathEscape(full))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, fmt.Errorf("npm request build failed: %w", err)
	}
	req.Header.Set("User-Agent", "uzomuzo-npmjs-client/1.0 (+https://github.com/future-architect/uzomuzo)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, false, fmt.Errorf("npm http failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("npm HTTP %d", resp.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, false, fmt.Errorf("npm decode failed: %w", err)
	}
	info := &DeprecationInfo{}
	// Check for unpublished
	if t, ok := doc["time"].(map[string]any); ok {
		if _, ok := t["unpublished"]; ok {
			info.Unpublished = true
		}
	}
	// Check for deprecated on the version
	if vmap, ok := doc["versions"].(map[string]any); ok {
		if v, ok := vmap[version].(map[string]any); ok {
			if msg, ok := v["deprecated"].(string); ok && msg != "" {
				info.Deprecated = true
				info.Message = msg
				if succ := extractNpmSuccessor(msg); succ != "" {
					// Self-reference suppression: compare normalized tokens ignoring case.
					fullName := pkg
					if ns != "" {
						fullName = ns + "/" + pkg
					}
					if !strings.EqualFold(succ, pkg) && !strings.EqualFold(succ, fullName) {
						info.Successor = succ
					}
				}
			}
		}
	}
	return info, true, nil
}

// extractNpmSuccessor tries to extract a successor package name from a deprecated message.
//
// Intentional limitations:
//   - We intentionally match only high-confidence phrases ("use ", "moved to ", "replaced by ")
//     to avoid false positives.
//   - Broader phrases like "migrate to" are NOT matched on purpose because they often point to
//     non-package targets (e.g., "migrate to ESM", "migrate to Node 18", "migrate to v3").
//     Allowing them would increase noise and degrade result quality.
//
// If we expand supported phrases in the future, we should validate the extracted token against
// npm package naming rules (optionally scoped "@scope/name") and add unit tests to lock in the
// intended behavior. See successor_test.go for negative cases that document this policy.
var npmPkgNamePattern = regexp.MustCompile(`^(?:@[-a-z0-9_.]+/)?[A-Za-z0-9][A-Za-z0-9._-]*$`)

func extractNpmSuccessor(msg string) string {
	raw := msg
	lower := strings.ToLower(msg)
	phrases := []string{"use ", "moved to ", "replaced by "}
	for _, p := range phrases {
		idx := strings.Index(lower, p)
		if idx >= 0 {
			rest := raw[idx+len(p):]
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				continue
			}
			candidate := strings.Trim(fields[0], ".,;:!?()[]{}")
			if candidate == "" || !npmPkgNamePattern.MatchString(candidate) {
				continue
			}
			return candidate
		}
	}
	return ""
}
