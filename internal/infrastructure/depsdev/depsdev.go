package depsdev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	neturl "net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	commonpurl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/goproxy"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/maven"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/npmjs"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/nuget"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/packagist"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/pypi"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/rubygems"
)

// DepsDevClient implements the deps.dev API client.
//
// DDD Layer: Infrastructure
// Responsibility: Call deps.dev v3alpha endpoints to retrieve package/project/release info.
//
// Authoritative docs:
//   - API reference: https://docs.deps.dev/api/v3alpha/
//   - PURL endpoint:  GET /v3alpha/purl/{purl}
//   - Systems/Packages endpoint (versions): GET /v3alpha/systems/{system}/packages/{name}
//   - Project batch endpoint: POST /v3alpha/projectbatch (paginated via nextPageToken)
//
// Concrete examples (public API host: https://api.deps.dev):
//
//   - PURL endpoint (URL-escaped PURL path segment):
//     PURL: pkg:npm/lodash@4.17.21
//     GET  https://api.deps.dev/v3alpha/purl/pkg%3Anpm%2Flodash%404.17.21
//
//   - Systems/Packages endpoint (versions listing):
//     NPM      → GET https://api.deps.dev/v3alpha/systems/NPM/packages/lodash
//     NUGET    → GET https://api.deps.dev/v3alpha/systems/NUGET/packages/newtonsoft.json
//     RUBYGEMS → GET https://api.deps.dev/v3alpha/systems/RUBYGEMS/packages/rails
//
//   - Project batch endpoint (single page request):
//     POST https://api.deps.dev/v3alpha/projectbatch
//     Body:
//     {
//     "requests": [
//     { "projectKey": { "id": "github.com/lodash/lodash" } },
//     { "projectKey": { "id": "github.com/serilog/serilog" } }
//     ]
//     }
//     The response may include nextPageToken; clients should repeat the request with that token
//     until it is empty to retrieve all results for large batches.
//
// Key behaviors implemented:
//   - Release selection: derive Stable, PreRelease, and MaxSemver from versions
//   - Repository URL extraction priority from Package.Project.RelatedProjects and Links:
//     1) SOURCE_REPO link if present
//     2) Any link that normalizes to a valid GitHub URL
//   - Project batch pagination: handle NextPageToken across pages

// errPURLNotFound is a sentinel error returned by fetchPURLRaw when deps.dev
// returns 404 for a PURL lookup. Used by fetchPackageInfo to trigger fallback
// logic via errors.Is rather than fragile string matching.
var errPURLNotFound = errors.New("deps.dev PURL not found")

type DepsDevClient struct {
	baseURL string
	client  *httpclient.Client
	config  *config.DepsDevConfig
	// optional helpers
	rubygems  *rubygems.Client
	packagist *packagist.Client
	npm       *npmjs.Client
	nuget     *nuget.Client
	maven     *maven.Client
	pypi      *pypi.Client
	goproxy   *goproxy.Client
}

// NewDepsDevClient creates a new DepsDevClient configured with the provided settings.
// It sets up an HTTP client with retries and composes the base API URL.
func NewDepsDevClient(cfg *config.DepsDevConfig) *DepsDevClient {
	// HTTP client configuration
	httpClient := &http.Client{
		Timeout: cfg.Timeout,
	}

	// Retry configuration
	retryConfig := httpclient.RetryConfig{
		MaxRetries:        cfg.MaxRetries,
		BaseBackoff:       1 * time.Second,
		MaxBackoff:        30 * time.Second,
		RetryOn5xx:        true,
		RetryOnNetworkErr: true,
	}

	client := httpclient.NewClient(httpClient, retryConfig)

	return &DepsDevClient{
		baseURL: cfg.BaseURL + "/v3alpha",
		client:  client,
		config:  cfg,
		goproxy: goproxy.NewClient(),
	}
}

// WithRubyGems enables a RubyGems client for fallback resolution (used in wiring/tests).
func (c *DepsDevClient) WithRubyGems(g *rubygems.Client) *DepsDevClient {
	c.rubygems = g
	return c
}

// WithPackagist enables a Packagist client for fallback resolution (used in wiring/tests).
func (c *DepsDevClient) WithPackagist(p *packagist.Client) *DepsDevClient {
	c.packagist = p
	return c
}

