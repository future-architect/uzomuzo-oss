// Package scorecard provides a client for the scorecard.dev REST API.
//
// DDD Layer: Infrastructure
// Responsibilities: HTTP I/O, JSON deserialization, batch orchestration.
// No business logic — returns domain ScoreEntity values for consumption
// by the integration layer.
package scorecard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// Client communicates with the scorecard.dev REST API.
type Client struct {
	http           *httpclient.Client
	baseURL        string
	maxConcurrency int
}

// NewClient creates a scorecard.dev API client from configuration.
func NewClient(cfg *config.ScorecardConfig) *Client {
	if cfg == nil {
		cfg = &config.DefaultValues.Scorecard
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = "https://api.scorecard.dev"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 2
	}
	maxConc := cfg.MaxConcurrency
	if maxConc <= 0 {
		maxConc = 10
	}
	httpClient := httpclient.NewClient(
		&http.Client{Timeout: timeout},
		httpclient.RetryConfig{
			MaxRetries:        maxRetries,
			BaseBackoff:       1 * time.Second,
			MaxBackoff:        30 * time.Second,
			RetryOn5xx:        true,
			RetryOnNetworkErr: true,
		},
	)
	return &Client{
		http:           httpClient,
		baseURL:        base,
		maxConcurrency: maxConc,
	}
}

// NewClientWith creates a client with explicit HTTP client and base URL (for tests).
func NewClientWith(httpClient *httpclient.Client, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.scorecard.dev"
	}
	return &Client{
		http:           httpClient,
		baseURL:        strings.TrimRight(baseURL, "/"),
		maxConcurrency: 10,
	}
}

// ScorecardResult holds converted domain data from a single scorecard.dev response.
type ScorecardResult struct {
	OverallScore float64
	Scores       map[string]*domain.ScoreEntity
}

// FetchScorecard fetches scorecard data for a single repository key (e.g. "github.com/owner/repo").
func (c *Client) FetchScorecard(ctx context.Context, repoKey string) (*ScorecardResult, error) {
	if c == nil {
		return nil, fmt.Errorf("nil scorecard client")
	}
	// Strip scheme (case-insensitive per RFC 3986) to normalize input.
	lower := strings.ToLower(repoKey)
	if strings.HasPrefix(lower, "https://") {
		repoKey = repoKey[len("https://"):]
	} else if strings.HasPrefix(lower, "http://") {
		repoKey = repoKey[len("http://"):]
	}
	u, err := url.JoinPath(c.baseURL, "projects", repoKey)
	if err != nil {
		return nil, fmt.Errorf("failed to construct scorecard URL for %s: %w", repoKey, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build scorecard request: %w", err)
	}
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("scorecard request failed for %s: %w", repoKey, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // project not scanned by OpenSSF — no data
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("scorecard API returned %d for %s: %s", resp.StatusCode, repoKey, string(body))
	}
	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode scorecard response for %s: %w", repoKey, err)
	}
	return convertToResult(&apiResp), nil
}

// FetchScorecardBatch fetches scorecard data for multiple repository keys concurrently.
// Returns results keyed by the input repoKey. Missing or failed entries are omitted.
func (c *Client) FetchScorecardBatch(ctx context.Context, repoKeys []string) map[string]*ScorecardResult {
	if c == nil || len(repoKeys) == 0 {
		return nil
	}

	results := make(map[string]*ScorecardResult, len(repoKeys))
	var mu sync.Mutex

	sem := make(chan struct{}, c.maxConcurrency)
	var wg sync.WaitGroup
	for _, key := range repoKeys {
		wg.Add(1)
		go func(rk string) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}
			result, err := c.FetchScorecard(ctx, rk)
			if err != nil {
				slog.Debug("scorecard_fetch_failed", "repo_key", rk, "error", err)
				return
			}
			if result == nil {
				return
			}
			mu.Lock()
			results[rk] = result
			mu.Unlock()
		}(key)
	}
	wg.Wait()
	if len(results) == 0 {
		return nil
	}
	return results
}

// convertToResult converts an API response to a domain ScorecardResult.
func convertToResult(resp *apiResponse) *ScorecardResult {
	if resp == nil {
		return nil
	}
	scores := make(map[string]*domain.ScoreEntity, len(resp.Checks))
	for _, check := range resp.Checks {
		scores[check.Name] = domain.NewScoreEntity(check.Name, check.Score, 10, check.Reason)
	}
	return &ScorecardResult{
		OverallScore: resp.Score,
		Scores:       scores,
	}
}
