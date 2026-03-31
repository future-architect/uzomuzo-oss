// Package github provides GitHub API client for repository state fetching.
//
// 🚨 GitHub API Rate Limit Strategy:
//
// GitHub API has strict rate limits:
// - REST API: 5,000 requests/hour (authenticated)
// - GraphQL API: 5,000 points/hour (authenticated)
// - Official docs: https://docs.github.com/en/rest/using-the-rest-api/rate-limits
//
// Design Decision:
// ❌ No retry for rate limit errors (HTTP 429)
// ✅ Reason: Rate limit exceeded requires 1-hour wait, short retries are ineffective
// ✅ Alternative: Proactive control to avoid limits
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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// repoResult stores parallel processing results
type repoResult struct {
	repoURL   string
	repoState *domain.RepoState
	err       error
	// Metadata
	stars         int
	forks         int
	description   string
	homepage      string
	license       *LicenseInfo // newly captured licenseInfo (spdxId/name) to avoid extra query
	defaultBranch string
}

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
		    licenseInfo { spdxId name }
		    source { nameWithOwner }

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
		    licenseInfo { spdxId name }
		    source { nameWithOwner }

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

// FetchRepositoryStates fetches repository states for multiple URLs with parallel processing
//
// ⚠️  GitHub API Rate Limits:
// - REST API: 5,000 requests/hour (authenticated)
// - GraphQL API: 5,000 points/hour (authenticated)
// - Details: https://docs.github.com/en/rest/using-the-rest-api/rate-limits
//
// 📝 Important Limitations:
// - Rate limit exceeded causes continuous errors for 1 hour
// - No retry for rate limit errors (immediate failure)
// - Reason: 1-hour wait required, retries are ineffective
// - Solution: Proactive control via MaxConcurrency and RequestInterval
func (c *Client) FetchRepositoryStates(ctx context.Context, analyses map[string]*domain.Analysis) error {
	if c.token == "" {
		repoCount := 0
		for _, analysis := range analyses {
			if analysis != nil && analysis.RepoURL != "" {
				repoCount++
			}
		}
		if repoCount > 0 {
			slog.Debug("GitHub token not available - commit data will be missing for lifecycle assessment",
				"affected_repos", repoCount,
			)
		}
		// Set default values for all analyses instead of failing
		for _, analysis := range analyses {
			if analysis != nil {
				if analysis.RepoState == nil {
					analysis.RepoState = &domain.RepoState{}
				}
				analysis.RepoState.IsArchived = false
				analysis.RepoState.IsDisabled = false
			}
		}
		return nil
	}

	// Extract repository URLs (GitHub only)
	repoURLs := make([]string, 0, len(analyses))
	for _, analysis := range analyses {
		if analysis != nil && analysis.RepoURL != "" {
			// Only process GitHub repositories; skip others to avoid parse errors
			if strings.Contains(strings.ToLower(analysis.RepoURL), "github.com") {
				repoURLs = append(repoURLs, analysis.RepoURL)
			} else {
				slog.Debug("Skipping non-GitHub repository for GitHub client", "repo_url", analysis.RepoURL)
			}
		}
	}

	if len(repoURLs) == 0 {
		return nil
	}

	slog.Debug("Starting parallel GitHub repository state fetch",
		"repo_count", len(repoURLs),
		"max_concurrency", c.config.MaxConcurrency)

	// Fetch repository states in parallel
	repoStates, repoErrors, repoMetas := c.FetchRepositoryStatesBatch(ctx, repoURLs)

	// Update analyses with fetched repository states and errors; enrich Repository metadata if available
	for _, analysis := range analyses {
		if analysis == nil || analysis.RepoURL == "" {
			continue
		}
		if repoState, exists := repoStates[analysis.RepoURL]; exists {
			analysis.RepoState = repoState
		}
		if repoError, hasError := repoErrors[analysis.RepoURL]; hasError {
			// Only set GitHub error if analysis has no pre-existing error.
			// GitHub enrichment is best-effort; a prior error (e.g. deps.dev
			// ResourceNotFoundError) carries typed semantics that downstream
			// fallback logic (registry / catalog) relies on.
			if analysis.Error == nil {
				analysis.Error = repoError
			} else {
				slog.Debug("github_repo_error_preserved_existing",
					"repo_url", analysis.RepoURL,
					"github_error", repoError,
					"existing_error", analysis.Error,
				)
			}
		}
		if analysis.Repository == nil {
			analysis.Repository = &domain.Repository{URL: analysis.RepoURL}
		} else if analysis.Repository.URL == "" {
			analysis.Repository.URL = analysis.RepoURL
		}
		if meta, ok := repoMetas[analysis.RepoURL]; ok {
			if meta.stars > 0 {
				analysis.Repository.StarsCount = meta.stars
			}
			if meta.forks > 0 {
				analysis.Repository.ForksCount = meta.forks
			}
			if meta.description != "" {
				analysis.Repository.Description = meta.description
			}
			if meta.homepage != "" {
				if analysis.PackageLinks == nil {
					analysis.PackageLinks = &domain.PackageLinks{}
				}
				if analysis.PackageLinks.HomepageURL == "" {
					analysis.PackageLinks.HomepageURL = meta.homepage
				}
			}
			// Default branch propagation (enables optimal downstream raw fetches without guessing)
			if analysis.Repository.DefaultBranch == "" && meta.defaultBranch != "" {
				analysis.Repository.DefaultBranch = meta.defaultBranch
			}
			// License enrichment: fallback only (do not override canonical deps.dev SPDX values).
			if meta.license != nil {
				if updated, changed := enrichProjectLicenseFromGitHub(analysis.ProjectLicense, meta.license); changed {
					analysis.ProjectLicense = updated
				}
			}
		}
	}

	return nil
}