// WithNPM enables an npmjs client for fallback resolution (used in wiring/tests).
func (c *DepsDevClient) WithNPM(n *npmjs.Client) *DepsDevClient {
	c.npm = n
	return c
}

// WithNuGet enables a NuGet client for fallback resolution (used in wiring/tests).
func (c *DepsDevClient) WithNuGet(n *nuget.Client) *DepsDevClient {
	c.nuget = n
	return c
}

// WithMaven enables a Maven client for fallback resolution (used in wiring/tests).
func (c *DepsDevClient) WithMaven(m *maven.Client) *DepsDevClient {
	c.maven = m
	return c
}

// WithPyPI enables a PyPI client for fallback resolution (used in wiring/tests).
func (c *DepsDevClient) WithPyPI(p *pypi.Client) *DepsDevClient {
	c.pypi = p
	return c
}

// ===== Flow: GitHub URL helpers (used only in GitHub→PURL completion) =====

// GetLatestReleasesForPURLs fetches latest release information for multiple PURLs
// Flow: GitHub URL
// Purpose: Used by the GitHub URL flow to resolve default/latest versions for base PURLs.
// Called from: integration.IntegrationService.AnalyzeFromGitHubURL
func (c *DepsDevClient) GetLatestReleasesForPURLs(ctx context.Context, purls []string) (map[string]*ReleaseInfo, error) {
	results := make(map[string]*ReleaseInfo)
	resultMutex := sync.Mutex{}

	// Limit the number of goroutines
	const maxWorkers = 10

	// Split processing into batches with progress tracking
	processedCount := 0
	for batchStart := 0; batchStart < len(purls); batchStart += c.config.BatchSize {
		batchEnd := batchStart + c.config.BatchSize
		if batchEnd > len(purls) {
			batchEnd = len(purls)
		}

		batch := purls[batchStart:batchEnd]
		// Log initial batch processing message only for actual batch processing (multiple items)
		if len(purls) > 1 {
			if batchStart == 0 {
				slog.Debug("Starting PURL batch processing", "total", len(purls), "batch_size", c.config.BatchSize)
			}
		}
		if batchStart == 0 || batchEnd == len(purls) {
			slog.Debug("Processing PURL batch",
				"batch_start", batchStart,
				"batch_end", batchEnd-1,
				"total_purls", len(purls))
		}

		// Parallel processing within batch
		semaphore := make(chan struct{}, maxWorkers)
		var wg sync.WaitGroup

		for _, purl := range batch {
			wg.Add(1)
			go func(p string) {
				defer wg.Done()
				semaphore <- struct{}{}        // Acquire semaphore
				defer func() { <-semaphore }() // Release semaphore

				releaseInfo, _ := c.fetchLatestRelease(ctx, p) // Intentionally ignore error: details captured in releaseInfo.Error

				resultMutex.Lock()
				results[p] = &releaseInfo
				processedCount++
				currentProcessed := processedCount
				resultMutex.Unlock()

				// Log processing progress only for actual batch processing
				if len(purls) > 1 && (currentProcessed%100 == 0 || currentProcessed == len(purls)) {
					slog.Debug("Processing progress", "completed", currentProcessed, "total", len(purls))
				}
			}(purl)
		}

		wg.Wait()

		// Log batch completion only for actual batch processing
		if len(purls) > 1 && batchEnd < len(purls) {
			slog.Debug("Batch completed", "processed", processedCount, "total", len(purls))
		}
	}

	// Log final completion message only for actual batch processing
	if len(purls) > 1 {
		slog.Debug("PURL processing completed", "processed", len(purls), "total", len(purls))
	}

	return results, nil
}

