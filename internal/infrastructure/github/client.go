// Package github provides GitHub API client for repository state fetching.
//
// 🚨 GitHub API Rate Limit Strategy:
//
// GitHub API has strict rate limits:
//   - REST API: 5,000 requests/hour (authenticated)
//   - GraphQL API: 5,000 points/hour (authenticated)
//   - Official docs: https://docs.github.com/en/rest/using-the-rest-api/rate-limits
//
// Design Decision:
//   - No retry for rate limit errors (HTTP 429)
//   - Reason: Rate limit exceeded requires 1-hour wait, short retries are ineffective
//   - Alternative: Proactive control to avoid limits
//   - Concurrency limiting (MaxConcurrency)
//   - Request interval control (RequestInterval)
//   - Individual timeouts (TimeoutSeconds)
//
// This approach achieves efficient parallel processing while avoiding rate limits.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// Client implements GitHub API client with parallel processing support
type Client struct {
	token          string
	config         *config.GitHubConfig
	appConfig      *config.Config // Reference to global configuration
	appTimeoutSecs int
	httpClient     *httpclient.Client
	// Aggregated rate limit tracking (thread-safe)
	rateMu             sync.Mutex
	rateLimitTotalCost int
	rateLimitQueries   int
	rateLimitRemaining int
	rateLimitResetAt   string
}

// NewClient creates a new GitHub client
func NewClient(cfg *config.Config) *Client {
	githubCfg := &cfg.GitHub
	timeout := githubCfg.Timeout

	baseClient := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		},
	}

	retryConfig := httpclient.RetryConfig{
		MaxRetries:        githubCfg.MaxRetries,
		BaseBackoff:       1 * time.Second,
		MaxBackoff:        60 * time.Second,
		RetryOn5xx:        true,
		RetryOnNetworkErr: true,
	}

	return &Client{
		token:          githubCfg.Token,
		config:         githubCfg,
		appConfig:      cfg,
		appTimeoutSecs: cfg.App.TimeoutSeconds,
		httpClient:     httpclient.NewClient(baseClient, retryConfig),
	}
}

// FetchBasicRepositoryInfo fetches lightweight repository information (no dependency manifests).
//
// Intent and usage:
//   - This is the low-cost query used in high-parallelism paths (PURL batch → repo state enrichment).
//   - It returns essential state (archived/disabled/fork), recent commit activity snapshots, and
//     basic metadata (stars, forks, description, homepage) with minimal GraphQL cost.
//   - Use for: "Given owner/repo, enrich analyses with state/metadata efficiently at scale.".
//
// Why separate from FetchDetailedRepositoryInfo?
//   - The detailed variant includes dependencyGraphManifests which is expensive and unnecessary for
//     state enrichment. Keeping them separate prevents wasting GraphQL budget during large batches.
//
// Do not merge lightly:
//   - If unified in the future, use an options pattern and make the default keep manifests disabled in
//     batch contexts to preserve performance.
//
// Used by: PURL batch processing, repository state fetching
func (c *Client) FetchBasicRepositoryInfo(ctx context.Context, owner, repo string) (*RepositoryInfo, error) {
	if c.token == "" {
		slog.Debug("GitHub token not available - skipping basic repository info fetch", "owner", owner, "repo", repo)
		return nil, nil
	}

	// Create lightweight GraphQL query without dependency manifests for PURL-based processing
	// Includes licenseInfo so we can reuse the same response for project license fallback
	query := `
		query ($owner: String!, $name: String!, $historySize: Int = 100) {
		  repository(owner: $owner, name: $name) {
		    isArchived
		    isDisabled
		    isFork
		    stargazerCount
		    forkCount
		    description
		    homepageUrl
		    primaryLanguage { name }
		    licenseInfo { spdxId name }
		    repositoryTopics(first: 20) { nodes { topic { name } } }
		    parent { nameWithOwner }

		    defaultBranchRef {
		      name
		      target {
		        ... on Commit {
		          history(first: $historySize) {
		            nodes {
		              committedDate
		              author { user { login } }
		            }
		          }
		        }
		      }
		    }
		  }
		  rateLimit {
		    cost
		    remaining
		    resetAt
		  }
		}
	`

	variables := map[string]interface{}{
		"owner":       owner,
		"name":        repo,
		"historySize": 100,
	}

	return c.executeGraphQLQuery(ctx, query, variables)
}

