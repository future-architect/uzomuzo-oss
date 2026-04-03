// Package actionscan discovers GitHub Actions referenced by repositories.
//
// Given a list of GitHub repository URLs, the service fetches their
// .github/workflows/*.yml files via the GitHub Contents API, parses uses:
// directives, and returns the set of discovered Action URLs.
//
// DDD Layer: Infrastructure (I/O, external API orchestration)
package actionscan

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/ghaworkflow"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
)

// DiscoveryResult contains discovered action URLs with provenance.
type DiscoveryResult struct {
	// Actions maps GitHub URL → depth at which it was first discovered.
	Actions map[string]int
	// Errors collects non-fatal fetch/parse errors keyed by source URL or path.
	Errors map[string]error
}

// DiscoveryService discovers GitHub Actions referenced by repositories.
type DiscoveryService struct {
	githubClient   *github.Client
	maxConcurrency int
}

// NewDiscoveryService creates a DiscoveryService.
// maxConcurrency limits concurrent GitHub API calls; values ≤ 0 default to 5.
func NewDiscoveryService(githubClient *github.Client, maxConcurrency int) (*DiscoveryService, error) {
	if githubClient == nil {
		return nil, fmt.Errorf("new discovery service: github client is nil")
	}
	if maxConcurrency <= 0 {
		maxConcurrency = 5
	}
	return &DiscoveryService{githubClient: githubClient, maxConcurrency: maxConcurrency}, nil
}

// DiscoverActions fetches workflows for each GitHub URL, parses uses: directives,
// and returns discovered Action URLs. Phase 1 supports depth=1 only (direct workflows).
//
// Each input URL is expected to be "https://github.com/{owner}/{repo}".
// Repositories without a .github/workflows/ directory are silently skipped.
// Parse/fetch errors for individual files are collected in DiscoveryResult.Errors
// rather than aborting the entire scan.
//
// The returned actionURLs slice is sorted lexicographically for deterministic output.
// This method satisfies scan.ActionsDiscoverer.
func (s *DiscoveryService) DiscoverActions(ctx context.Context, repoURLs []string) (actionURLs []string, errors map[string]error, err error) {
	result := &DiscoveryResult{
		Actions: make(map[string]int),
		Errors:  make(map[string]error),
	}

	// Collect URLs with mutex protection, bounded by semaphore.
	var orderedURLs []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, s.maxConcurrency)

	for _, repoURL := range repoURLs {
		wg.Add(1)
		go func(repoURL string) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire semaphore
			defer func() { <-semaphore }() // Release semaphore

			owner, repo, err := common.ExtractGitHubOwnerRepo(repoURL)
			if err != nil {
				mu.Lock()
				result.Errors[repoURL] = err
				mu.Unlock()
				return
			}

			urls, errs := s.discoverFromRepo(ctx, owner, repo)

			mu.Lock()
			for _, u := range urls {
				if _, exists := result.Actions[u]; !exists {
					result.Actions[u] = 0
					orderedURLs = append(orderedURLs, u)
				}
			}
			for k, v := range errs {
				result.Errors[k] = v
			}
			mu.Unlock()
		}(repoURL)
	}

	wg.Wait()

	// Sort for deterministic output — goroutine scheduling makes insertion order
	// non-deterministic across runs.
	sort.Strings(orderedURLs)

	slog.Info("actions discovery complete",
		"repos_scanned", len(repoURLs),
		"actions_found", len(result.Actions),
		"errors", len(result.Errors),
	)

	return orderedURLs, result.Errors, nil
}

// discoverFromRepo fetches workflow files from a single repository and parses them.
func (s *DiscoveryService) discoverFromRepo(ctx context.Context, owner, repo string) ([]string, map[string]error) {
	errs := make(map[string]error)

	entries, err := s.githubClient.FetchDirectoryContents(ctx, owner, repo, ".github/workflows")
	if err != nil {
		errs[fmt.Sprintf("%s/%s/.github/workflows", owner, repo)] = err
		return nil, errs
	}
	if entries == nil {
		slog.Debug("no .github/workflows directory", "owner", owner, "repo", repo)
		return nil, nil
	}

	// Filter to YAML files only.
	var yamlFiles []github.DirectoryEntry
	for _, e := range entries {
		if e.Type != "file" {
			continue
		}
		lower := strings.ToLower(e.Name)
		if strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml") {
			yamlFiles = append(yamlFiles, e)
		}
	}

	if len(yamlFiles) == 0 {
		slog.Debug("no workflow YAML files found", "owner", owner, "repo", repo)
		return nil, nil
	}

	slog.Debug("fetching workflow files", "owner", owner, "repo", repo, "count", len(yamlFiles))

	seen := make(map[string]struct{})
	var allURLs []string

	for _, yf := range yamlFiles {
		data, fetchErr := s.githubClient.FetchFileContent(ctx, owner, repo, yf.Path)
		if fetchErr != nil {
			errs[fmt.Sprintf("%s/%s/%s", owner, repo, yf.Path)] = fetchErr
			continue
		}
		if data == nil {
			continue // 404 — file disappeared between listing and fetch
		}

		urls, parseErr := ghaworkflow.ParseGitHubURLs(data)
		if parseErr != nil {
			errs[fmt.Sprintf("%s/%s/%s", owner, repo, yf.Path)] = parseErr
			continue
		}

		for _, u := range urls {
			if _, exists := seen[u]; !exists {
				seen[u] = struct{}{}
				allURLs = append(allURLs, u)
			}
		}
	}

	return allURLs, errs
}