// fetchLatestRelease fetches the latest release information for the specified PURL
func (c *DepsDevClient) fetchLatestRelease(ctx context.Context, purlStr string) (ReleaseInfo, error) {
	parser := commonpurl.NewParser()
	parsed, err := parser.Parse(purlStr)
	if err != nil {
		return ReleaseInfo{
			Error: fmt.Errorf("failed to parse PURL: %w", err),
		}, err
	}

	// Map PURL ecosystem and package name to deps.dev expectations (breaking simplified API)
	system, name := toDepsDevSystemAndName(parsed)
	origSystem, origName := system, name // capture before any normalization so we can log only when changed

	// Track normalized module name locally (avoid context misuse for intra-function data)
	normalizedRawName := parsed.GetPackageName()
	if strings.EqualFold(parsed.GetEcosystem(), "golang") {
		// Use helper to encapsulate Go-specific normalization logic for versions endpoint.
		norm := normalizeGoModuleForVersions(ctx, c.goproxy, parsed)
		switch norm.Strategy {
		case "proxy", "fallback", "fallback-no-proxy":
			if norm.Changed {
				slog.Debug("deps.dev: go module name normalized", "strategy", norm.Strategy, "from", name, "to", norm.ModuleRootRaw)
			}
			name = norm.EscapedName
			normalizedRawName = norm.ModuleRootRaw
		}
	}
	// Maven PURL validation is done in fetchPackageInfo after normalization
	// to avoid duplicate warnings.

	// Log only when mapping changed (noise reduction for high-volume batches)
	if system != origSystem || name != origName {
		fields := []any{"purl", purlStr, "system", system, "name", name, "from_system", origSystem, "from_name", origName}
		if strings.EqualFold(parsed.GetEcosystem(), "golang") && normalizedRawName != parsed.GetPackageName() {
			fields = append(fields, "normalized_raw", normalizedRawName)
		}
		slog.Debug("deps.dev versions endpoint mapping changed", fields...)
	}
	endpoint := fmt.Sprintf("%s/systems/%s/packages/%s", c.baseURL, system, name)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		slog.Debug("deps.dev request creation failed", "method", "GET", "url", endpoint, "error", err)
		return ReleaseInfo{
			Endpoint: endpoint,
			Error:    fmt.Errorf("failed to create request (url=%s): %w", endpoint, err),
		}, err
	}
	// Set descriptive User-Agent for deps.dev requests
	req.Header.Set("User-Agent", "uzomuzo-depsdev-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")

	resp, err := c.client.Do(ctx, req)
	if err != nil {
		slog.Debug("deps.dev HTTP request failed", "method", "GET", "url", endpoint, "error", err)
		return ReleaseInfo{
			Endpoint: endpoint,
			Error:    fmt.Errorf("HTTP request failed (url=%s): %w", endpoint, err),
		}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Treat 404 as benign: deps.dev may not index certain Go forks or packages; continue without versions.
		if resp.StatusCode == http.StatusNotFound {
			slog.Debug("deps.dev versions endpoint returned 404", "method", "GET", "url", endpoint)
			return ReleaseInfo{Endpoint: endpoint}, nil
		}
		// Other status codes remain errors.
		body, _ := io.ReadAll(resp.Body)
		snippet := truncateString(string(body), 1024)
		derr := fmt.Errorf("HTTP %d %s (url=%s): %s", resp.StatusCode, resp.Status, endpoint, snippet)
		slog.Debug("deps.dev versions endpoint non-OK response", "method", "GET", "url", endpoint, "status", resp.StatusCode, "body_snippet", snippet)
		return ReleaseInfo{Endpoint: endpoint, Error: derr}, derr
	}

	var result struct {
		Versions []struct {
			VersionKey struct {
				Version string `json:"version"`
			} `json:"versionKey"`
			PublishedAt  string `json:"publishedAt"`
			IsDefault    bool   `json:"isDefault"`
			IsDeprecated bool   `json:"isDeprecated"`
		} `json:"versions"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Debug("deps.dev versions endpoint JSON decode failed", "method", "GET", "url", endpoint, "error", err)
		return ReleaseInfo{
			Endpoint: endpoint,
			Error:    fmt.Errorf("JSON decode failed (url=%s): %w", endpoint, err),
		}, err
	}

	releaseInfo := ReleaseInfo{
		Endpoint: endpoint,
	}

	requestedVersion := parsed.Version()

	// Collect built versions for selection
	builtVersions := make([]Version, 0, len(result.Versions))

	// Process version information
	for _, version := range result.Versions {
		versionInfo := Version{
			VersionKey: VersionKey{
				System: parsed.GetEcosystem(),
				// Prefer the normalized module name captured earlier for Go; fallback to original
				Name:    parsed.GetPackageName(),
				Version: version.VersionKey.Version,
			},
		}

		// If Go normalization provided a different module name, reconstruct the PURL accordingly
		if strings.EqualFold(parsed.GetEcosystem(), "golang") {
			if newPURL, ok := reconstructGoVersionPURL(purlStr, normalizedRawName, version.VersionKey.Version); ok {
				versionInfo.PURL = newPURL
				versionInfo.VersionKey.Name = normalizedRawName
			} else if newPURL, err := commonpurl.WithVersion(purlStr, version.VersionKey.Version); err == nil {
				versionInfo.PURL = newPURL
			} else {
				slog.Debug("failed to update PURL version", "purl", purlStr, "to_version", version.VersionKey.Version, "error", err)
			}
		} else {
			// Non-Go: keep previous behavior (version-only update)
			if newPURL, err := commonpurl.WithVersion(purlStr, version.VersionKey.Version); err == nil {
				versionInfo.PURL = newPURL
			} else {
				slog.Debug("failed to update PURL version", "purl", purlStr, "to_version", version.VersionKey.Version, "error", err)
			}
		}

		if version.PublishedAt != "" {
			if publishedAt, err := time.Parse(time.RFC3339, version.PublishedAt); err == nil {
				versionInfo.PublishedAt = publishedAt
			}
		}

		// carry flags
		versionInfo.IsDefault = version.IsDefault
		versionInfo.IsDeprecated = version.IsDeprecated

		if version.VersionKey.Version == requestedVersion {
			releaseInfo.RequestedVersion = versionInfo
		}

		builtVersions = append(builtVersions, versionInfo)
	}

	// Determine Stable/Dev/Max using unified selection logic
	stable, dev, max := pickStableDevAndMax(builtVersions)
	releaseInfo.StableVersion = stable
	if dev.VersionKey.Version != "" {
		releaseInfo.PreReleaseVersion = dev
	}
	if max.VersionKey.Version != "" {
		releaseInfo.MaxSemverVersion = max
	}
	return releaseInfo, nil
}

// GetDetailsForPURLs fetches detailed information for multiple PURLs with optimized batch processing
// ===== Flow: PURL batch (canonical PURL input main path) =====

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
		slog.Info("ReleaseInfoBatchFailed", "error", err)
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
		eco := strings.ToLower(strings.TrimSpace(parsed.GetEcosystem()))

		// Go-specific fallback: synthesize repository URL (module root or static fallback) centrally
		if eco == "golang" && c.goproxy != nil {
			if repo := attemptGoRepoURLFromPackageName(ctx, c.goproxy, parsed.GetPackageName()); repo != "" {
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
					System:  parsed.GetEcosystem(),
					Name:    parsed.GetPackageName(),
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
		slog.Info("ProjectBatchFailed", "error", err)
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
				if pr, err := purlpkgToParsed(p); err == nil && pr != nil && strings.EqualFold(pr.GetEcosystem(), "golang") {
					if unescName, err := neturl.PathUnescape(pr.GetPackageName()); err == nil && unescName != "" {
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

	results := make(map[string]*PackageResponse)
	resultMutex := sync.Mutex{}

	// Internal parallel processing
	const maxWorkers = 10
	semaphore := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, purl := range purls {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			packageResp, err := c.fetchPackageInfo(ctx, p)
			if err != nil {
				slog.Debug("Failed to fetch package info", "purl", p, "error", err)
				return
			}

			resultMutex.Lock()
			results[p] = packageResp
			resultMutex.Unlock()
		}(purl)
	}

	wg.Wait()
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
		if err != nil || pr == nil || !strings.EqualFold(pr.GetEcosystem(), "maven") {
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
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				bodyBytes, _ := io.ReadAll(resp.Body)
				snippet := truncateString(string(bodyBytes), 1024)
				slog.Debug("deps.dev project batch non-OK response", "method", "POST", "url", url, "page", page, "status", resp.StatusCode, "body_snippet", snippet)
				return nil, fmt.Errorf("HTTP %d (page=%d, url=%s): %s", resp.StatusCode, page, url, snippet)
			}
			var projectResp ProjectBatchResponse
			if err := json.NewDecoder(resp.Body).Decode(&projectResp); err != nil {
				slog.Debug("deps.dev project batch JSON decode failed", "method", "POST", "url", url, "page", page, "error", err)
				return nil, fmt.Errorf("JSON decode failed (url=%s, page=%d): %w", url, page, err)
			}
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

// fetchReleaseInfoBatch fetches release information for multiple PURLs with internal parallelization
func (c *DepsDevClient) fetchReleaseInfoBatch(ctx context.Context, purls []string) (map[string]ReleaseInfo, error) {
	results := make(map[string]ReleaseInfo)
	resultMutex := sync.Mutex{}

	// Internal parallel processing
	const maxWorkers = 10
	semaphore := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	// Progress tracking
	processedCount := 0
	totalPURLs := len(purls)

	for _, purl := range purls {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			releaseInfo, err := c.fetchLatestRelease(ctx, p)
			if err != nil {
				slog.Debug("Failed to fetch release information", "purl", p, "error", err)
				return
			}

			resultMutex.Lock()
			results[p] = releaseInfo
			processedCount++
			currentProcessed := processedCount
			resultMutex.Unlock()

			if totalPURLs >= 1000 && currentProcessed%1000 == 0 {
				slog.Debug("Release info progress", "processed", currentProcessed, "total", totalPURLs)
			}
		}(purl)
	}

	wg.Wait()
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

// fetchPackageInfo fetches basic package information from PURL
func (c *DepsDevClient) fetchPackageInfo(ctx context.Context, purlStr string) (*PackageResponse, error) {
	// Pre-flight normalization + diagnostics for suspicious Maven PURLs
	original := purlStr
	normalizedApplied := false
	if pr, err := purlpkgToParsed(purlStr); err == nil && pr != nil && strings.EqualFold(pr.GetEcosystem(), "maven") {
		ns := strings.TrimSpace(pr.Namespace())
		n := strings.TrimSpace(pr.Name())
		// Attempt normalization only if namespace empty (collapsed form)
		if ns == "" {
			if np := commonpurl.NormalizeMavenCollapsedCoordinates(purlStr); np != purlStr {
				slog.Info("Normalized collapsed Maven PURL", "original", purlStr, "normalized", np)
				purlStr = np
				normalizedApplied = true
				// Re-parse to update namespace/name for warning evaluation
				if pr2, err2 := purlpkgToParsed(purlStr); err2 == nil && pr2 != nil {
					ns = strings.TrimSpace(pr2.Namespace())
					n = strings.TrimSpace(pr2.Name())
				}
			}
		}
		// Re-check after normalization; suppress warning if now valid
		if ns == "" || strings.EqualFold(ns, n) || !strings.Contains(ns, ".") {
			slog.Debug("Suspicious Maven PURL - namespace (groupId) may be missing or incorrect",
				"purl", original,
				"effective", purlStr,
				"namespace", ns,
				"name", n,
				"normalized_applied", normalizedApplied,
				"hint", "Maven PURLs must be pkg:maven/<groupId>/<artifactId>@<version> (e.g., pkg:maven/org.javapos/javapos-contracts@1.14.3)")
		}
	}
	result, err := c.fetchPURLRaw(ctx, purlStr)
	if err != nil {
		// Maven Central Search fallback: when deps.dev returns 404 for a Maven
		// PURL with a missing or suspicious namespace, try to resolve the correct
		// groupId via Maven Central Search API and retry.
		if c.maven != nil && errors.Is(err, errPURLNotFound) {
			if corrected := c.tryMavenSearchFallback(ctx, original); corrected != nil {
				return corrected, nil
			}
		}
		return nil, err
	}
	return result, nil
}

// fetchPURLRaw performs a single HTTP GET against the deps.dev PURL endpoint
// and returns the decoded PackageResponse. No fallback logic is applied here.
func (c *DepsDevClient) fetchPURLRaw(ctx context.Context, purlStr string) (*PackageResponse, error) {
	decoded := purlStr
	if unescaped, err := neturl.PathUnescape(purlStr); err == nil {
		decoded = unescaped
	}
	encodedPURL := neturl.PathEscape(decoded)
	apiURL := c.baseURL + "/purl/" + encodedPURL

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request (url=%s): %w", apiURL, err)
	}
	req.Header.Set("User-Agent", "uzomuzo-depsdev-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")

	resp, err := c.client.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("request failed (url=%s): %w", apiURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w (url=%s)", errPURLNotFound, apiURL)
		}
		body, _ := io.ReadAll(resp.Body)
		snippet := truncateString(string(body), 1024)
		slog.Debug("deps.dev PURL endpoint non-OK response", "method", "GET", "url", apiURL, "status", resp.StatusCode, "body_snippet", snippet)
		return nil, fmt.Errorf("API returned status %d (url=%s): %s", resp.StatusCode, apiURL, snippet)
	}

	var packageResp PackageResponse
	if err := json.NewDecoder(resp.Body).Decode(&packageResp); err != nil {
		slog.Debug("deps.dev PURL endpoint JSON decode failed", "method", "GET", "url", apiURL, "error", err)
		return nil, fmt.Errorf("failed to decode response (url=%s): %w", apiURL, err)
	}

	return &packageResp, nil
}

// tryMavenSearchFallback attempts to resolve a Maven PURL with missing or suspicious
// namespace by querying the Maven Central Search API. If a unique groupId is found,
// it reconstructs the PURL and retries the deps.dev lookup.
//
// Triggers:
//   - namespace is empty (e.g., pkg:maven/jsr250-api)
//   - namespace equals name (e.g., pkg:maven/spring-aop/spring-aop)
//
// Returns the PackageResponse from the retry, or nil if fallback is not applicable
// or the retry also fails.
func (c *DepsDevClient) tryMavenSearchFallback(ctx context.Context, purlStr string) *PackageResponse {
	pr, err := purlpkgToParsed(purlStr)
	if err != nil || pr == nil {
		return nil
	}
	if !strings.EqualFold(pr.GetEcosystem(), "maven") {
		return nil
	}

	ns := strings.TrimSpace(pr.Namespace())
	name := strings.TrimSpace(pr.Name())
	if name == "" {
		return nil
	}
	// Only trigger for missing namespace or namespace == name
	if ns != "" && !strings.EqualFold(ns, name) {
		return nil
	}

	// Strip trailing version-like suffix from artifact name
	// (e.g., "opentelemetry-sdk-extension-autoconfigure-1.28.0" → "opentelemetry-sdk-extension-autoconfigure")
	searchName := stripTrailingVersion(name)

	groupID, found, searchErr := c.maven.SearchByArtifactID(ctx, searchName)
	if searchErr != nil {
		slog.Debug("maven search fallback failed", "purl", purlStr, "error", searchErr)
		return nil
	}
	if !found {
		return nil
	}

	// Reconstruct PURL with resolved groupId
	version := strings.TrimSpace(pr.Version())
	correctedPURL := "pkg:maven/" + groupID + "/" + searchName
	if version != "" {
		correctedPURL += "@" + version
	}

	slog.Info("maven search fallback: retrying with corrected PURL",
		"original", purlStr,
		"corrected", correctedPURL,
	)

	// Retry deps.dev lookup with corrected PURL via shared HTTP helper
	result, err := c.fetchPURLRaw(ctx, correctedPURL)
	if err != nil {
		slog.Debug("maven search fallback: retry also failed",
			"corrected_purl", correctedPURL,
			"error", err,
		)
		return nil
	}

	slog.Info("maven search fallback: resolved via Maven Central Search",
		"original", purlStr,
		"corrected", correctedPURL,
	)
	return result
}

// stripTrailingVersion removes a trailing version-like suffix from an artifact name.
// e.g., "opentelemetry-sdk-extension-autoconfigure-1.28.0" → "opentelemetry-sdk-extension-autoconfigure"
// Only strips if the trailing segment consists entirely of digits and dots (e.g., "1.28.0", "6.4").
// Pre-release suffixes like "-SNAPSHOT", "-M1", "-rc1" are NOT stripped; this is intentional
// to avoid false positives on artifact names ending with alphabetic segments.
func stripTrailingVersion(name string) string {
	idx := strings.LastIndex(name, "-")
	if idx < 0 || idx == len(name)-1 {
		return name
	}
	suffix := name[idx+1:]
	// Check if suffix looks like a version: all chars are digits or dots, starts with digit
	if len(suffix) == 0 || suffix[0] < '0' || suffix[0] > '9' {
		return name
	}
	for _, ch := range suffix {
		if (ch < '0' || ch > '9') && ch != '.' {
			return name
		}
	}
	return name[:idx]
}

// FetchDependentCount fetches the dependent count for a single versioned PURL from the deps.dev GetDependents API.
// The PURL must include a version (e.g., pkg:npm/lodash@4.17.21). Versionless PURLs return nil.
// Supported systems: npm, maven, pypi, cargo (Go, NuGet, RubyGems are NOT supported by this endpoint).
// Returns nil when the endpoint returns 404 (unsupported or unknown package version).
// See: https://docs.deps.dev/api/v3alpha/#getdependents
//
// DDD Layer: Infrastructure
// Endpoint: GET /v3alpha/systems/{system}/packages/{name}/versions/{version}:dependents
func (c *DepsDevClient) FetchDependentCount(ctx context.Context, purlStr string) (*DependentsResponse, error) {
	parser := commonpurl.NewParser()
	parsed, err := parser.Parse(purlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PURL for dependents: %w", err)
	}

	version := strings.TrimSpace(parsed.Version())
	if version == "" {
		slog.Debug("dependents: skipping versionless PURL", "purl", purlStr)
		return nil, nil
	}
	// Go modules may have "+incompatible" suffix that deps.dev doesn't recognize in the version path.
	if strings.ToLower(parsed.GetEcosystem()) == "golang" {
		if idx := strings.Index(version, "+"); idx >= 0 {
			version = version[:idx]
		}
	}

	system, name := toDepsDevSystemAndName(parsed)
	escapedVersion := neturl.PathEscape(version)
	endpoint := fmt.Sprintf("%s/systems/%s/packages/%s/versions/%s:dependents", c.baseURL, system, name, escapedVersion)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create dependents request (url=%s): %w", endpoint, err)
	}
	req.Header.Set("User-Agent", "uzomuzo-depsdev-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")

	resp, err := c.client.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("dependents HTTP request failed (url=%s): %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		slog.Debug("dependents: 404 not found", "purl", purlStr, "url", endpoint)
		return nil, nil // unsupported system or unknown package version
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		snippet := truncateString(string(body), 1024)
		return nil, fmt.Errorf("dependents HTTP %d (url=%s): %s", resp.StatusCode, endpoint, snippet)
	}

	var result DependentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("dependents JSON decode failed (url=%s): %w", endpoint, err)
	}
	return &result, nil
}

// FetchDependentCountBatch fetches dependent counts for multiple PURLs in parallel.
// Returns a map of canonical (versionless) PURL -> DependentsResponse.
// PURLs that fail or return 404 are silently omitted from the result.
//
// DDD Layer: Infrastructure (parallel processing)
func (c *DepsDevClient) FetchDependentCountBatch(ctx context.Context, purls []string) map[string]*DependentsResponse {
	if len(purls) == 0 {
		return make(map[string]*DependentsResponse)
	}

	const maxWorkers = 10
	results := make(map[string]*DependentsResponse)
	var mu sync.Mutex
	semaphore := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, p := range purls {
		wg.Add(1)
		go func(purl string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			resp, err := c.FetchDependentCount(ctx, purl)
			if err != nil {
				slog.Debug("Failed to fetch dependent count", "purl", purl, "error", err)
				return
			}
			if resp == nil {
				return
			}

			// Normalize to versionless canonical key for map consistency
			key := commonpurl.CanonicalKey(purl)
			if key == "" {
				key = purl
			}

			mu.Lock()
			results[key] = resp
			mu.Unlock()
		}(p)
	}

	wg.Wait()
	slog.Debug("Dependent count batch completed", "requested", len(purls), "successful", len(results))
	return results
}

// GetPackageVersionLicenses fetches license identifiers (SPDX preferred) for a specific versioned PURL.
// The input must include an explicit version (e.g., pkg:npm/%40babel/core@7.25.0).
// Returns a normalized, deduplicated, sorted slice of canonical SPDX identifiers (casing preserved, e.g.
// Apache-2.0, GPL-3.0-only) or an empty slice if none found. Non-SPDX strings may appear only as
// fallback-normalized tokens (spaces collapsed to dashes). Empty values and NOASSERTION are excluded.
// Errors are wrapped with context for upstream diagnostics.
func (c *DepsDevClient) GetPackageVersionLicenses(ctx context.Context, versionedPURL string) ([]string, error) {
	vp := strings.TrimSpace(versionedPURL)
	if vp == "" {
		return nil, fmt.Errorf("empty versioned PURL")
	}
	resp, err := c.fetchPackageInfo(ctx, vp)
	if err != nil {
		return nil, fmt.Errorf("fetch package info for license (purl=%s): %w", vp, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("nil package response (purl=%s)", vp)
	}
	licenses := collectVersionLicenses(&resp.Version)
	return licenses, nil
}

// collectVersionLicenses extracts SPDX (preferred) or raw license identifiers from a Version.
// Normalization steps:
//  1. Collect non-empty SPDX strings (LicenseDetails[].Spdx) and normalize via domain.NormalizeLicenseIdentifier
//     preserving canonical SPDX casing (e.g., Apache-2.0, GPL-3.0-only, LGPL-2.1-or-later).
//  2. If none collected, fallback to Version.Licenses values with the same normalization.
//  3. Deduplicate (normalization makes comparison case-insensitive) and return a sorted slice (canonical casing preserved).
//  4. Excludes empty strings and NOASSERTION.
func collectVersionLicenses(v *Version) []string {
	if v == nil {
		return nil
	}
	set := make(map[string]struct{})
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if norm, _ := domain.NormalizeLicenseIdentifier(raw); norm != "" && !strings.EqualFold(norm, "NOASSERTION") {
			set[norm] = struct{}{}
		}
	}
	for _, d := range v.LicenseDetails {
		if d.Spdx != "" {
			add(d.Spdx)
		}
	}
	if len(set) == 0 {
		for _, l := range v.Licenses {
			add(l)
		}
	}
	if len(set) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// helper: parse a PURL string using the shared parser without importing cycles in this file
func purlpkgToParsed(s string) (*commonpurl.ParsedPURL, error) {
	parser := commonpurl.NewParser()
	return parser.Parse(s)
}

// ExtractRepositoryURLFromLinks extracts and normalizes the repository URL from deps.dev links.
//
// Priority order:
//  1. SOURCE_REPO when present and valid
//  2. Fallback to any valid GitHub URL from other links
//
// Returns an empty string when nothing usable is found.
func ExtractRepositoryURLFromLinks(links []Link) string {
	// Priority 1: SOURCE_REPO first
	for _, link := range links {
		if link.Label == "SOURCE_REPO" {
			if gh := common.MapApacheHostedToGitHub(link.URL); gh != "" {
				return gh
			}
			if normalized := common.NormalizeRepositoryURL(link.URL); normalized != "" {
				return normalized
			}
		}
	}

	// Priority 2: any GitHub URL as fallback
	for _, link := range links {
		if gh := common.MapApacheHostedToGitHub(link.URL); gh != "" {
			return gh
		}
		if normalized := common.NormalizeRepositoryURL(link.URL); common.IsValidGitHubURL(normalized) {
			return normalized
		}
	}
	return ""
}

// buildBasicResult creates a BatchResult with basic package information only
func (c *DepsDevClient) buildBasicResult(purl string, packageResp *PackageResponse) *BatchResult {
	return &BatchResult{
		PURL: purl,
		Package: &Package{
			PURL:     packageResp.Version.PURL,
			Versions: []Version{packageResp.Version},
		},
	}
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
	s := strings.ToLower(strings.TrimSpace(repoURL))

	// Remove fragment and query early
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "?"); i >= 0 {
		s = s[:i]
	}

	// Remove common scheme prefixes
	for _, prefix := range []string{
		"https://", "http://", "git+ssh://", "ssh://",
	} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			break
		}
	}

	// Drop leading user@ if present (e.g., git@github.com/...)
	s = strings.TrimPrefix(s, "git@")

	// If the string contains github.com anywhere, extract from there
	idx := strings.Index(s, "github.com")
	if idx == -1 {
		return ""
	}
	key := s[idx:]

	// Normalize separator after host: allow github.com:owner/repo or github.com/owner/repo
	if strings.HasPrefix(key, "github.com:") {
		key = strings.Replace(key, "github.com:", "github.com/", 1)
	}

	// Trim any trailing slashes
	key = strings.TrimRight(key, "/")

	// Ensure we only keep host/owner/repo even if deeper paths appear
	if strings.HasPrefix(key, "github.com/") {
		rest := strings.TrimPrefix(key, "github.com/")
		parts := strings.Split(rest, "/")
		if len(parts) >= 2 {
			owner := parts[0]
			repo := strings.TrimSuffix(parts[1], ".git")
			return "github.com/" + owner + "/" + repo
		}
		// Not enough path segments to form owner/repo
		return ""
	}
	return ""
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

// truncateString returns s if it's shorter than or equal to max; otherwise it returns a shortened
// prefix with an ellipsis suffix. Helps keep error logs compact while still useful.
func truncateString(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
