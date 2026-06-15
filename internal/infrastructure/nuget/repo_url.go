package nuget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common"
)

// githubURLPattern matches a GitHub URL embedded in scraped HTML. Compiled once
// at package scope so the per-package HTML-scrape fallback does not recompile it.
var githubURLPattern = regexp.MustCompile(`https?://github\.com/[^"'\s<>]+`)

// GetRepoURL attempts to extract a repository URL (preferably GitHub) for the given NuGet package ID.
//
// DDD Layer: Infrastructure
// Behavior:
//   - Queries the NuGet Registration index like GetDeprecation
//   - Scans embedded page leaves for an embedded catalogEntry and extracts repository/project URL fields
//   - If needed, fetches registration pages to inspect embedded catalogEntry entries
//   - Returns a normalized URL or an empty string when not determinable
func (c *Client) GetRepoURL(ctx context.Context, packageID string, _ string) (string, error) {
	id := strings.TrimSpace(packageID)
	if id == "" {
		return "", fmt.Errorf("package id is required")
	}

	idLower := strings.ToLower(id)
	candidates := c.getRegistrationCandidates(ctx)

	for idx, b := range candidates {
		endpoint := fmt.Sprintf("%s/%s/index.json", b, url.PathEscape(idLower))
		slog.Debug("nuget: request registration index (repo)", "id", id, "endpoint", endpoint, "attempt", idx+1)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return "", fmt.Errorf("failed to build NuGet request: %w", err)
		}
		req.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
		resp, err := c.http.Do(ctx, req)
		if err != nil {
			return "", fmt.Errorf("NuGet HTTP error: %w", err)
		}
		if resp.StatusCode == http.StatusNotFound {
			_ = resp.Body.Close() // best-effort cleanup
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close() // best-effort cleanup
			return "", fmt.Errorf("NuGet HTTP %d", resp.StatusCode)
		}
		var reg registrationIndex
		if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
			_ = resp.Body.Close() // best-effort cleanup
			return "", fmt.Errorf("NuGet decode failed: %w", err)
		}
		_ = resp.Body.Close() // best-effort cleanup

		// 1) Inspect embedded leaves for catalogEntry object
		for _, page := range reg.Items {
			if len(page.Items) > 0 {
				if repo := extractRepoURLFromLeaves(page.Items); repo != "" {
					if resolved := c.resolveRepoURLHeuristics(ctx, repo); resolved != "" {
						return resolved, nil
					}
					return repo, nil
				}
				continue
			}
			if page.ID == "" {
				continue
			}
			// 2) Fetch page document and inspect its items
			piReq, err := http.NewRequestWithContext(ctx, http.MethodGet, page.ID, nil)
			if err != nil {
				return "", fmt.Errorf("NuGet request (page) failed: %w", err)
			}
			piReq.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
			piResp, err := c.http.Do(ctx, piReq)
			if err != nil {
				return "", fmt.Errorf("NuGet HTTP (page) error: %w", err)
			}
			if piResp.StatusCode != http.StatusOK {
				_ = piResp.Body.Close() // best-effort cleanup
				continue
			}
			var leaf registrationPage
			if err := json.NewDecoder(piResp.Body).Decode(&leaf); err != nil {
				_ = piResp.Body.Close() // best-effort cleanup
				return "", fmt.Errorf("NuGet page decode failed: %w", err)
			}
			_ = piResp.Body.Close() // best-effort cleanup
			if repo := extractRepoURLFromLeaves(leaf.Items); repo != "" {
				if resolved := c.resolveRepoURLHeuristics(ctx, repo); resolved != "" {
					return resolved, nil
				}
				return repo, nil
			}
		}
	}
	return "", nil
}