// FetchDetailedRepositoryInfo fetches detailed repository information including dependency manifests.
//
// Intent and usage:
//   - Used in the [[GitHub URL → PURL]] flow to detect which ecosystems/manifests are present.
//   - Higher GraphQL cost due to dependencyGraphManifests; avoid using in batch state enrichment.
//   - Use for: "Given a GitHub URL, decide which ecosystem PURLs to generate".
//
// Relationship with FetchBasicRepositoryInfo:
//   - Complementary: basic serves batch state enrichment, detailed serves URL→PURL detection.
//     Both are intentionally separate for performance and rate-limit reasons.
//
// Used by: GitHub URL processing, PURL generation from GitHub URLs
func (c *Client) FetchDetailedRepositoryInfo(ctx context.Context, owner, repo string) (*RepositoryInfo, error) {
	if c.token == "" {
		slog.Debug("GitHub token not available - skipping detailed repository info fetch", "owner", owner, "repo", repo)
		return nil, nil
	}

	// Create detailed GraphQL query with dependency manifests for GitHub URL → PURL conversion
	query := `
		query ($owner: String!, $name: String!, $historySize: Int = 100, $manifests: Int = 20) {
		  repository(owner: $owner, name: $name) {
		    isArchived
		    isDisabled
		    isFork
		    stargazerCount
		    forkCount
		    description
		    homepageUrl
		    primaryLanguage { name }
		    licenseInfo { spdxId name }
		    repositoryTopics(first: 20) { nodes { topic { name } } }
		    parent { nameWithOwner }

		    defaultBranchRef {
		      name
		      target {
		        ... on Commit {
		          history(first: $historySize) {
		            nodes {
		              committedDate
		              author { user { login } }
		            }
		          }
		        }
		      }
		    }

		    dependencyGraphManifests(first: $manifests) {
		      nodes {
		        filename
		        dependencies(first: 1) {
		          nodes {
		            packageManager
		            packageName
		          }
		        }
		      }
		    }
		  }
		  rateLimit {
		    cost
		    remaining
		    resetAt
		  }
		}
	`

	variables := map[string]interface{}{
		"owner":       owner,
		"name":        repo,
		"historySize": 100,
		"manifests":   20,
	}

	return c.executeGraphQLQuery(ctx, query, variables)
}

// FetchRepoLanguages calls the GitHub REST API (no authentication required) to retrieve
// the primary languages used in a repository. Returns a map of language -> bytes.
// This works without a token (unauthenticated: 60 req/h rate limit).
func (c *Client) FetchRepoLanguages(ctx context.Context, owner, repo string) (map[string]int, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/languages", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "uzomuzo-github-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch languages: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.CopyN(io.Discard, resp.Body, 1024) // best-effort drain before close
		return nil, fmt.Errorf("GitHub languages API returned %d", resp.StatusCode)
	}

	var languages map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&languages); err != nil {
		return nil, fmt.Errorf("failed to decode languages response: %w", err)
	}
	return languages, nil
}

// MaxTopics caps the number of topics retained from a GraphQL response. Matches
// the `repositoryTopics(first: 20)` GraphQL parameter; enforced defensively in
// code so a future schema change cannot silently inflate the slice.
const MaxTopics = 20

// collectTopics extracts topic names from a GraphQL repositoryTopics connection.
// Returns a non-nil slice (possibly empty) so callers can use nil as the
// "not fetched" sentinel. Topics are already lowercased by GitHub; we deduplicate
// defensively while preserving insertion order, and cap the result at MaxTopics.
func collectTopics(c RepositoryTopicConnection) []string {
	topics := make([]string, 0, len(c.Nodes))
	seen := make(map[string]struct{}, len(c.Nodes))
	for _, n := range c.Nodes {
		name := strings.TrimSpace(n.Topic.Name)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		topics = append(topics, name)
		if len(topics) >= MaxTopics {
			break
		}
	}
	return topics
}

