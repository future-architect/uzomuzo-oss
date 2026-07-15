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

	"github.com/future-architect/uzomuzo-oss/internal/common"
	commonpurl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

// GetDetailsForPURLs fetches detailed information for multiple PURLs with optimized batch processing
// Flow: PURL
// Purpose: Main entry for PURL batch analysis (package, project, releases).
// Called from: integration.IntegrationService.AnalyzeFromPURLs
func (c *DepsDevClient) GetDetailsForPURLs(ctx context.Context, purls []string) (map[string]*BatchResult, error) {
	return c.fetchBatchPURLs(ctx, purls)
}

// fetchBatchPURLs fetches information for multiple PURLs with optimized batch processing
//
// DDD Layer: Infrastructure (orchestration layer - delegates parallel processing)
// Dependencies: Specialized batch functions that handle their own parallelization
// Reuses: Existing patterns from individual functions
func (c *DepsDevClient) fetchBatchPURLs(ctx context.Context, purls []string) (map[string]*BatchResult, error) {
	if len(purls) == 0 {
		return make(map[string]*BatchResult), nil
	}

	// Step 1: Fetch release information for ALL original PURLs in parallel (once per PURL)
	releaseInfoMap, err := c.fetchReleaseInfoBatch(ctx, purls)
	if err != nil {
		slog.Warn("release_info_batch_failed", "error", err)
		releaseInfoMap = make(map[string]ReleaseInfo)
	}

	// Step 2: Resolve effective PURLs to use for data fetching
	// Preference order: Stable -> MaxSemver -> PreRelease -> original
	originalToEffective := make(map[string]string, len(purls))
	effectiveSet := make(map[string]struct{}, len(purls))
	effectivePURLs := make([]string, 0, len(purls))

	for _, orig := range purls {
		eff := orig
		if ri, ok := releaseInfoMap[orig]; ok {
			// Prefer Stable > MaxSemver > PreRelease when choosing effective PURL
			if ri.StableVersion.PURL != "" {
				eff = ri.StableVersion.PURL
			} else if ri.MaxSemverVersion.PURL != "" {
				eff = ri.MaxSemverVersion.PURL
			} else if ri.PreReleaseVersion.PURL != "" {
				eff = ri.PreReleaseVersion.PURL
			}
		}
		originalToEffective[orig] = eff
		if _, seen := effectiveSet[eff]; !seen {
			effectiveSet[eff] = struct{}{}
			effectivePURLs = append(effectivePURLs, eff)
		}
	}

	// Step 3: Fetch package information once per effective PURL (latest view)
	effectivePkgMap, err := c.fetchPackageInfoBatch(ctx, effectivePURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch package info batch (effective_purls=%d): %w", len(effectivePURLs), err)
	}
	if len(effectivePkgMap) == 0 {
		slog.Info("PackageInfoBatchEmpty", "effective_unique_count", len(effectivePURLs))
	}

	// Re-key package info back to original PURLs so downstream logic stays unchanged
	packageInfoMap := make(map[string]*PackageResponse, len(purls))
	missingPkgCount := 0
	for _, orig := range purls {
		eff := originalToEffective[orig]
		if pkg, ok := effectivePkgMap[eff]; ok {
			packageInfoMap[orig] = pkg
		} else {
			missingPkgCount++
		}
	}
	if missingPkgCount > 0 && float64(missingPkgCount) >= 0.05*float64(len(purls)) {
		slog.Debug("Missing package info for some PURLs", "missing_count", missingPkgCount, "total", len(purls))
	}

	// Step 4: Extract repository URLs and group by unique repos (keyed by original PURLs)
	repoURLMap, purlsWithoutRepo := c.resolveRepoURLsBatch(ctx, packageInfoMap)
	if len(purlsWithoutRepo) > 0 {
		slog.Info("PURLsWithoutRepoURL", "count", len(purlsWithoutRepo))
	}

	// Step 4.1: Fallback repo resolution for ecosystems unsupported by deps.dev package API
	// or when package info is completely missing for a PURL. Reuse existing resolvers
	// where applicable and add a Go-specific module-root-to-GitHub synthesis.
	for _, orig := range purls {
		if _, ok := packageInfoMap[orig]; ok {
			continue // already processed via deps.dev package info
		}
		parser := commonpurl.NewParser()
		parsed, err := parser.Parse(orig)
		if err != nil {
			slog.Debug("Fallback repo resolution skipped (parse failed)", "purl", orig, "error", err)
			continue
		}
		eco := strings.ToLower(strings.TrimSpace(parsed.Ecosystem()))

		// Go-specific fallback: synthesize repository URL (module root or static fallback) centrally
		if eco == "golang" && c.goproxy != nil {
			if repo := attemptGoRepoURLFromPackageName(ctx, c.goproxy, parsed.PackageName()); repo != "" {
				repoURLMap[repo] = append(repoURLMap[repo], orig)
				slog.Debug("Fallback repo resolved (golang)", "purl", orig, "repo", repo)
			}
			continue
		}

		// Registry fallback via resolvers for ecosystems that support repo URL resolution
		if !hasRegistryResolver(eco) {
			// Silent skip: CLI final summary already lists unresolved / unsupported PURLs; avoid per-item noise.
			continue
		}

		// Build synthetic PackageResponse so registry resolvers can operate.
		synthetic := &PackageResponse{
			Version: Version{
				PURL: orig,
				VersionKey: VersionKey{
					System:  parsed.Ecosystem(),
					Name:    parsed.PackageName(),
					Version: strings.TrimSpace(parsed.Version()),
				},
			},
		}

		if repo := c.resolveRepoURL(ctx, synthetic, orig); repo != "" {
			repo = normalizeRepoURLForProject(repo)
			if repo != "" {
				repoURLMap[repo] = append(repoURLMap[repo], orig)
				slog.Debug("Fallback repo resolved via resolvers", "purl", orig, "url", repo)
			}
		}
	}

	// Step 5: Fetch project information using batch API
	repoURLs := make([]string, 0, len(repoURLMap))
	for repoURL := range repoURLMap {
		repoURLs = append(repoURLs, repoURL)
	}

	projectInfoMap, err := c.fetchProjectsBatch(ctx, repoURLs)
	if err != nil {
		slog.Warn("project_batch_failed", "error", err)
		projectInfoMap = make(map[string]*Project)
	}
	if len(projectInfoMap) == 0 && len(repoURLs) > 0 {
		slog.Warn("ProjectBatchEmptyForRepos", "repo_count", len(repoURLs))
	}

	// Step 6: Build final results (keyed by original PURLs)
	return c.buildFinalResults(purls, packageInfoMap, purlsWithoutRepo, repoURLMap, projectInfoMap, releaseInfoMap), nil
}

