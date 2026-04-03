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
// Parse/fetch errors for individual files are collected in DiscoveryResult.Errors
// rather than aborting the entire scan.
//
// The returned slices are sorted lexicographically for deterministic output.
// This method satisfies scan.ActionsDiscoverer.
func (s *DiscoveryService) DiscoverActions(ctx context.Context, repoURLs []string, resolveTransitive bool) (directURLs []string, transitiveActions map[string]string, errors map[string]error, err error) {
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

	// Sort before transitive resolution so BFS seed order is deterministic.
	// This ensures consistent "via" parent selection when multiple direct actions
	// lead to the same transitive dependency (lexicographically first wins).
	sort.Strings(directURLs)

	// Phase 2: Resolve transitive composite action dependencies via BFS (opt-in).
	if resolveTransitive {
		transitiveActions = s.resolveTransitiveActions(ctx, directURLs, result)
	}

	slog.Info("actions discovery complete",
		"repos_scanned", len(repoURLs),
		"direct_actions", len(directURLs),
		"transitive_actions", len(transitiveActions),
		"errors", len(result.Errors),
	)

	return directURLs, transitiveActions, result.Errors, nil
}

// actionRefKey returns a dedup key for BFS traversal that includes the subdirectory path.
// Different paths within the same repo (e.g., actions/cache vs actions/cache/save) are
// treated as distinct actions for fetching, while the repo-level URL is used for scan output.
func actionRefKey(ref ghaworkflow.ActionRef) string {
	key := ref.Owner + "/" + ref.Repo
	if ref.Path != "" {
		key += "/" + ref.Path
	}
	return key
}

// buildActionRefFromGitHubURL parses a GitHub URL into an ActionRef, preserving any
// subdirectory path beyond owner/repo (e.g., "https://github.com/owner/repo/subpath").
func buildActionRefFromGitHubURL(rawURL string) (ghaworkflow.ActionRef, error) {
	owner, repo, err := common.ExtractGitHubOwnerRepo(rawURL)
	if err != nil {
		return ghaworkflow.ActionRef{}, fmt.Errorf("extract owner/repo from GitHub URL %q: %w", rawURL, err)
	}

	ref := ghaworkflow.ActionRef{
		Owner: owner,
		Repo:  repo,
	}

	trimmedURL := strings.TrimSpace(rawURL)
	trimmedURL = strings.TrimPrefix(trimmedURL, "https://github.com/")
	trimmedURL = strings.TrimPrefix(trimmedURL, "http://github.com/")
	if idx := strings.IndexAny(trimmedURL, "?#"); idx >= 0 {
		trimmedURL = trimmedURL[:idx]
	}

	parts := strings.Split(trimmedURL, "/")
	if len(parts) > 2 {
		ref.Path = strings.Join(parts[2:], "/")
	}

	return ref, nil
}

// resolveTransitiveActions performs BFS to discover actions referenced by composite actions.
// It fetches action.yml for each queued action, parses composite steps, and adds new
// action URLs to the queue. The seen set tracks owner/repo/path to handle subdirectory actions.
//
// Returns a map of transitive URL → via URL (the direct action that caused the discovery).
// For depth > 1 chains (A→B→C), C's via is A (the original direct action), not B.
func (s *DiscoveryService) resolveTransitiveActions(ctx context.Context, initialURLs []string, result *DiscoveryResult) map[string]string {
	type queueItem struct {
		ref ghaworkflow.ActionRef
		url string // GitHub URL (https://github.com/owner/repo)
		via string // The direct action that led to this discovery
	}

	// Track seen actions by full path (owner/repo/path) to correctly handle
	// subdirectory actions within the same repository.
	seen := make(map[string]struct{})
	for _, u := range initialURLs {
		ref, err := buildActionRefFromGitHubURL(u)
		if err != nil {
			continue
		}
		seen[actionRefKey(ref)] = struct{}{}
	}

	var queue []queueItem
	for _, u := range initialURLs {
		ref, err := buildActionRefFromGitHubURL(u)
		if err != nil {
			continue
		}
		queue = append(queue, queueItem{ref: ref, url: u, via: u})
	}

	// transitive URL → via (direct parent action URL)
	transitiveActions := make(map[string]string)

	for len(queue) > 0 {
		current := queue
		queue = nil

		for _, item := range current {
			data := s.fetchActionYAML(ctx, item.ref, result)
			if data == nil {
				continue
			}

			refs, isComposite, err := ghaworkflow.ParseCompositeActionURLs(data)
			if err != nil {
				result.Errors[actionRefKey(item.ref)] = err
				continue
			}
			if !isComposite {
				continue
			}

			for _, ref := range refs {
				key := actionRefKey(ref)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}

				ghURL := ref.GitHubURL()
				if _, exists := result.Actions[ghURL]; !exists {
					result.Actions[ghURL] = 1
					transitiveActions[ghURL] = item.via
				}
				queue = append(queue, queueItem{ref: ref, url: ghURL, via: item.via})
			}
		}
	}

	return transitiveActions
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

