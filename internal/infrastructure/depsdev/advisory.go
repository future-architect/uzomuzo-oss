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
)

// FetchAdvisory fetches advisory detail (title, CVSS3 score) for a single advisory ID.
// Returns nil without error for 404 (withdrawn advisory).
func (c *DepsDevClient) FetchAdvisory(ctx context.Context, advisoryID string) (*AdvisoryDetail, error) {
	// Normalize the ID the same way FetchAdvisoriesBatch does, so direct callers
	// don't pollute the cache with whitespace-padded keys or issue invalid
	// requests for empty IDs.
	advisoryID = strings.TrimSpace(advisoryID)
	if advisoryID == "" {
		return nil, fmt.Errorf("advisory ID is required")
	}

	// Check cache first.
	if cached, ok := c.advisoryCache.Load(advisoryID); ok {
		return cached.(*AdvisoryDetail), nil
	}

	endpoint := fmt.Sprintf("%s/advisories/%s", c.baseURL, neturl.PathEscape(advisoryID))
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create advisory request (id=%s): %w", advisoryID, err)
	}
	req.Header.Set("User-Agent", "uzomuzo-depsdev-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")

	resp, err := c.client.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("advisory HTTP request failed (id=%s): %w", advisoryID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		slog.Debug("advisory: 404 not found", "id", advisoryID)
		var missing *AdvisoryDetail
		c.advisoryCache.Store(advisoryID, missing)
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		snippet := truncateString(string(body), 1024)
		return nil, fmt.Errorf("advisory HTTP %d (id=%s): %s", resp.StatusCode, advisoryID, snippet)
	}

	var detail AdvisoryDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("advisory JSON decode failed (id=%s): %w", advisoryID, err)
	}

	c.advisoryCache.Store(advisoryID, &detail)
	return &detail, nil
}

// FetchAdvisoriesBatch fetches advisory details for multiple advisory IDs in parallel.
// Returns a map of advisory ID -> AdvisoryDetail. Unknown/failed IDs are silently omitted.
// Results are cached in-memory since advisory metadata is immutable.
//
// DDD Layer: Infrastructure (parallel processing)
func (c *DepsDevClient) FetchAdvisoriesBatch(ctx context.Context, advisoryIDs []string) map[string]*AdvisoryDetail {
	if len(advisoryIDs) == 0 {
		return make(map[string]*AdvisoryDetail)
	}

	// Normalize and deduplicate IDs.
	seen := make(map[string]struct{}, len(advisoryIDs))
	unique := make([]string, 0, len(advisoryIDs))
	for _, id := range advisoryIDs {
		trimmedID := strings.TrimSpace(id)
		if trimmedID == "" {
			continue
		}
		if _, exists := seen[trimmedID]; !exists {
			seen[trimmedID] = struct{}{}
			unique = append(unique, trimmedID)
		}
	}

	const maxWorkers = 10
	results := collectBounded[*AdvisoryDetail](ctx, unique, maxWorkers, func(ctx context.Context, advisoryID string) (string, *AdvisoryDetail, bool) {
		detail, err := c.FetchAdvisory(ctx, advisoryID)
		if err != nil {
			slog.Debug("failed to fetch advisory detail", "id", advisoryID, "error", err)
			return "", nil, false
		}
		if detail == nil {
			return "", nil, false
		}
		return advisoryID, detail, true
	})

	slog.Debug("advisory severity fetch complete",
		"requested", len(advisoryIDs),
		"unique", len(unique),
		"fetched", len(results),
	)

	return results
}