// resolveRepoURLsBatch resolves repository URLs for many PURLs concurrently and returns:
// - repoURLMap: normalized repo URL -> list of original PURLs that map to it
// - purlsWithoutRepo: list of PURLs for which no repo URL could be determined
//
// DDD Layer: Infrastructure (parallel processing)
// Notes:
// - Bounded concurrency to avoid overwhelming registries (Maven Central, etc.)
// - Reuses existing resolver chain (which may perform network I/O) safely across goroutines
func (c *DepsDevClient) resolveRepoURLsBatch(ctx context.Context, packageInfoMap map[string]*PackageResponse) (map[string][]string, []string) {
	if len(packageInfoMap) == 0 {
		return make(map[string][]string), nil
	}

	// Tunable but conservative parallelism; matches other batch helpers
	const maxWorkers = 10

	repoURLMap := make(map[string][]string)
	purlsWithoutRepo := make([]string, 0)

	// Collect keys to iterate deterministically
	keys := make([]string, 0, len(packageInfoMap))
	for k := range packageInfoMap {
		keys = append(keys, k)
	}

	// Work queue
	jobs := make(chan string, len(keys))
	var mu sync.Mutex
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for p := range jobs {
			pkg := packageInfoMap[p]
			url := c.resolveRepoURL(ctx, pkg, p)
			if url != "" {
				url = normalizeRepoURLForProject(url)
			}
			// Small-step 2: If repo URL still missing and ecosystem is golang, attempt a best-effort fallback
			if url == "" {
				if pr, err := purlpkgToParsed(p); err == nil && pr != nil && strings.EqualFold(pr.Ecosystem(), "golang") {
					if unescName, err := neturl.PathUnescape(pr.PackageName()); err == nil && unescName != "" {
						url = synthesizeGoGitHubRepoURL(ctx, c.goproxy, unescName)
					}
				}
			}
			mu.Lock()
			if url == "" {
				purlsWithoutRepo = append(purlsWithoutRepo, p)
			} else {
				repoURLMap[url] = append(repoURLMap[url], p)
			}
			mu.Unlock()
		}
	}

	// Start workers (bounded by keys size)
	workers := maxWorkers
	if workers > len(keys) {
		workers = len(keys)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker()
	}
	for _, k := range keys {
		jobs <- k
	}
	close(jobs)
	wg.Wait()

	return repoURLMap, purlsWithoutRepo
}