// extractRepoURLFromLeaves inspects registration leaves for an embedded catalogEntry object
// and attempts to extract a repository or project URL from it. Returns a normalized URL or empty string.
func extractRepoURLFromLeaves(items []registrationLeaf) string {
	for _, it := range items {
		if len(it.CatalogEntry) == 0 || it.CatalogEntry[0] != '{' {
			continue
		}
		// Use a permissive map to handle variations: repository (object or string), projectUrl, repositoryUrl
		var m map[string]any
		if err := json.Unmarshal(it.CatalogEntry, &m); err != nil {
			continue
		}
		// Try repository (object with url)
		if v, ok := m["repository"]; ok {
			switch rv := v.(type) {
			case map[string]any:
				if u, ok := rv["url"].(string); ok && strings.TrimSpace(u) != "" {
					if norm := normalizeRepoURL(u); norm != "" {
						return norm
					}
				}
			case string:
				if norm := normalizeRepoURL(rv); norm != "" {
					return norm
				}
			}
		}
		// Try repositoryUrl (string)
		if u, ok := m["repositoryUrl"].(string); ok && strings.TrimSpace(u) != "" {
			if norm := normalizeRepoURL(u); norm != "" {
				return norm
			}
		}
		// Fallback to projectUrl (string)
		if u, ok := m["projectUrl"].(string); ok && strings.TrimSpace(u) != "" {
			if norm := normalizeRepoURL(u); norm != "" {
				return norm
			}
		}
	}
	return ""
}

// normalizeRepoURL trims and returns the URL as-is; the consumer will further normalize
// to GitHub project keys if needed. Keep it minimal here.
func normalizeRepoURL(s string) string {
	return strings.TrimSpace(s)
}

// resolveRepoURLHeuristics attempts to improve a non-GitHub repository URL returned by NuGet metadata.
// For Microsoft packages, NuGet often provides go.microsoft.com/aka.ms short links or docs pages.
// This function will try to follow redirects and, if landing on a docs page, scrape a GitHub repo URL.
func (c *Client) resolveRepoURLHeuristics(ctx context.Context, raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())

	// Only attempt network heuristics for Microsoft shorteners/docs
	if host == "aka.ms" || strings.HasSuffix(host, ".microsoft.com") {
		if final := c.followRedirect(ctx, s); final != "" {
			fu, _ := url.Parse(final)
			if fu != nil {
				if strings.Contains(strings.ToLower(fu.Hostname()), "github.com") {
					// Normalize and validate before returning
					if norm := common.NormalizeRepositoryURL(final); norm != "" && common.IsValidGitHubURL(norm) {
						return norm
					}
					if common.IsValidGitHubURL(final) {
						return final
					}
				}
				// If we landed on a docs page, attempt to find a GitHub link within the HTML
				if strings.Contains(strings.ToLower(fu.Hostname()), "docs.microsoft.com") || strings.Contains(strings.ToLower(fu.Hostname()), "learn.microsoft.com") {
					if gh := c.scrapeFirstGitHubFromHTML(ctx, final); gh != "" {
						return gh
					}
				}
			}
		}
	}
	return ""
}

// followRedirect performs a GET request and returns the final URL after redirects.
func (c *Client) followRedirect(ctx context.Context, startURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, startURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain small body to allow re-use
	_, _ = io.CopyN(io.Discard, resp.Body, 1024) // best-effort drain before close
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String()
	}
	return ""
}

// scrapeFirstGitHubFromHTML fetches the page and returns the first GitHub repository URL found in the HTML.
func (c *Client) scrapeFirstGitHubFromHTML(ctx context.Context, pageURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return ""
	}
	// Read up to 512KB to avoid large downloads
	const maxRead = 512 * 1024
	limited := io.LimitReader(resp.Body, maxRead)
	body, err := io.ReadAll(limited)
	if err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	// Find a GitHub URL using the package-scope compiled pattern.
	if m := githubURLPattern.Find(body); len(m) > 0 {
		// Normalize candidate URL and validate it's a GitHub repo URL
		candidate := string(m)
		normalized := common.NormalizeRepositoryURL(candidate)
		if normalized != "" && common.IsValidGitHubURL(normalized) {
			return normalized
		}
		// Fallback to raw candidate if normalization empties, but still looks valid
		if common.IsValidGitHubURL(candidate) {
			return candidate
		}
	}
	return ""
}
