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
// and recursively resolves transitive composite action dependencies.
//
// Each input URL is expected to be "https://github.com/{owner}/{repo}".
// Repositories without a .github/workflows/ directory are silently skipped.
// Parse/fetch errors for individual files are collected in DiscoveryOutput.Errors
// rather than aborting the entire scan.
//
// The returned slices are sorted lexicographically for deterministic output.
// This method satisfies scan.ActionsDiscoverer.
func (s *DiscoveryService) DiscoverActions(ctx context.Context, repoURLs []string) (directURLs, transitiveURLs []string, errors map[string]error, err error) {
	result := &DiscoveryResult{
		Actions: make(map[string]int),
		Errors:  make(map[string]error),
	}

	// Phase 1: Discover direct actions from workflow files.
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, s.maxConcurrency)

	for _, repoURL := range repoURLs {
		wg.Add(1)
		go func(repoURL string) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire semaphore
			defer func() { <-semaphore }() // Release semaphore

			owner, repo, parseErr := common.ExtractGitHubOwnerRepo(repoURL)
			if parseErr != nil {
				mu.Lock()
				result.Errors[repoURL] = parseErr
				mu.Unlock()
				return
			}

			urls, errs := s.discoverFromRepo(ctx, owner, repo)

			mu.Lock()
			for _, u := range urls {
				if _, exists := result.Actions[u]; !exists {
					result.Actions[u] = 0
					directURLs = append(directURLs, u)
				}
			}
			for k, v := range errs {
				result.Errors[k] = v
			}
			mu.Unlock()
		}(repoURL)
	}

	wg.Wait()

	// Phase 2: Resolve transitive composite action dependencies via BFS.
	transitiveURLs = s.resolveTransitiveActions(ctx, directURLs, result)

	// Sort for deterministic output.
	sort.Strings(directURLs)
	sort.Strings(transitiveURLs)

	slog.Info("actions discovery complete",
		"repos_scanned", len(repoURLs),
		"direct_actions", len(directURLs),
		"transitive_actions", len(transitiveURLs),
		"errors", len(result.Errors),
	)

	return directURLs, transitiveURLs, result.Errors, nil
}

// resolveTransitiveActions performs BFS to discover actions referenced by composite actions.
// It fetches action.yml for each queued action, parses composite steps, and adds new
// action URLs to the queue. The seen set (result.Actions) prevents cycles and redundant fetches.
func (s *DiscoveryService) resolveTransitiveActions(ctx context.Context, initialURLs []string, result *DiscoveryResult) []string {
	// Build queue from initial direct URLs.
	type queueItem struct {
		ref ghaworkflow.ActionRef
		url string // GitHub URL (https://github.com/owner/repo)
	}

	var queue []queueItem
	for _, u := range initialURLs {
		owner, repo, err := common.ExtractGitHubOwnerRepo(u)
		if err != nil {
			continue
		}
		queue = append(queue, queueItem{
			ref: ghaworkflow.ActionRef{Owner: owner, Repo: repo},
			url: u,
		})
	}

	var transitiveURLs []string

	for len(queue) > 0 {
		// Process current wave.
		current := queue
		queue = nil

		for _, item := range current {
			data := s.fetchActionYAML(ctx, item.ref, result)
			if data == nil {
				continue
			}

			refs, isComposite, err := ghaworkflow.ParseCompositeActionURLs(data)
			if err != nil {
				result.Errors[item.url+"/action.yml"] = fmt.Errorf("failed to parse action.yml: %w", err)
				continue
			}
			if !isComposite {
				continue
			}

			for _, ref := range refs {
				ghURL := ref.GitHubURL()
				if _, exists := result.Actions[ghURL]; exists {
					continue // Already discovered (direct or earlier transitive).
				}
				result.Actions[ghURL] = 1 // Mark as transitive.
				transitiveURLs = append(transitiveURLs, ghURL)
				queue = append(queue, queueItem{ref: ref, url: ghURL})
			}
		}
	}

	return transitiveURLs
}

// fetchActionYAML fetches action.yml (or action.yaml as fallback) from a GitHub repository.
// Returns nil if neither file exists. Errors are recorded in result.Errors.
func (s *DiscoveryService) fetchActionYAML(ctx context.Context, ref ghaworkflow.ActionRef, result *DiscoveryResult) []byte {
	// Try action.yml first.
	ymlPath := ref.ActionYAMLPath("action.yml")
	data, err := s.githubClient.FetchFileContent(ctx, ref.Owner, ref.Repo, ymlPath)
	if err != nil {
		result.Errors[fmt.Sprintf("%s/%s/%s", ref.Owner, ref.Repo, ymlPath)] = err
		return nil
	}
	if data != nil {
		return data
	}

	// Fallback to action.yaml.
	yamlPath := ref.ActionYAMLPath("action.yaml")
	data, err = s.githubClient.FetchFileContent(ctx, ref.Owner, ref.Repo, yamlPath)
	if err != nil {
		result.Errors[fmt.Sprintf("%s/%s/%s", ref.Owner, ref.Repo, yamlPath)] = err
		return nil
	}
	return data // nil if not found
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