// repoURLFromRelatedProjects inspects deps.dev RelatedProjects and returns a normalized repo URL when possible.
func repoURLFromRelatedProjects(related []RelatedProject) string {
	for _, rp := range related {
		key := strings.ToLower(strings.TrimSpace(rp.ProjectKey.ID))
		if key == "" {
			continue
		}
		// deps.dev uses host/path form, e.g., github.com/owner/repo
		if strings.HasPrefix(key, "github.com/") {
			return "https://" + strings.TrimRight(key, "/")
		}
	}
	return ""
}

// isGemSystem returns true if the deps.dev system/ecosystem maps to RubyGems.
func isGemSystem(system string) bool {
	switch strings.ToLower(strings.TrimSpace(system)) {
	case "gem", "rubygems":
		return true
	default:
		return false
	}
}

// fetchPackageInfoBatch fetches package information for multiple PURLs with internal parallelization
func (c *DepsDevClient) fetchPackageInfoBatch(ctx context.Context, purls []string) (map[string]*PackageResponse, error) {
	// Pre-flight: count suspicious Maven PURLs for a single summary warning
	suspiciousMavenCount := countSuspiciousMavenPURLs(purls)

	const maxWorkers = 10
	results := collectBounded[*PackageResponse](ctx, purls, maxWorkers, func(ctx context.Context, p string) (string, *PackageResponse, bool) {
		packageResp, err := c.fetchPackageInfo(ctx, p)
		if err != nil {
			slog.Debug("Failed to fetch package info", "purl", p, "error", err)
			return "", nil, false
		}
		return p, packageResp, true
	})

	if suspiciousMavenCount > 0 {
		slog.Warn("Suspicious Maven PURLs detected — namespace (groupId) may be missing or incorrect (set LOG_LEVEL=debug for details)",
			"count", suspiciousMavenCount,
			"hint", "Maven PURLs must be pkg:maven/<groupId>/<artifactId>@<version>",
		)
	}
	slog.Debug("Package info batch completed", "requested", len(purls), "successful", len(results))
	return results, nil
}

// countSuspiciousMavenPURLs counts Maven PURLs with missing or suspicious namespace.
func countSuspiciousMavenPURLs(purls []string) int {
	count := 0
	for _, p := range purls {
		pr, err := purlpkgToParsed(p)
		if err != nil || pr == nil || !strings.EqualFold(pr.Ecosystem(), "maven") {
			continue
		}
		ns := strings.TrimSpace(pr.Namespace())
		n := strings.TrimSpace(pr.Name())
		// Apply same normalization as fetchPackageInfo
		normalized := commonpurl.NormalizeMavenCollapsedCoordinates(p)
		if normalized != p {
			if pr2, err2 := purlpkgToParsed(normalized); err2 == nil && pr2 != nil {
				ns = strings.TrimSpace(pr2.Namespace())
				n = strings.TrimSpace(pr2.Name())
			}
		}
		if ns == "" || strings.EqualFold(ns, n) || !strings.Contains(ns, ".") {
			count++
		}
	}
	return count
}