// discoverFromRepo fetches workflow files from a single repository, parses them,
// and resolves local composite actions to discover external action dependencies
// that would otherwise be hidden behind ./ references.
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
	var localPaths []string
	localSeen := make(map[string]struct{})

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

		// Extract local action paths (uses: ./.github/actions/foo).
		locals, parseErr := ghaworkflow.ParseLocalActionPaths(data)
		if parseErr != nil {
			// Error already covered by ParseGitHubURLs above; skip silently.
			continue
		}
		for _, lp := range locals {
			if _, exists := localSeen[lp]; !exists {
				localSeen[lp] = struct{}{}
				localPaths = append(localPaths, lp)
			}
		}
	}

	// Resolve local composite actions to find external action dependencies.
	if len(localPaths) > 0 {
		localURLs := s.resolveLocalActions(ctx, owner, repo, localPaths, errs)
		for _, u := range localURLs {
			if _, exists := seen[u]; !exists {
				seen[u] = struct{}{}
				allURLs = append(allURLs, u)
			}
		}
	}

	return allURLs, errs
}

// resolveLocalActions performs BFS over local composite actions within a repository.
// It fetches action.yml for each local path, extracts external action URLs, and
// follows nested local references (uses: ./) with cycle detection.
//
// Returns external GitHub URLs discovered inside local composite actions.
func (s *DiscoveryService) resolveLocalActions(ctx context.Context, owner, repo string, initialPaths []string, errs map[string]error) []string {
	seen := make(map[string]struct{})
	for _, p := range initialPaths {
		seen[p] = struct{}{}
	}

	queue := append([]string(nil), initialPaths...)
	var externalURLs []string
	externalSeen := make(map[string]struct{})

	for len(queue) > 0 {
		current := queue
		queue = nil

		for _, localPath := range current {
			data := s.fetchLocalActionYAML(ctx, owner, repo, localPath, errs)
			if data == nil {
				continue
			}

			// Extract external action references.
			refs, isComposite, err := ghaworkflow.ParseCompositeActionURLs(data)
			if err != nil {
				errs[fmt.Sprintf("%s/%s/%s/action.yml", owner, repo, localPath)] = err
				continue
			}
			if !isComposite {
				continue
			}

			for _, ref := range refs {
				ghURL := ref.GitHubURL()
				if _, exists := externalSeen[ghURL]; !exists {
					externalSeen[ghURL] = struct{}{}
					externalURLs = append(externalURLs, ghURL)
				}
			}

			// Extract nested local action references for BFS continuation.
			nestedLocals, _, err := ghaworkflow.ParseCompositeLocalActionPaths(data)
			if err != nil {
				// Already logged above; skip.
				continue
			}
			for _, nested := range nestedLocals {
				if _, exists := seen[nested]; !exists {
					seen[nested] = struct{}{}
					queue = append(queue, nested)
				}
			}
		}
	}

	if len(externalURLs) > 0 {
		slog.Debug("resolved local composite actions",
			"owner", owner, "repo", repo,
			"local_actions_scanned", len(seen),
			"external_urls_found", len(externalURLs),
		)
	}

	return externalURLs
}

// fetchLocalActionYAML fetches action.yml (or action.yaml as fallback) for a local action
// path within a repository (e.g., ".github/actions/foo" → fetch ".github/actions/foo/action.yml").
// Returns nil if neither file exists. Errors are recorded in errs.
func (s *DiscoveryService) fetchLocalActionYAML(ctx context.Context, owner, repo, localPath string, errs map[string]error) []byte {
	ymlPath := localPath + "/action.yml"
	data, err := s.githubClient.FetchFileContent(ctx, owner, repo, ymlPath)
	if err != nil {
		errs[fmt.Sprintf("%s/%s/%s", owner, repo, ymlPath)] = err
		return nil
	}
	if data != nil {
		return data
	}

	// Fallback to action.yaml.
	yamlPath := localPath + "/action.yaml"
	data, err = s.githubClient.FetchFileContent(ctx, owner, repo, yamlPath)
	if err != nil {
		errs[fmt.Sprintf("%s/%s/%s", owner, repo, yamlPath)] = err
		return nil
	}
	return data
}
