package depsdev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"

	commonpurl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

// FetchDependencies fetches the dependency graph for a single versioned PURL.
// Returns nil, nil for unsupported ecosystems or 404 responses.
//
// DDD Layer: Infrastructure (external API call)
func (c *DepsDevClient) FetchDependencies(ctx context.Context, purlStr string) (*DependenciesResponse, error) {
	parser := commonpurl.NewParser()
	parsed, err := parser.Parse(purlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PURL for dependencies: %w", err)
	}

	version := strings.TrimSpace(parsed.Version())
	if version == "" {
		slog.Debug("dependencies: skipping versionless PURL", "purl", purlStr)
		return nil, nil
	}
	// Go modules may have "+incompatible" suffix that deps.dev doesn't recognize in the version path.
	if strings.ToLower(parsed.GetEcosystem()) == "golang" {
		if idx := strings.Index(version, "+"); idx >= 0 {
			version = version[:idx]
		}
	}

	system, name := toDepsDevSystemAndName(parsed)
	escapedVersion := neturl.PathEscape(version)
	endpoint := fmt.Sprintf("%s/systems/%s/packages/%s/versions/%s:dependencies", c.baseURL, system, name, escapedVersion)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create dependencies request (url=%s): %w", endpoint, err)
	}
	req.Header.Set("User-Agent", "uzomuzo-depsdev-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")

	resp, err := c.client.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("dependencies HTTP request failed (url=%s): %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		slog.Debug("dependencies: 404 not found", "purl", purlStr, "url", endpoint)
		return nil, nil // unsupported system or unknown package version
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		snippet := truncateString(string(body), 1024)
		return nil, fmt.Errorf("dependencies HTTP %d (url=%s): %s", resp.StatusCode, endpoint, snippet)
	}

	var result DependenciesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("dependencies JSON decode failed (url=%s): %w", endpoint, err)
	}
	return &result, nil
}

// FetchDependenciesBatch fetches dependency graphs for multiple PURLs in parallel.
// Returns a map of canonical (versionless) PURL -> DependenciesResponse.
// PURLs that fail or return 404 are silently omitted from the result.
//
// DDD Layer: Infrastructure (parallel processing)
func (c *DepsDevClient) FetchDependenciesBatch(ctx context.Context, purls []string) map[string]*DependenciesResponse {
	if len(purls) == 0 {
		return make(map[string]*DependenciesResponse)
	}

	const maxWorkers = 10
	results := make(map[string]*DependenciesResponse)
	var mu sync.Mutex
	semaphore := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, p := range purls {
		wg.Add(1)
		go func(purl string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			resp, err := c.FetchDependencies(ctx, purl)
			if err != nil {
				slog.Debug("Failed to fetch dependencies", "purl", purl, "error", err)
				return
			}
			if resp == nil {
				return
			}

			// Normalize to versionless canonical key for map consistency
			key := commonpurl.CanonicalKey(purl)
			if key == "" {
				key = purl
			}

			mu.Lock()
			results[key] = resp
			mu.Unlock()
		}(p)
	}

	wg.Wait()
	slog.Debug("Dependencies batch completed", "requested", len(purls), "successful", len(results))
	return results
}
