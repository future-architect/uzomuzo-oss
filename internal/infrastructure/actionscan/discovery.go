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

// maxFileFetchConcurrency limits concurrent workflow file fetches within a single repository.
// Kept small to avoid bursting the GitHub API when combined with repo-level concurrency.
const maxFileFetchConcurrency = 5

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
// Returns:
//   - directURLs: actions referenced directly in workflow files
//   - localActions: actions found inside local composite actions (URL → local path)
//   - transitiveActions: actions found via composite action BFS (URL → parent action URL)
//
// The returned slices are sorted lexicographically for deterministic output.
// This method satisfies scan.ActionsDiscoverer.
func (s *DiscoveryService) DiscoverActions(ctx context.Context, repoURLs []string, resolveTransitive bool) (directURLs []string, localActions map[string]string, transitiveActions map[string]string, errors map[string]error, err error) {
	result := &DiscoveryResult{
		Actions: make(map[string]int),
		Errors:  make(map[string]error),
	}

	localActions = make(map[string]string)

	// Phase 1: Discover direct and local actions from workflow files.
	// Collect per-repo local actions separately, then merge deterministically
	// after all goroutines complete (sorted by repoURL for stable Via provenance).
	type repoLocalResult struct {
		repoURL      string
		localActions map[string]string
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	var perRepoLocals []repoLocalResult
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

			urls, repoLocalActions, errs := s.discoverFromRepo(ctx, owner, repo)

			mu.Lock()
			for _, u := range urls {
				if _, exists := result.Actions[u]; !exists {
					result.Actions[u] = 0
					directURLs = append(directURLs, u)
				}
			}
			if len(repoLocalActions) > 0 {
				perRepoLocals = append(perRepoLocals, repoLocalResult{
					repoURL:      repoURL,
					localActions: repoLocalActions,
				})
			}
			for k, v := range errs {
				result.Errors[k] = v
			}
			mu.Unlock()
		}(repoURL)
	}

	wg.Wait()

	// Merge per-repo local actions deterministically: sort by repoURL so
	// "first-seen wins" Via provenance is stable across runs.
	sort.Slice(perRepoLocals, func(i, j int) bool {
		return perRepoLocals[i].repoURL < perRepoLocals[j].repoURL
	})
	for _, prl := range perRepoLocals {
		for u, localPath := range prl.localActions {
			if _, exists := result.Actions[u]; !exists {
				result.Actions[u] = 0
				localActions[u] = localPath
			}
		}
	}

	// Sort before transitive resolution so BFS seed order is deterministic.
	sort.Strings(directURLs)

	// Phase 2: Resolve transitive composite action dependencies via BFS (opt-in).
	// Seed BFS with both direct and local-discovered action URLs.
	if resolveTransitive {
		allSeedURLs := make([]string, 0, len(directURLs)+len(localActions))
		allSeedURLs = append(allSeedURLs, directURLs...)
		for u := range localActions {
			allSeedURLs = append(allSeedURLs, u)
		}
		sort.Strings(allSeedURLs)
		transitiveActions = s.resolveTransitiveActions(ctx, allSeedURLs, result)
	}

	slog.Info("actions discovery complete",
		"repos_scanned", len(repoURLs),
		"direct_actions", len(directURLs),
		"local_actions", len(localActions),
		"transitive_actions", len(transitiveActions),
		"errors", len(result.Errors),
	)

	return directURLs, localActions, transitiveActions, result.Errors, nil
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
//
// Returns:
//   - directURLs: external action URLs referenced directly in workflow files
//   - localActions: external action URLs found inside local composite actions (URL → local path)
//   - errs: non-fatal errors keyed by source path
func (s *DiscoveryService) discoverFromRepo(ctx context.Context, owner, repo string) (directURLs []string, localActions map[string]string, errs map[string]error) {
	errs = make(map[string]error)

	entries, err := s.githubClient.FetchDirectoryContents(ctx, owner, repo, ".github/workflows")
	if err != nil {
		errs[fmt.Sprintf("%s/%s/.github/workflows", owner, repo)] = err
		return nil, nil, errs
	}
	if entries == nil {
		slog.Debug("no .github/workflows directory", "owner", owner, "repo", repo)
		return nil, nil, nil
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
		return nil, nil, nil
	}

	slog.Debug("fetching workflow files", "owner", owner, "repo", repo, "count", len(yamlFiles))

	seen := make(map[string]struct{})
	var localPaths []string
	localSeen := make(map[string]struct{})

	// Fetch and parse workflow files concurrently to reduce sequential API latency.
	var (
		fileMu  sync.Mutex
		fileWG  sync.WaitGroup
		fileSem = make(chan struct{}, maxFileFetchConcurrency)
	)

	for _, yf := range yamlFiles {
		fileWG.Add(1)
		go func(yf github.DirectoryEntry) {
			defer fileWG.Done()
			fileSem <- struct{}{}
			defer func() { <-fileSem }()

			data, fetchErr := s.githubClient.FetchFileContent(ctx, owner, repo, yf.Path)
			if fetchErr != nil {
				fileMu.Lock()
				errs[fmt.Sprintf("%s/%s/%s", owner, repo, yf.Path)] = fetchErr
				fileMu.Unlock()
				return
			}
			if data == nil {
				return // 404 — file disappeared between listing and fetch
			}

			urls, locals, parseErr := ghaworkflow.ParseWorkflowAll(data)
			if parseErr != nil {
				fileMu.Lock()
				errs[fmt.Sprintf("%s/%s/%s", owner, repo, yf.Path)] = parseErr
				fileMu.Unlock()
				return
			}

			fileMu.Lock()
			for _, u := range urls {
				if _, exists := seen[u]; !exists {
					seen[u] = struct{}{}
					directURLs = append(directURLs, u)
				}
			}
			for _, lp := range locals {
				if _, exists := localSeen[lp]; !exists {
					localSeen[lp] = struct{}{}
					localPaths = append(localPaths, lp)
				}
			}
			fileMu.Unlock()
		}(yf)
	}

	fileWG.Wait()

	// Resolve local composite actions to find external action dependencies.
	// Sort for deterministic BFS "first-seen wins" Via provenance.
	if len(localPaths) > 0 {
		sort.Strings(localPaths)
		localActions = s.resolveLocalActions(ctx, owner, repo, localPaths, errs)
	}

	return directURLs, localActions, errs
}

