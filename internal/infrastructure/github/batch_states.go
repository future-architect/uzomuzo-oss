package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
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
	// language is the GitHub-reported primary language (e.g., "Go", "Python").
	// Empty when the repository has no detected language.
	language string
	// topics is non-nil (possibly empty) on a successful fetch. Stays nil when
	// the result represents an error so downstream callers can distinguish
	// "fetched, none" from "not fetched". See domain.Repository.Topics.
	topics []string
}

// FetchRepositoryStates fetches repository states for multiple URLs with parallel processing
//
// ⚠️  GitHub API Rate Limits:
//   - REST API: 5,000 requests/hour (authenticated)
//   - GraphQL API: 5,000 points/hour (authenticated)
//   - Details: https://docs.github.com/en/rest/using-the-rest-api/rate-limits
//
// 📝 Important Limitations:
//   - Rate limit exceeded causes continuous errors for 1 hour
//   - No retry for rate limit errors (immediate failure)
//   - Reason: 1-hour wait required, retries are ineffective
//   - Solution: Proactive control via MaxConcurrency and RequestInterval
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
		// Set default values for all analyses instead of failing.
		// Preserve IsArchived if already set from Scorecard data (deps.dev fallback).
		for _, analysis := range analyses {
			if analysis != nil {
				if analysis.RepoState == nil {
					analysis.RepoState = &domain.RepoState{}
				}
				// Don't overwrite IsArchived — it may already be true via Scorecard "Maintained" check
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
	repoStates, repoErrors, repoMetas := c.fetchRepositoryStatesBatch(ctx, repoURLs)

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
				analysis.Repository.Summary = domain.NormalizeSummary(meta.description)
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
			if analysis.Repository.Language == "" && meta.language != "" {
				analysis.Repository.Language = meta.language
			}
			// License enrichment: fallback only (do not override canonical deps.dev SPDX values).
			if meta.license != nil {
				if updated, changed := enrichProjectLicenseFromGitHub(analysis.ProjectLicense, meta.license); changed {
					analysis.ProjectLicense = updated
				}
			}
			// Topics: meta entry exists => GraphQL fetch succeeded. meta.topics is non-nil
			// (possibly empty); preserve nil sentinel for analyses without a meta entry.
			if meta.topics != nil {
				analysis.Repository.Topics = meta.topics
			}
		}
	}

	return nil
}

// fetchRepositoryStatesBatch efficiently fetches repository states for multiple URLs.
// Package-internal: only called by FetchRepositoryStates.
func (c *Client) fetchRepositoryStatesBatch(ctx context.Context, repoURLs []string) (map[string]*domain.RepoState, map[string]error, map[string]repoMeta) {
	if len(repoURLs) == 0 {
		return make(map[string]*domain.RepoState), make(map[string]error), make(map[string]repoMeta)
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
	errs := make(map[string]error)
	metas := make(map[string]repoMeta)
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
			if common.IsRateLimitError(result.err) {
				rateLimitExceeded = true
				slog.Error("GitHub API rate limit exceeded during batch processing",
					"repo_url", result.repoURL,
					"error", result.err)
			} else {
				slog.Debug("Failed to fetch repository state",
					"repo_url", result.repoURL,
					"error", result.err)
			}
			errs[result.repoURL] = result.err
		} else {
			results[result.repoURL] = result.repoState
			metas[result.repoURL] = repoMeta{
				stars:         result.stars,
				forks:         result.forks,
				description:   result.description,
				homepage:      result.homepage,
				license:       result.license,
				defaultBranch: result.defaultBranch,
				language:      result.language,
				topics:        result.topics,
			}
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

	return results, errs, metas
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
				} else if common.IsRateLimitError(err) {
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

		// Return the RepoState; the caller re-keys results by repoURL and enriches
		// analysis.Repository with star/metadata fields (RepoState itself does not carry them).
		resultChannel <- repoResult{
			repoURL:       repoURL,
			repoState:     repoState,
			stars:         repoInfo.StargazerCount,
			forks:         repoInfo.ForkCount,
			description:   repoInfo.Description,
			homepage:      repoInfo.HomepageURL,
			license:       repoInfo.LicenseInfo,
			defaultBranch: repoInfo.DefaultBranchRef.Name,
			language:      primaryLanguageName(repoInfo.PrimaryLanguage),
			topics:        collectTopics(repoInfo.RepositoryTopics),
		}

		cancel()
	}
}