// executeGraphQLQuery executes a GraphQL query and returns repository information
func (c *Client) executeGraphQLQuery(ctx context.Context, query string, variables map[string]interface{}) (*RepositoryInfo, error) {
	request := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}

	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, common.NewIOError("failed to marshal GraphQL request", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", graphqlEndpoint(c.config), bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, common.NewIOError("failed to create HTTP request", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(ctx, httpReq)
	if err != nil {
		return nil, common.NewFetchError("failed to execute GraphQL request", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, common.NewAuthenticationError("GitHub authentication failed: token is invalid or expired. Set a valid GITHUB_TOKEN in .env or run 'gh auth login'", nil).
				WithContext("status_code", resp.StatusCode)
		}
		return nil, common.NewFetchError("GitHub API returned error status", nil).
			WithContext("status_code", resp.StatusCode)
	}

	var graphqlResp GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&graphqlResp); err != nil {
		return nil, common.NewIOError("failed to decode GraphQL response", err)
	}

	if len(graphqlResp.Errors) > 0 {
		// Detect common not-found pattern to enable fallback handling upstream
		msgAll := strings.ToLower(fmt.Sprintf("%v", graphqlResp.Errors))
		if strings.Contains(msgAll, "could not resolve to a repository") || strings.Contains(msgAll, "not_found") {
			return nil, common.NewResourceNotFoundError("repository not found").WithContext("errors", graphqlResp.Errors)
		}
		return nil, common.NewFetchError("GraphQL query returned errors", nil).WithContext("graphql_errors", graphqlResp.Errors)
	}

	// Check and handle rate limit information
	rateLimit := graphqlResp.Data.RateLimit
	if rateLimit.Remaining <= 0 {
		// Parse and format the reset time for better user experience
		resetTime, err := time.Parse(time.RFC3339, rateLimit.ResetAt)
		var resetTimeStr string
		if err != nil {
			resetTimeStr = rateLimit.ResetAt // Fallback to raw string if parsing fails
		} else {
			resetTimeStr = resetTime.Format("2006-01-02 15:04:05 MST")
		}

		return nil, common.NewRateLimitError("GitHub API rate limit exceeded", nil).
			WithContext("remaining_requests", rateLimit.Remaining).
			WithContext("reset_time", resetTimeStr).
			WithContext("cost", rateLimit.Cost)
	}

	// Record aggregated rate limit stats (no per-request log to reduce noise)
	c.recordRateLimit(rateLimit.Cost, rateLimit.Remaining, rateLimit.ResetAt)

	return &graphqlResp.Data.Repository, nil
}

// graphqlEndpoint resolves the GraphQL endpoint from GitHubConfig.BaseURL using the
// same TrimRight + fallback pattern used by some REST helpers (e.g., contents.go), so
// GraphQL requests can be redirected to GHES or httptest fixtures via BaseURL.
// Note: not all REST callers honor BaseURL yet (e.g., FetchRepoLanguages hardcodes
// api.github.com); this function only governs the GraphQL path.
//
// Suffix translation: a BaseURL ending in "/api/v3" (the canonical GHES REST root)
// is rewritten to "/api/graphql"; any other base value gets "/graphql" appended.
// This is a generic suffix rule, not GHES-only — any deployment that exposes a
// "/api/v3" REST root and "/api/graphql" GraphQL root benefits.
// Defaults to api.github.com when BaseURL is unset.
func graphqlEndpoint(cfg *config.GitHubConfig) string {
	base := ""
	if cfg != nil {
		base = strings.TrimRight(cfg.BaseURL, "/")
	}
	if base == "" {
		base = "https://api.github.com"
	}
	if strings.HasSuffix(base, "/api/v3") {
		return strings.TrimSuffix(base, "/api/v3") + "/api/graphql"
	}
	return base + "/graphql"
}

// forkSourceFromRepoInfo extracts the parent repository name ("owner/repo") from a
// GraphQL RepositoryInfo response. Returns empty string when the repo is not a fork or parent
// data is unavailable (e.g. private parent).
func forkSourceFromRepoInfo(info *RepositoryInfo) string {
	if info == nil || !info.IsFork {
		return ""
	}
	if info.Parent != nil && info.Parent.NameWithOwner != "" {
		return info.Parent.NameWithOwner
	}
	return ""
}

// normalizeRepoURL follows GitHub redirects for a repository HTML URL and returns the final URL.
// Best-effort; returns empty string on failure.
func (c *Client) normalizeRepoURL(ctx context.Context, raw string) string {
	if raw == "" || !strings.Contains(strings.ToLower(raw), "github.com") {
		return ""
	}
	// Ensure scheme
	urlStr := raw
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		urlStr = "https://" + urlStr
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "uzomuzo-github-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.httpClient.Do(ctx, req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.CopyN(io.Discard, resp.Body, 1024) // best-effort drain before close
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String()
	}
	return ""
}

// primaryLanguageName returns the GitHub-reported primary language name, or "" when
// the GraphQL primaryLanguage field is null (e.g. empty repos, docs-only repos) or
// when the returned name is blank/whitespace-only.
func primaryLanguageName(p *PrimaryLanguage) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.Name)
}