// fetchProjectsBatch fetches project information for multiple repository URLs using batch API
func (c *DepsDevClient) fetchProjectsBatch(ctx context.Context, repoURLs []string) (map[string]*Project, error) {
	if len(repoURLs) == 0 {
		return make(map[string]*Project), nil
	}

	// Convert repository URLs to project keys
	projectKeys := make([]string, 0, len(repoURLs))
	repoToKeyMap := make(map[string]string)

	for _, repoURL := range repoURLs {
		projectKey := convertRepoURLToProjectKey(repoURL)
		if projectKey != "" {
			projectKeys = append(projectKeys, projectKey)
			repoToKeyMap[repoURL] = projectKey
		}
	}

	if len(projectKeys) == 0 {
		return make(map[string]*Project), nil
	}

	// Helper: perform a single paginated batch call for a slice of project keys
	doPaginatedBatch := func(ctx context.Context, keys []string) (map[string]*Project, error) {
		accumulated := make(map[string]*Project)
		pageToken := ""
		page := 1
		for {
			body := ProjectBatchRequest{
				Requests:  make([]ProjectRequest, 0, len(keys)),
				PageToken: pageToken,
			}
			for _, k := range keys {
				body.Requests = append(body.Requests, ProjectRequest{ProjectKey: ProjectKey{ID: k}})
			}
			b, err := json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal batch request (page=%d): %w", page, err)
			}
			url := fmt.Sprintf("%s/projectbatch", c.baseURL)
			req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(b)))
			if err != nil {
				return nil, fmt.Errorf("failed to create batch request (page=%d, url=%s): %w", page, url, err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "uzomuzo-depsdev-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
			resp, err := c.client.Do(ctx, req)
			if err != nil {
				slog.Debug("deps.dev HTTP batch request failed", "method", "POST", "url", url, "page", page, "error", err)
				return nil, fmt.Errorf("HTTP batch request failed (page=%d, url=%s): %w", page, url, err)
			}
			if resp.StatusCode != http.StatusOK {
				bodyBytes, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close() // best-effort cleanup
				snippet := truncateString(string(bodyBytes), 1024)
				slog.Debug("deps.dev project batch non-OK response", "method", "POST", "url", url, "page", page, "status", resp.StatusCode, "body_snippet", snippet)
				return nil, fmt.Errorf("HTTP %d (page=%d, url=%s): %s", resp.StatusCode, page, url, snippet)
			}
			var projectResp ProjectBatchResponse
			if err := json.NewDecoder(resp.Body).Decode(&projectResp); err != nil {
				_ = resp.Body.Close() // best-effort cleanup
				slog.Debug("deps.dev project batch JSON decode failed", "method", "POST", "url", url, "page", page, "error", err)
				return nil, fmt.Errorf("JSON decode failed (url=%s, page=%d): %w", url, page, err)
			}
			_ = resp.Body.Close() // close explicitly per iteration to avoid resource accumulation
			for _, response := range projectResp.Responses {
				if response.Project != nil {
					key := strings.ToLower(response.Project.ProjectKey.ID)
					accumulated[key] = response.Project
				}
			}
			if projectResp.NextPageToken == "" {
				break
			}
			pageToken = projectResp.NextPageToken
			page++
		}

		return accumulated, nil
	}

	// Chunk project keys to respect API limits; reuse configured BatchSize with a sane upper cap
	chunkSize := c.config.BatchSize
	if chunkSize <= 0 {
		chunkSize = 100
	}
	if chunkSize > 200 {
		chunkSize = 200
	}

	// Accumulate all project results across chunks
	allProjectsByKey := make(map[string]*Project)
	for start := 0; start < len(projectKeys); start += chunkSize {
		end := start + chunkSize
		if end > len(projectKeys) {
			end = len(projectKeys)
		}
		keysChunk := projectKeys[start:end]

		chunkResults, err := doPaginatedBatch(ctx, keysChunk)
		if err != nil {
			// Log and continue with other chunks
			slog.Warn("ProjectBatchChunkFailed", "start", start, "end", end-1, "size", len(keysChunk), "error", err)
			continue
		}
		for k, v := range chunkResults {
			allProjectsByKey[k] = v
		}
	}

	// Map results back to repository URLs
	results := make(map[string]*Project)
	for repoURL, key := range repoToKeyMap {
		if p, ok := allProjectsByKey[strings.ToLower(key)]; ok {
			results[repoURL] = p
		}
	}

	return results, nil
}

// buildFinalResults builds final results without any API calls (pure data assembly)
func (c *DepsDevClient) buildFinalResults(purls []string, packageInfoMap map[string]*PackageResponse, purlsWithoutRepo []string, repoURLMap map[string][]string, projectInfoMap map[string]*Project, releaseInfoMap map[string]ReleaseInfo) map[string]*BatchResult {
	results := make(map[string]*BatchResult)

	// Process PURLs without repository information
	for _, purl := range purlsWithoutRepo {
		packageResp := packageInfoMap[purl]
		basicResult := c.buildBasicResult(purl, packageResp)
		if releaseInfo, exists := releaseInfoMap[purl]; exists {
			if releaseInfo.StableVersion.VersionKey.Version != "" || releaseInfo.PreReleaseVersion.VersionKey.Version != "" {
				basicResult.ReleaseInfo = releaseInfo
			}
		}
		results[purl] = basicResult
	}

	// Process PURLs with repository information
	for repoURL, purlList := range repoURLMap {
		projectInfo := projectInfoMap[repoURL] // May be nil if batch fetch failed

		for _, purl := range purlList {
			packageResp := packageInfoMap[purl]
			releaseInfo := releaseInfoMap[purl] // May be empty if fetch failed

			result := c.buildCompleteResult(purl, packageResp, projectInfo, releaseInfo)
			// Persist the resolved repository URL so upper layers can use it directly
			result.RepoURL = repoURL
			results[purl] = result

		}
	}
	// Mark any completely missing entries as not found to aid upstream handling
	for _, purl := range purls {
		if _, ok := results[purl]; ok {
			continue
		}
		// If we have neither package info nor release info, surface as not found
		if _, ok := packageInfoMap[purl]; !ok {
			if ri, ok := releaseInfoMap[purl]; !ok || ri.Error != nil {
				msg := "package not found in deps.dev"
				s := msg
				results[purl] = &BatchResult{PURL: purl, Error: &s}
			}
		}
	}
	return results
}