// FetchRepositoryStatesBatch efficiently fetches repository states for multiple URLs
func (c *Client) FetchRepositoryStatesBatch(ctx context.Context, repoURLs []string) (map[string]*domain.RepoState, map[string]error, map[string]struct {
	stars, forks          int
	description, homepage string
	license               *LicenseInfo
	defaultBranch         string
}) {
	if len(repoURLs) == 0 {
		return make(map[string]*domain.RepoState), make(map[string]error), make(map[string]struct {
			stars, forks          int
			description, homepage string
			license               *LicenseInfo
			defaultBranch         string
		})
	}

	// Remove duplicates
	uniqueURLs := make([]string, 0, len(repoURLs))
	seen := make(map[string]bool)
	for _, url := range repoURLs {
		if !seen[url] {
			uniqueURLs = append(uniqueURLs, url)
			seen[url] = true
		}
	}

	slog.Debug("Fetching repository states",
		"unique_repos", len(uniqueURLs),
		"max_concurrency", c.config.MaxConcurrency)

	// Create channels for parallel processing
	resultChan := make(chan repoResult, len(uniqueURLs))
	repoChannel := make(chan string, len(uniqueURLs))

	// Fill the channel with repository URLs
	for _, url := range uniqueURLs {
		repoChannel <- url
	}
	close(repoChannel) // Start worker goroutines with progress tracking
	maxWorkers := c.config.MaxConcurrency
	if maxWorkers <= 0 {
		maxWorkers = 20 // Increased default for GitHub GraphQL efficiency
	}
	// Cap maximum workers to prevent overwhelming GitHub API
	if maxWorkers > 30 {
		maxWorkers = 30
	}

	// Reset aggregated rate limit metrics for this batch
	c.resetRateLimitAggregation()

	// Display initial progress message for batch processing (log every 100)
	if len(uniqueURLs) > 100 {
		fmt.Printf("🔄 GitHub GraphQL processing started: 0/%d repositories (progress every 100)\n", len(uniqueURLs))
	}

	// Wrap context so we can cancel all workers on fatal errors (e.g. auth failure)
	batchCtx, batchCancel := context.WithCancel(ctx)
	defer batchCancel()

	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go c.githubWorker(batchCtx, batchCancel, repoChannel, resultChan, &wg)
	}

	// Wait for all workers to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}() // Collect results and errors with progress tracking
	results := make(map[string]*domain.RepoState)
	errors := make(map[string]error)
	metas := make(map[string]struct {
		stars, forks          int
		description, homepage string
		license               *LicenseInfo
		defaultBranch         string
	})
	rateLimitExceeded := false
	processedCount := 0
	totalRepos := len(uniqueURLs)

	// Initial progress (only for larger batches)
	if totalRepos > 100 {
		fmt.Printf("🔄 GitHub GraphQL processing: 0/%d repositories processed\n", totalRepos)
	}

	for result := range resultChan {
		processedCount++

		if result.err != nil {
			// Check if this is a rate limit error
			if c.isRateLimitError(result.err) {
				rateLimitExceeded = true
				slog.Error("GitHub API rate limit exceeded during batch processing",
					"repo_url", result.repoURL,
					"error", result.err)
			} else {
				slog.Debug("Failed to fetch repository state",
					"repo_url", result.repoURL,
					"error", result.err)
			}
			errors[result.repoURL] = result.err
		} else {
			results[result.repoURL] = result.repoState
			metas[result.repoURL] = struct {
				stars, forks          int
				description, homepage string
				license               *LicenseInfo
				defaultBranch         string
			}{stars: result.stars, forks: result.forks, description: result.description, homepage: result.homepage, license: result.license, defaultBranch: result.defaultBranch}
		}
		// Display progress every 100 repositories (or at the end) including aggregated rate limit info
		if totalRepos > 100 && (processedCount%100 == 0 || processedCount == totalRepos) {
			costTotal, remaining, resetAt, avgCost := c.snapshotRateLimit()
			if resetAt == "" {
				fmt.Printf("🔄 GitHub GraphQL progress: %d/%d (total_cost=%d avg_cost=%.2f)\n", processedCount, totalRepos, costTotal, avgCost)
			} else {
				fmt.Printf("🔄 GitHub GraphQL progress: %d/%d (total_cost=%d avg_cost=%.2f remaining=%d reset=%s)\n", processedCount, totalRepos, costTotal, avgCost, remaining, formatResetLocal(resetAt))
			}
		}
	}

	// Log summary with rate limit warning if applicable
	if rateLimitExceeded {
		slog.Warn("Batch processing completed with rate limit errors",
			"successful", len(results),
			"total", len(uniqueURLs),
			"rate_limit_errors", "some requests failed due to API rate limits")
	} else {
		slog.Debug("Completed repository state batch fetch",
			"successful", len(results),
			"total", len(uniqueURLs))
	}

	// Display final progress message for batch processing
	if totalRepos > 100 {
		costTotal, remaining, resetAt, avgCost := c.snapshotRateLimit()
		if resetAt == "" {
			fmt.Printf("✅ GitHub GraphQL processing completed: %d/%d (total_cost=%d avg_cost=%.2f)\n", totalRepos, totalRepos, costTotal, avgCost)
		} else {
			fmt.Printf("✅ GitHub GraphQL processing completed: %d/%d (total_cost=%d avg_cost=%.2f remaining=%d reset=%s)\n", totalRepos, totalRepos, costTotal, avgCost, remaining, formatResetLocal(resetAt))
		}
	}

	return results, errors, metas
}

