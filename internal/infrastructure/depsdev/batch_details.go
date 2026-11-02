package depsdev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	neturl "net/url"
	"regexp"
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
			if releaseInfo.HasAnyVersion() {
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
	if releaseInfo.HasAnyVersion() {
		result.ReleaseInfo = releaseInfo
	}

	return result
}

var (
	// repoURLSchemes are the schemes convertRepoURLToProjectKey accepts.
	repoURLSchemes = map[string]bool{
		"http": true, "https": true, "git": true, "ssh": true,
		"git+http": true, "git+https": true, "git+ssh": true,
	}
	// githubOwnerPattern and githubRepoPattern accept only the characters GitHub
	// allows in account and repository names (matched after lowercasing).
	githubOwnerPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)
	githubRepoPattern  = regexp.MustCompile(`^[a-z0-9._-]+$`)
)

// convertRepoURLToProjectKey converts a repository URL to the deps.dev project key
// github.com/<owner>/<repo> (lowercased, ".git" suffix removed).
//
// The host must be exactly github.com or www.github.com (case-insensitive, any port,
// optional trailing dot)
// and the scheme, if any, one of repoURLSchemes; any other host or scheme, or a path
// without valid owner and repository segments, returns "".
// Supported inputs:
//   - https://github.com/owner/repo, http://, git://, git+https:// (optionally with user@ and :port)
//   - ssh://git@github.com/owner/repo, git+ssh://git@github.com/owner/repo
//   - a scheme wrapped around another one, e.g. https://git+ssh://git@github.com/owner/repo
//   - ssh://git@github.com:owner/repo, https://github.com:owner/repo (scp-style path after an allowed scheme)
//   - git@github.com:owner/repo, github.com:owner/repo, with any user@ and with or without www. (scp-style, no scheme)
//   - github.com/owner/repo, git@github.com/owner/repo, //github.com/owner/repo (no scheme)
//   - any of the above behind a Maven SCM prefix: scm:git:..., scm:hg:..., scm:svn:... or a bare scm:
//
// Query strings, fragments, trailing slashes and path segments below owner/repo are ignored.
func convertRepoURLToProjectKey(repoURL string) string {
	s := strings.TrimSpace(repoURL)
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	s = trimMavenSCMPrefix(s)
	if s == "" {
		return ""
	}
	// asciiLower keeps byte offsets in lower valid in s, and never folds a
	// non-ASCII rune (e.g. U+212A KELVIN SIGN) into an ASCII letter.
	lower := asciiLower(s)
	// Peel an outer scheme wrapped around another one (https://git+ssh://...);
	// the inner URL is then parsed and host-checked as a whole.
	for {
		scheme, rest, ok := cutURLScheme(lower)
		inner, _, hasInner := cutURLScheme(rest)
		if !ok || !hasInner || !repoURLSchemes[scheme] || !repoURLSchemes[inner] {
			break
		}
		s = s[len(scheme)+len("://"):]
		lower = rest
	}
	// Only known schemes are trusted, so a host disguised as a scheme
	// (evil.example://github.com/...) or foo://github.com/... yields no key.
	scheme, _, hasScheme := cutURLScheme(lower)
	if hasScheme && !repoURLSchemes[scheme] {
		return ""
	}

	if !hasScheme {
		if path, ok := trimGitHubSCPPrefix(s); ok {
			return githubProjectKeyFromPath(path)
		}
		if !strings.HasPrefix(s, "//") {
			s = "https://" + s
		}
	}

	parsed, err := neturl.Parse(s)
	if err != nil {
		// ssh://git@github.com:owner/repo is not a valid URL ("owner" is not a port),
		// but it is a common way to write an scp-style path behind a scheme.
		if hasScheme {
			if path, ok := trimGitHubSCPPrefix(s[len(scheme)+len("://"):]); ok {
				return githubProjectKeyFromPath(path)
			}
		}
		return ""
	}
	if !common.IsGitHubHost(parsed.Hostname()) {
		return ""
	}
	return githubProjectKeyFromPath(parsed.EscapedPath())
}

// trimMavenSCMPrefix strips a Maven SCM prefix, "scm:<provider>:" or a bare "scm:"
// (scm:git@github.com:o/r is common in POMs), so the URL behind it is checked
// like any other.
func trimMavenSCMPrefix(s string) string {
	lower := asciiLower(s)
	if !strings.HasPrefix(lower, "scm:") {
		return s
	}
	s, lower = s[len("scm:"):], lower[len("scm:"):]
	for _, provider := range []string{"git:", "hg:", "svn:"} {
		if strings.HasPrefix(lower, provider) && !strings.HasPrefix(lower[len(provider)-1:], "://") {
			return s[len(provider):]
		}
	}
	return s
}

// trimGitHubSCPPrefix strips an scp-style "[user@][www.]github.com:" prefix and
// reports whether one was present. As in git, "host:path" without a scheme is
// always scp-style, so github.com:123/repo names owner "123", not port 123.
func trimGitHubSCPPrefix(s string) (string, bool) {
	lower := asciiLower(s)
	if at := strings.IndexByte(lower, '@'); at > 0 && !strings.ContainsAny(lower[:at], "/:") {
		s, lower = s[at+1:], lower[at+1:]
	}
	for _, prefix := range []string{"github.com:", "www.github.com:"} {
		if strings.HasPrefix(lower, prefix) {
			return s[len(prefix):], true
		}
	}
	return "", false
}

// cutURLScheme splits lower at its leading "<scheme>://". It reports false when
// lower does not start with a syntactically valid (RFC 3986) scheme, so a "://"
// further along, e.g. in a path, is not mistaken for one.
func cutURLScheme(lower string) (scheme, rest string, ok bool) {
	scheme, rest, ok = strings.Cut(lower, "://")
	if !ok || scheme == "" || scheme[0] < 'a' || scheme[0] > 'z' ||
		strings.Trim(scheme, "abcdefghijklmnopqrstuvwxyz0123456789+.-") != "" {
		return "", "", false
	}
	return scheme, rest, true
}

// asciiLower lowercases only the ASCII letters A-Z, leaving every other byte as is.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// githubProjectKeyFromPath builds github.com/<owner>/<repo> from the first two
// segments of path, or returns "" when they are missing or not valid GitHub names.
func githubProjectKeyFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	owner := asciiLower(parts[0])
	repo := strings.TrimSuffix(asciiLower(parts[1]), ".git")
	if !githubOwnerPattern.MatchString(owner) || !githubRepoPattern.MatchString(repo) || repo == "." || repo == ".." {
		return ""
	}
	return "github.com/" + owner + "/" + repo
}

// normalizeRepoURLForProject converts any repo URL into canonical form for project lookups:
//   - Uses common.NormalizeRepositoryURL to clean schemes, .git, fragments, etc.
//   - When convertRepoURLToProjectKey yields a key, returns https://<key>
//     (https://github.com/<owner>/<repo>, lowercased, www. dropped)
//   - Otherwise (non-GitHub host or no valid owner/repo), returns the normalized URL unchanged
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
