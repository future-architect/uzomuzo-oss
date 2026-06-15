package depsdev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	neturl "net/url"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common/links"
	commonpurl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

// FetchDependentCount fetches the dependent count for a single versioned PURL from the deps.dev GetDependents API.
// The PURL must include a version (e.g., pkg:npm/lodash@4.17.21). Versionless PURLs return nil.
// Supported systems: npm, maven, pypi, cargo (Go, NuGet, RubyGems are NOT supported by this endpoint).
// Returns nil when the endpoint returns 404 (unsupported or unknown package version).
// See: https://docs.deps.dev/api/v3alpha/#getdependents
//
// DDD Layer: Infrastructure
// Endpoint: GET /v3alpha/systems/{system}/packages/{name}/versions/{version}:dependents
func (c *DepsDevClient) FetchDependentCount(ctx context.Context, purlStr string) (*DependentsResponse, error) {
	parser := commonpurl.NewParser()
	parsed, err := parser.Parse(purlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PURL for dependents: %w", err)
	}

	version := strings.TrimSpace(parsed.Version())
	if version == "" {
		slog.Debug("dependents: skipping versionless PURL", "purl", purlStr)
		return nil, nil
	}
	// Go modules may have "+incompatible" suffix that deps.dev doesn't recognize in the version path.
	if strings.ToLower(parsed.Ecosystem()) == "golang" {
		if idx := strings.Index(version, "+"); idx >= 0 {
			version = version[:idx]
		}
	}

	system, name, err := toDepsDevSystemAndName(parsed)
	if err != nil {
		if errors.Is(err, links.ErrUnsupportedEcosystem) {
			slog.Debug("dependents: skipping unsupported ecosystem", "purl", purlStr, "error", err)
			return nil, nil
		}
		return nil, fmt.Errorf("dependents: normalize PURL: %w", err)
	}
	escapedVersion := neturl.PathEscape(version)
	endpoint := fmt.Sprintf("%s/systems/%s/packages/%s/versions/%s:dependents", c.baseURL, system, name, escapedVersion)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create dependents request (url=%s): %w", endpoint, err)
	}
	req.Header.Set("User-Agent", "uzomuzo-depsdev-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")

	resp, err := c.client.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("dependents HTTP request failed (url=%s): %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		slog.Debug("dependents: 404 not found", "purl", purlStr, "url", endpoint)
		return nil, nil // unsupported system or unknown package version
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		snippet := truncateString(string(body), 1024)
		return nil, fmt.Errorf("dependents HTTP %d (url=%s): %s", resp.StatusCode, endpoint, snippet)
	}

	var result DependentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("dependents JSON decode failed (url=%s): %w", endpoint, err)
	}
	return &result, nil
}

// FetchDependentCountBatch fetches dependent counts for multiple PURLs in parallel.
// Returns a map of canonical (versionless) PURL -> DependentsResponse.
// PURLs that fail or return 404 are silently omitted from the result.
//
// DDD Layer: Infrastructure (parallel processing)
func (c *DepsDevClient) FetchDependentCountBatch(ctx context.Context, purls []string) map[string]*DependentsResponse {
	if len(purls) == 0 {
		return make(map[string]*DependentsResponse)
	}

	const maxWorkers = 10
	results := collectBounded[*DependentsResponse](ctx, purls, maxWorkers, func(ctx context.Context, purl string) (string, *DependentsResponse, bool) {
		resp, err := c.FetchDependentCount(ctx, purl)
		if err != nil {
			slog.Debug("Failed to fetch dependent count", "purl", purl, "error", err)
			return "", nil, false
		}
		if resp == nil {
			return "", nil, false
		}
		// Normalize to versionless canonical key for map consistency
		key := commonpurl.CanonicalKey(purl)
		if key == "" {
			key = purl
		}
		return key, resp, true
	})

	slog.Debug("Dependent count batch completed", "requested", len(purls), "successful", len(results))
	return results
}