// isRateLimitError checks if the error is related to GitHub API rate limit
func (c *Client) isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errorMsg := strings.ToLower(err.Error())
	return strings.Contains(errorMsg, "rate limit") || strings.Contains(errorMsg, "remaining: 0")
}

// githubWorker processes repository URLs in parallel.
// batchCancel is called on fatal errors (e.g. authentication failure) to stop all workers early.
func (c *Client) githubWorker(ctx context.Context, batchCancel context.CancelFunc, repoChannel <-chan string, resultChannel chan<- repoResult, wg *sync.WaitGroup) {
	defer wg.Done()
	for repoURL := range repoChannel {
		// If the batch context is already cancelled (e.g. auth failure in another worker),
		// drain remaining URLs with a concise error instead of hitting the API again.
		if ctx.Err() != nil {
			resultChannel <- repoResult{
				repoURL: repoURL,
				// Use AuthenticationError so these skipped entries are grouped with the
			// original auth failure in the batch error summary display.
			// Currently the only fatal error that triggers batchCancel is auth failure.
			err: common.NewAuthenticationError("skipped: GitHub authentication failed (see earlier error)", ctx.Err()),
			}
			continue
		}

		// Create individual timeout for each request with more robust error handling
		repoCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)

		owner, repo, err := common.ExtractGitHubOwnerRepo(repoURL)
		if err != nil {
			resultChannel <- repoResult{
				repoURL: repoURL,
				err:     common.NewValidationError("failed to parse repository URL").WithContext("url", repoURL),
			}
			cancel()
			continue
		}

		repoInfo, err := c.FetchBasicRepositoryInfo(repoCtx, owner, repo)
		if err != nil {
			// If repo not found, try to follow redirect to new location once
			if common.IsResourceNotFoundError(err) {
				if norm := c.normalizeRepoURL(repoCtx, repoURL); norm != "" && norm != repoURL {
					if o2, r2, perr := common.ExtractGitHubOwnerRepo(norm); perr == nil {
						if ri2, e2 := c.FetchBasicRepositoryInfo(repoCtx, o2, r2); e2 == nil {
							repoInfo = ri2
							err = nil
						} else {
							err = e2
						}
					}
				}
			}
			if err != nil {
				// Enhanced error handling with context information
				var enhancedErr error
				if common.IsAuthenticationError(err) {
					enhancedErr = err
					// Fatal: cancel all remaining workers to avoid repeating the same auth error
					slog.Error("GitHub authentication failed — aborting remaining requests. Set a valid GITHUB_TOKEN in .env or run 'gh auth login'")
					batchCancel()
				} else if errors.Is(err, context.DeadlineExceeded) {
					enhancedErr = common.NewTimeoutError("GitHub API timeout", err).
						WithContext("repository", fmt.Sprintf("%s/%s", owner, repo)).
						WithContext("timeout_duration", c.config.Timeout.String())
				} else if c.isRateLimitError(err) {
					enhancedErr = common.NewRateLimitError("GitHub API rate limit exceeded", err).
						WithContext("repository", fmt.Sprintf("%s/%s", owner, repo))
				} else {
					enhancedErr = common.NewFetchError("GitHub API request failed", err).
						WithContext("repository", fmt.Sprintf("%s/%s", owner, repo))
				}

				resultChannel <- repoResult{
					repoURL: repoURL,
					err:     enhancedErr,
				}
				cancel()
				continue
			}
		}

		// Convert RepositoryInfo to RepoState
		repoState := &domain.RepoState{
			IsArchived: repoInfo.IsArchived,
			IsDisabled: repoInfo.IsDisabled,
			IsFork:     repoInfo.IsFork,
			ForkSource: forkSourceFromRepoInfo(repoInfo),
		}

		// Process commit history if available
		if len(repoInfo.DefaultBranchRef.Target.History.Nodes) > 0 {
			commitStats := c.processCommitHistory(repoInfo.DefaultBranchRef.Target.History)
			repoState.CommitStats = commitStats

			// Set latest human commit
			if latestHumanCommit := c.getLatestHumanCommit(repoInfo.DefaultBranchRef.Target.History); latestHumanCommit != nil {
				repoState.LatestHumanCommit = latestHumanCommit
			}

			// Set days since last commit (any commit, not just human)
			if latestCommit := c.getLatestCommit(repoInfo.DefaultBranchRef.Target.History); latestCommit != nil {
				repoState.DaysSinceLastCommit = int(time.Since(*latestCommit).Hours() / 24)
			}
		}

		// Attach metadata to RepoState via unused fields? RepoState does not carry stars; instead,
		// we will pass back RepoState and later enrich analysis.Repository using a side channel is not available.
		// As a pragmatic step, we encode stars etc. into the result by updating a map later. Here, just return state.
		resultChannel <- repoResult{repoURL: repoURL, repoState: repoState, stars: repoInfo.StargazerCount, forks: repoInfo.ForkCount, description: repoInfo.Description, homepage: repoInfo.HomepageURL, license: repoInfo.LicenseInfo, defaultBranch: repoInfo.DefaultBranchRef.Name}

		cancel()
	}
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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(reqBody))
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