// resolveLocalActions performs BFS over local composite actions within a repository,
// fetching action.yml for each local path, extracting external action URLs, and
// following nested local references (uses: ./) with cycle detection.
// It returns a map of external GitHub URL → local action path that contained it (first-seen wins).
func (s *DiscoveryService) resolveLocalActions(ctx context.Context, owner, repo string, initialPaths []string, errs map[string]error) map[string]string {
	seen := make(map[string]struct{})
	for _, p := range initialPaths {
		seen[p] = struct{}{}
	}

	queue := append([]string(nil), initialPaths...)
	// external URL → originating local action path (first-seen wins)
	externalActions := make(map[string]string)

	for len(queue) > 0 {
		current := queue
		queue = nil

		for _, localPath := range current {
			data := s.fetchLocalActionYAML(ctx, owner, repo, localPath, errs)
			if data == nil {
				continue
			}

			// Extract both external refs and nested local paths from a single unmarshal.
			refs, nestedLocals, isComposite, err := ghaworkflow.ParseCompositeAll(data)
			if err != nil {
				errs[fmt.Sprintf("%s/%s/%s", owner, repo, localPath)] = err
				continue
			}
			if !isComposite {
				continue
			}

			for _, ref := range refs {
				ghURL := ref.GitHubURL()
				if _, exists := externalActions[ghURL]; !exists {
					externalActions[ghURL] = localPath
				}
			}

			for _, nested := range nestedLocals {
				if _, exists := seen[nested]; !exists {
					seen[nested] = struct{}{}
					queue = append(queue, nested)
				}
			}
		}
	}

	if len(externalActions) > 0 {
		slog.Debug("resolved local composite actions",
			"owner", owner, "repo", repo,
			"local_actions_scanned", len(seen),
			"external_urls_found", len(externalActions),
		)
	}

	return externalActions
}

// fetchLocalActionYAML fetches action.yml (or action.yaml as fallback) for a local action
// path within a repository (e.g., ".github/actions/foo" → fetch ".github/actions/foo/action.yml").
// It returns nil if neither file exists. Errors are recorded in errs.
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