// buildBasicResult creates a BatchResult with basic package information only
func (c *DepsDevClient) buildBasicResult(purl string, packageResp *PackageResponse) *BatchResult {
	result := &BatchResult{PURL: purl}
	if packageResp != nil {
		result.Package = &Package{
			PURL:     packageResp.Version.PURL,
			Versions: []Version{packageResp.Version},
		}
	}
	return result
}

// buildCompleteResult creates a BatchResult with all available information
func (c *DepsDevClient) buildCompleteResult(purl string, packageResp *PackageResponse, projectInfo *Project, releaseInfo ReleaseInfo) *BatchResult {
	result := &BatchResult{PURL: purl, Project: projectInfo}
	if packageResp != nil {
		result.Package = &Package{
			PURL:     packageResp.Version.PURL,
			Versions: []Version{packageResp.Version},
		}
	}

	// Add release information if available
	if releaseInfo.StableVersion.VersionKey.Version != "" || releaseInfo.PreReleaseVersion.VersionKey.Version != "" {
		result.ReleaseInfo = releaseInfo
	}

	return result
}

// convertRepoURLToProjectKey converts repository URL to deps.dev project key format
func convertRepoURLToProjectKey(repoURL string) string {
	// Normalize and extract the GitHub owner/repo into github.com/owner/repo
	// Supports inputs like:
	// - https://github.com/owner/repo
	// - http://github.com/owner/repo
	// - github.com/owner/repo
	// - git+ssh://git@github.com/owner/repo
	// - ssh://git@github.com/owner/repo
	// - git@github.com:owner/repo
	s := strings.TrimSpace(repoURL)
	if s == "" {
		return ""
	}

	for {
		lower := strings.ToLower(s)
		idx := strings.Index(lower, "://")
		if idx == -1 {
			break
		}
		rest := s[idx+len("://"):]
		restLower := strings.ToLower(rest)
		if strings.HasPrefix(restLower, "git+ssh://") ||
			strings.HasPrefix(restLower, "ssh://") ||
			strings.HasPrefix(restLower, "git@github.com:") ||
			strings.HasPrefix(restLower, "git@github.com/") {
			s = rest
			continue
		}
		break
	}

	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "git@github.com:") {
		path := s[len("git@github.com:"):]
		return githubProjectKeyFromPath(path)
	}
	if strings.HasPrefix(lower, "git@github.com/") {
		s = s[len("git@"):]
		lower = strings.ToLower(s)
	}
	if strings.HasPrefix(lower, "github.com/") || strings.HasPrefix(lower, "github.com:") {
		s = "https://" + s
	}

	parsed, err := neturl.Parse(s)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return ""
	}
	return githubProjectKeyFromPath(parsed.Path)
}

func githubProjectKeyFromPath(path string) string {
	if i := strings.Index(path, "#"); i >= 0 {
		path = path[:i]
	}
	if i := strings.Index(path, "?"); i >= 0 {
		path = path[:i]
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	owner := strings.ToLower(parts[0])
	repo := strings.ToLower(strings.TrimSuffix(parts[1], ".git"))
	if owner == "" || repo == "" {
		return ""
	}
	return "github.com/" + owner + "/" + repo
}

// normalizeRepoURLForProject converts any repo URL into canonical form for project lookups:
// - Uses common.NormalizeRepositoryURL to clean schemes, .git, fragments, etc.
// - For GitHub URLs, trims to https://github.com/<owner>/<repo>
// - For non-GitHub URLs, returns the normalized URL unchanged
func normalizeRepoURLForProject(raw string) string {
	if raw == "" {
		return ""
	}
	norm := common.NormalizeRepositoryURL(raw)
	if key := convertRepoURLToProjectKey(norm); key != "" {
		return "https://" + key
	}
	return norm
}