// forkSourceFromRepoInfo extracts the ultimate source repository name ("owner/repo") from a
// GraphQL RepositoryInfo response. Returns empty string when the repo is not a fork or source
// data is unavailable (e.g. private parent).
func forkSourceFromRepoInfo(info *RepositoryInfo) string {
	if info == nil || !info.IsFork {
		return ""
	}
	if info.Source != nil && info.Source.NameWithOwner != "" {
		return info.Source.NameWithOwner
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

// FetchRepoLanguages calls the GitHub REST API (no authentication required) to retrieve
// the primary languages used in a repository. Returns a map of language → bytes.
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

// processCommitHistory processes commit history and returns commit statistics
func (c *Client) processCommitHistory(history CommitHistory) *domain.CommitStats {
	if len(history.Nodes) == 0 {
		return &domain.CommitStats{}
	}

	totalCommits := len(history.Nodes)
	userCommits := 0
	botCommits := 0

	for _, commit := range history.Nodes {
		// Check if author and user information exists
		if commit.Author.User != nil && commit.Author.User.Login != "" {
			// Simple heuristic: if login contains "bot", consider it a bot
			if strings.Contains(strings.ToLower(commit.Author.User.Login), "bot") {
				botCommits++
			} else {
				userCommits++
			}
		} else {
			userCommits++ // Unknown author assumed to be human
		}
	}

	return &domain.CommitStats{
		Total:       totalCommits,
		UserCommits: userCommits,
		BotCommits:  botCommits,
		UserRatio:   float64(userCommits) / float64(totalCommits),
		BotRatio:    float64(botCommits) / float64(totalCommits),
	}
}

// getLatestHumanCommit finds the latest commit by a human author
func (c *Client) getLatestHumanCommit(history CommitHistory) *time.Time {
	for _, commit := range history.Nodes {
		// Skip if it's a bot commit - check for nil user first
		if commit.Author.User != nil && commit.Author.User.Login != "" &&
			strings.Contains(strings.ToLower(commit.Author.User.Login), "bot") {
			continue
		}

		// Parse commit date
		if commitTime, err := time.Parse(time.RFC3339, commit.CommittedDate); err == nil {
			return &commitTime
		}
	}
	return nil
}

// getLatestCommit finds the latest commit (including bot commits)
func (c *Client) getLatestCommit(history CommitHistory) *time.Time {
	if len(history.Nodes) == 0 {
		return nil
	}

	// The first commit in the list should be the latest
	if commitTime, err := time.Parse(time.RFC3339, history.Nodes[0].CommittedDate); err == nil {
		return &commitTime
	}
	return nil
}

// enrichProjectLicenseFromGitHub applies GitHub licenseInfo as a fallback.
//
// Rules:
//   - Only modifies when current license is empty OR came from a non-standard deps.dev value.
//   - Prefers SPDX identifier; falls back to name only if it normalizes to a canonical SPDX.
//   - Ignores empty values and NOASSERTION.
//
// Returns (possibly updated license, source, changed).
func enrichProjectLicenseFromGitHub(current domain.ResolvedLicense, license *LicenseInfo) (domain.ResolvedLicense, bool) {
	if license == nil {
		return current, false
	}

	// If already have a canonical SPDX identifier, keep it.
	if current.Identifier != "" && current.IsSPDX {
		return current, false
	}

	// SPDX normalization helper (reject empty / NOASSERTION)
	tryNormalize := func(raw string) (string, bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.EqualFold(raw, "NOASSERTION") {
			return "", false
		}
		norm, is := domain.NormalizeLicenseIdentifier(raw)
		if !is || norm == "" || strings.EqualFold(norm, "NOASSERTION") {
			return "", false
		}
		return norm, true
	}

	// 1. Prefer spdxId
	if lic, ok := tryNormalize(license.SpdxID); ok {
		if current.Identifier == "" || current.IsNonStandard() {
			return domain.ResolvedLicense{Identifier: lic, Raw: license.SpdxID, IsSPDX: true, Source: domain.LicenseSourceGitHubProjectSPDX}, true
		}
		return current, false
	}
	// 2. Next try name
	if lic, ok := tryNormalize(license.Name); ok {
		if current.Identifier == "" || current.IsNonStandard() {
			return domain.ResolvedLicense{Identifier: lic, Raw: license.Name, IsSPDX: true, Source: domain.LicenseSourceGitHubProjectSPDX}, true
		}
		return current, false
	}

	// 3. Capture non-SPDX raw (name preferred, else spdxId if it had some string) when we still have no SPDX
	pickRaw := func(v string) string {
		v = strings.TrimSpace(v)
		if v == "" || strings.EqualFold(v, "NOASSERTION") {
			return ""
		}
		return v
	}
	rawCandidate := pickRaw(license.Name)
	if rawCandidate == "" {
		rawCandidate = pickRaw(license.SpdxID)
	}
	if rawCandidate == "" { // nothing to record
		return current, false
	}

	if current.IsZero() || current.IsNonStandard() {
		if current.IsNonStandard() && current.Raw == rawCandidate { // identical already
			return current, false
		}
		return domain.ResolvedLicense{Identifier: "", Raw: rawCandidate, IsSPDX: false, Source: domain.LicenseSourceGitHubProjectNonStandard}, true
	}
	return current, false
}

// resetRateLimitAggregation resets aggregated rate limit tracking counters.
func (c *Client) resetRateLimitAggregation() {
	c.rateMu.Lock()
	c.rateLimitTotalCost = 0
	c.rateLimitQueries = 0
	c.rateLimitRemaining = 0
	c.rateLimitResetAt = ""
	c.rateMu.Unlock()
}

// recordRateLimit records a single query's rate limit info into aggregated counters.
func (c *Client) recordRateLimit(cost, remaining int, resetAt string) {
	c.rateMu.Lock()
	c.rateLimitTotalCost += cost
	c.rateLimitQueries++
	c.rateLimitRemaining = remaining // latest snapshot
	c.rateLimitResetAt = resetAt
	c.rateMu.Unlock()
}

// snapshotRateLimit returns a consistent snapshot of aggregated rate limit metrics.
func (c *Client) snapshotRateLimit() (costTotal int, remaining int, resetAt string, avgCost float64) {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	costTotal = c.rateLimitTotalCost
	remaining = c.rateLimitRemaining
	resetAt = c.rateLimitResetAt
	if c.rateLimitQueries > 0 {
		avgCost = float64(costTotal) / float64(c.rateLimitQueries)
	}
	return
}

// RateLimitSummary returns the latest remaining quota and reset time
// from GitHub GraphQL API rate limit tracking.
// resetAt is an ISO-8601 timestamp (empty if no API calls were made).
func (c *Client) RateLimitSummary() (remaining int, resetAt string) {
	_, remaining, resetAt, _ = c.snapshotRateLimit()
	return
}

// formatResetLocal converts an RFC3339 reset timestamp to local time for display.
// Returns the original string unchanged if parsing fails, or empty string if input is empty.
func formatResetLocal(resetAt string) string {
	if resetAt == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, resetAt); err == nil {
		return t.Local().Format("15:04 MST")
	}
	return resetAt
}
