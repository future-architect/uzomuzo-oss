// Package integration provides external API integration services
package integration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	commonlinks "github.com/future-architect/uzomuzo-oss/internal/common/links"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/clearlydefined"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/golangresolve"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/goproxy"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/govanityresolve"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/links"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/maven"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/packagist"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/pypi"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/rubygems"
)

// IntegrationService handles repository data fetching and analysis from external APIs
type IntegrationService struct {
	githubClient    *github.Client
	depsdevClient   depsdev.Client
	config          *config.Config
	goProxy         *goproxy.Client
	rubygemsClient  *rubygems.Client
	packagistClient *packagist.Client
	pypiClient      *pypi.Client
	mavenClient     *maven.Client
	cdClient        *clearlydefined.Client
	vanityResolver  *govanityresolve.Resolver
}

// IntegrationOption configures an IntegrationService.
type IntegrationOption func(*IntegrationService)

// WithConfig sets the application configuration.
func WithConfig(cfg *config.Config) IntegrationOption {
	return func(s *IntegrationService) { s.config = cfg }
}

// WithRubyGemsClient injects a RubyGems client for dependent count lookups.
func WithRubyGemsClient(c *rubygems.Client) IntegrationOption {
	return func(s *IntegrationService) { s.rubygemsClient = c }
}

// WithPackagistClient injects a Packagist client for dependent count lookups.
func WithPackagistClient(c *packagist.Client) IntegrationOption {
	return func(s *IntegrationService) { s.packagistClient = c }
}

// WithPyPIClient injects a PyPI client used to override Repository.Summary with
// info.summary for PyPI-ecosystem analyses. Optional — when unset, Summary keeps
// the deps.dev / GitHub-derived value.
func WithPyPIClient(c *pypi.Client) IntegrationOption {
	return func(s *IntegrationService) { s.pypiClient = c }
}

// WithMavenClient injects a Maven client used by enrichLicenseFromManifest to
// fall back to pom.xml <licenses> when deps.dev and GitHub fail to yield a
// canonical SPDX license.
//
// Optional in the strict sense: when unset the manifest fallback is skipped
// and Maven licenses remain as resolved by upstream sources. In production
// this materially reduces Maven license coverage (~38% baseline per issue
// #327), so library users wiring their own IntegrationService should opt in.
// NewAnalysisServiceFromConfig and NewFetchServiceFromConfig wire it eagerly.
func WithMavenClient(c *maven.Client) IntegrationOption {
	return func(s *IntegrationService) { s.mavenClient = c }
}

// WithClearlyDefinedClient injects a ClearlyDefined.io client used by the
// fourth-tier license fallback (after deps.dev, GitHub, and the Maven POM
// manifest tier).
//
// Optional: when unset, the CD pass is skipped and licenses remain as resolved
// by upstream tiers. In production this materially reduces license coverage
// (#327 issue context: CD's empirical hit rate is 67-93% on the residual
// "broken subset" across maven/nuget/pypi). Library users wiring their own
// IntegrationService should opt in. NewAnalysisServiceFromConfig and
// NewFetchServiceFromConfig wire it eagerly.
func WithClearlyDefinedClient(c *clearlydefined.Client) IntegrationOption {
	return func(s *IntegrationService) { s.cdClient = c }
}

// WithVanityResolver overrides the default Go vanity-URL resolver that
// NewIntegrationService installs eagerly. Tests use this option to inject
// a stubbed resolver backed by httptest; production callers rarely need it.
func WithVanityResolver(r *govanityresolve.Resolver) IntegrationOption {
	return func(s *IntegrationService) { s.vanityResolver = r }
}

// NewIntegrationService creates a new integration service with optional configuration.
func NewIntegrationService(githubClient *github.Client, depsdevClient depsdev.Client, opts ...IntegrationOption) *IntegrationService {
	s := &IntegrationService{
		githubClient:   githubClient,
		depsdevClient:  depsdevClient,
		goProxy:        goproxy.NewClient(),
		vanityResolver: govanityresolve.NewResolver(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// GitHubClient returns the underlying GitHub client (read-only access for wiring evaluators).
func (s *IntegrationService) GitHubClient() *github.Client { return s.githubClient }

// ===== Flow: PURL inputs =====

// FetchAnalysis fetches analysis for a single PURL by delegating to optimized batch processing
// Flow: PURL
func (s *IntegrationService) FetchAnalysis(ctx context.Context, purl string) (*domain.Analysis, error) {
	slog.Debug("fetch_analysis_delegating_to_batch", "purl", purl)

	// Delegate to batch processing for efficiency and consistency
	batchResults, err := s.AnalyzeFromPURLs(ctx, []string{purl})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch analysis using batch processing: %w", err)
	}

	// Extract single result from batch
	analysis, exists := batchResults[purl]
	if !exists {
		// Create fallback analysis if no result found
		pkg := s.createPackageFromPURL(purl)
		// Fallback: we could not obtain batch details. Preserve input exactly as OriginalPURL.
		// EffectivePURL mirrors it now; later enrichment (if any) may still diverge.
		an := &domain.Analysis{OriginalPURL: purl, EffectivePURL: purl, Package: pkg, AnalyzedAt: time.Now(), Error: fmt.Errorf("no analysis result found for PURL: %s", purl)}
		an.EnsureCanonical()
		return an, nil
	}
	analysis.EnsureCanonical()

	return analysis, nil
}

// FetchAnalysisWithGitHub is an alias for FetchAnalysis that delegates to batch processing
// Flow: PURL
func (s *IntegrationService) FetchAnalysisWithGitHub(ctx context.Context, purl string) (*domain.Analysis, error) {
	// Simply delegate to FetchAnalysis since it already uses batch processing with GitHub enhancement
	return s.FetchAnalysis(ctx, purl)
}

// enhanceAnalysesWithGitHubBatch enhances multiple analyses with GitHub data in parallel
func (s *IntegrationService) enhanceAnalysesWithGitHubBatch(ctx context.Context, analyses map[string]*domain.Analysis) error {
	// Use GitHub client's parallel processing for repository states
	return s.githubClient.FetchRepositoryStates(ctx, analyses)
}

// createPackageFromPURL creates a domain Package entity from PURL string using unified parser
func (s *IntegrationService) createPackageFromPURL(purlStr string) *domain.Package {
	parser := purl.NewParser()
	parsed, err := parser.Parse(purlStr)

	if err != nil {
		slog.Debug("purl_parse_failed_fallback", "purl", purlStr, "error", err)
		// Fallback for invalid PURLs
		return &domain.Package{
			PURL:      purlStr,
			Ecosystem: "",
			Version:   "",
		}
	}

	return &domain.Package{
		PURL:      purlStr,
		Ecosystem: parsed.GetEcosystem(),
		Version:   parsed.Version(),
	}
}

// buildVersionDetail constructs a domain.VersionDetail (flattened: only registry URL retained).
func (s *IntegrationService) buildVersionDetail(src *depsdev.Version, analysis *domain.Analysis) *domain.VersionDetail {
	if src == nil || src.VersionKey.Version == "" {
		return nil
	}
	vd := &domain.VersionDetail{
		Version:      src.VersionKey.Version,
		PublishedAt:  src.PublishedAt,
		IsPrerelease: false,
		IsDeprecated: src.IsDeprecated,
	}
	// Extract advisories (all IDs)
	if len(src.AdvisoryKeys) > 0 {
		for _, adv := range src.AdvisoryKeys {
			srcName, url := classifyAdvisory(adv.ID)
			vd.Advisories = append(vd.Advisories, domain.Advisory{ID: adv.ID, Source: srcName, URL: url})
		}
	}
	// Build registry URL for this version
	if analysis != nil && analysis.Package != nil {
		parser := purl.NewParser()
		raw := analysis.Package.PURL
		if u, err := url.PathUnescape(raw); err == nil && u != "" {
			raw = u
		}
		if parsed, err := parser.Parse(raw); err == nil {
			pkgName := parsed.GetPackageName()
			group := parsed.Namespace()
			finalName := pkgName
			if group != "" {
				switch strings.ToLower(strings.TrimSpace(analysis.Package.Ecosystem)) {
				case "maven":
					finalName = commonlinks.JoinMavenName(group, pkgName)
				case "npm":
					finalName = commonlinks.JoinNpmName(group, pkgName)
				case "packagist", "composer":
					finalName = group + "/" + pkgName
				}
			}
			vd.RegistryURL = links.BuildVersionRegistryURL(analysis.Package.Ecosystem, finalName, src.VersionKey.Version)
		}
	}
	return vd
}

// classifyAdvisory infers advisory source and canonical URL from an ID.
func classifyAdvisory(id string) (string, string) {
	upper := strings.ToUpper(id)
	switch {
	case strings.HasPrefix(upper, "GHSA-"):
		return "GHSA", "https://github.com/advisories/" + id
	case strings.HasPrefix(upper, "CVE-"):
		return "CVE", "https://nvd.nist.gov/vuln/detail/" + upper
	case strings.HasPrefix(upper, "GO-"):
		return "OSV", "https://osv.dev/" + upper
	default:
		// Fallback: OSV global search handles many ecosystem IDs (PyPI, npm, etc.)
		return "OTHER", "https://osv.dev/" + upper
	}
}

// extractScorecardChecks returns scorecard checks using best-effort across possible shapes
// Some deps.dev payloads may embed checks under Project.Scorecard.Scorecard.Checks rather than Project.Scorecard.Checks
func (s *IntegrationService) extractScorecardChecks(project *depsdev.Project) []depsdev.ScorecardCheckSet {
	if project == nil {
		return nil
	}
	if len(project.Scorecard.Checks) > 0 {
		return project.Scorecard.Checks
	}
	if len(project.Scorecard.Scorecard.Checks) > 0 {
		return project.Scorecard.Scorecard.Checks
	}
	return nil
}

// ===== Flow: GitHub URL inputs =====

// AnalyzeFromGitHubURL processes a GitHub URL with default version detection
//
// DDD Layer: Infrastructure (complex business logic and external API calls)
// This method encapsulates the complete GitHub URL to PURL conversion and analysis workflow:
// 1. Converts GitHub URL to basic PURL (without version)
// 2. Fetches default version from deps.dev
// 3. Creates versioned PURL for complete analysis
// 4. Performs full scorecard analysis
// 5. Validates resolved package repo URL matches input (round-trip check)
// Flow: GitHub URL
func (s *IntegrationService) AnalyzeFromGitHubURL(ctx context.Context, githubURL string) (*domain.Analysis, error) {
	slog.Debug("analyze_github_url_called", "github_url", githubURL)

	// Step 1: Convert GitHub URL to basic PURL (without version)
	basePURL, err := s.githubURLToPURL(ctx, githubURL)
	if err != nil {
		// Repos without registry packages (e.g., GitHub Actions) cannot produce a PURL.
		// Fall back to GitHub-only analysis using repository metadata.
		// Only match the "no supported package managers" case — other ResourceNotFoundErrors
		// (e.g., repo not found on GitHub) should propagate as failures.
		var scorecardErr *common.ScorecardError
		if errors.As(err, &scorecardErr) && scorecardErr.Type == common.ErrorTypeResourceNotFound &&
			strings.Contains(scorecardErr.Message, "no supported package managers") {
			slog.Info("no_registry_package_falling_back_to_github_only",
				"github_url", githubURL)
			return s.buildGitHubOnlyAnalysis(ctx, githubURL)
		}
		return nil, fmt.Errorf("failed to convert GitHub URL to PURL: %w", err)
	}

	slog.Debug("base_purl_generated", "purl", basePURL)

	// Step 2: Fetch default version from deps.dev using GetLatestReleasesForPURLs
	releaseInfo, err := s.depsdevClient.GetLatestReleasesForPURLs(ctx, []string{basePURL})
	if err != nil {
		slog.Debug("fetch_version_info_failed", "error", err)
		// Fallback: proceed with base PURL without version
		//TODO FetchFrom GrraphQL
		return s.fetchAndValidateGitHubAnalysis(ctx, basePURL, githubURL)
	}

	// Extract stable version from release info
	releaseData, exists := releaseInfo[basePURL]
	if !exists || releaseData == nil || releaseData.Error != nil {
		slog.Debug("no_version_data", "purl", basePURL)
		// Fallback: proceed with base PURL without version
		//TODO FetchFrom GrraphQL
		return s.fetchAndValidateGitHubAnalysis(ctx, basePURL, githubURL)
	}

	// Step 3: Create versioned PURL if stable version is available
	var versionedPURL string
	if releaseData.StableVersion.VersionKey.Version != "" {
		// Generate versioned PURL
		versionedPURL = s.generateVersionedPURL(basePURL, releaseData.StableVersion.VersionKey.Version)
		slog.Debug("stable_version_detected",
			"version", releaseData.StableVersion.VersionKey.Version,
			"versioned_purl", versionedPURL)
	} else {
		slog.Debug("no_stable_version", "purl", basePURL)
		//TODO FetchFrom GrraphQL
		versionedPURL = basePURL
	}

	// Step 4: Perform full analysis with the versioned PURL, then validate
	return s.fetchAndValidateGitHubAnalysis(ctx, versionedPURL, githubURL)
}

// fetchAndValidateGitHubAnalysis fetches analysis for a PURL derived from a GitHub URL,
// then validates that the resolved package's repository URL matches the original GitHub URL.
// If the resolved repo URL points to a different repository, the deps.dev resolution is
// discarded and a GitHub-only analysis is returned to prevent misattribution.
//
// See: https://github.com/future-architect/uzomuzo-oss/issues/99
func (s *IntegrationService) fetchAndValidateGitHubAnalysis(ctx context.Context, purl, githubURL string) (*domain.Analysis, error) {
	analysis, err := s.FetchAnalysisWithGitHub(ctx, purl)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch analysis for PURL %s (from %s): %w", purl, githubURL, err)
	}

	// If the PURL was generated but the package is not found in deps.dev
	// (e.g., package.json exists in repo but not published to npm),
	// fall back to GitHub-only analysis.
	// Gate on the deps.dev-specific error message to avoid catching unrelated
	// ResourceNotFoundErrors that may be introduced by other enrichment steps.
	var depsdevErr *common.ScorecardError
	if analysis.Error != nil && errors.As(analysis.Error, &depsdevErr) &&
		depsdevErr.Type == common.ErrorTypeResourceNotFound &&
		strings.Contains(depsdevErr.Message, "package not found in deps.dev") {
		slog.Info("deps_dev_package_not_found_falling_back_to_github_only",
			"purl", purl, "github_url", githubURL)
		// Reuse the existing analysis if GitHub enrichment already populated RepoState
		// for the same repository, avoiding a redundant GitHub API call.
		if analysis.RepoState != nil && analysis.RepoURL != "" && s.validateRepoURLMatch(analysis.RepoURL, githubURL) {
			analysis.Error = nil
			analysis.OriginalPURL = githubURL
			analysis.EffectivePURL = githubURL
			analysis.Package = nil
			analysis.ReleaseInfo = nil
			analysis.EnsureCanonical()
			return analysis, nil
		}
		return s.buildGitHubOnlyAnalysis(ctx, githubURL)
	}

	// Round-trip validation: verify the resolved package actually belongs to the input repository.
	// deps.dev may return an unrelated package that happens to share the repo name
	// (e.g., "checkout" → pkg:npm/checkout from github.com/bmeck/node-checkout,
	//  not the intended github.com/actions/checkout).
	if !s.validateRepoURLMatch(analysis.RepoURL, githubURL) {
		slog.Warn("deps_dev_repo_mismatch_detected",
			"github_url", githubURL,
			"resolved_purl", purl,
			"resolved_repo_url", analysis.RepoURL,
		)
		return s.buildGitHubOnlyAnalysis(ctx, githubURL)
	}

	return analysis, nil
}

// validateRepoURLMatch checks whether the resolved repository URL matches the input GitHub URL.
// Both URLs are normalized to owner/repo form (case-insensitive) before comparison.
// Returns true if they match, or if the resolved URL is empty (no data to validate against).
func (s *IntegrationService) validateRepoURLMatch(resolvedRepoURL, inputGitHubURL string) bool {
	// If deps.dev returned no repo URL, we cannot validate — allow the result.
	if resolvedRepoURL == "" {
		return true
	}

	inputOwner, inputRepo, err := common.ExtractGitHubOwnerRepo(inputGitHubURL)
	if err != nil {
		// Cannot parse the input URL; skip validation rather than rejecting valid results.
		return true
	}

	resolvedOwner, resolvedRepo, err := common.ExtractGitHubOwnerRepo(resolvedRepoURL)
	if err != nil {
		// Resolved URL is not a GitHub URL (e.g., GitLab, Bitbucket) — mismatch.
		return false
	}

	return strings.EqualFold(inputOwner, resolvedOwner) && strings.EqualFold(inputRepo, resolvedRepo)
}

// buildGitHubOnlyAnalysis creates an Analysis populated solely from GitHub repository metadata,
// without any deps.dev package resolution. This is the fallback when round-trip validation
// detects that deps.dev resolved to an unrelated package.
func (s *IntegrationService) buildGitHubOnlyAnalysis(ctx context.Context, githubURL string) (*domain.Analysis, error) {
	owner, repo, err := common.ExtractGitHubOwnerRepo(githubURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse GitHub URL for fallback analysis: %w", err)
	}

	repoURL := fmt.Sprintf("https://github.com/%s/%s", owner, repo)
	analysis := &domain.Analysis{
		OriginalPURL:  githubURL,
		EffectivePURL: githubURL,
		RepoURL:       repoURL,
		AnalyzedAt:    time.Now(),
	}
	analysis.EnsureCanonical()

	// Enrich with GitHub repository metadata (repo state, commit stats, etc.)
	analyses := map[string]*domain.Analysis{githubURL: analysis}
	if err := s.enhanceAnalysesWithGitHubBatch(ctx, analyses); err != nil {
		slog.Debug("github_only_enhancement_failed", "error", err, "github_url", githubURL)
	}

	return analysis, nil
}

// githubURLToPURL converts a GitHub URL to a PURL using GitHub GraphQL API to identify package managers
func (s *IntegrationService) githubURLToPURL(ctx context.Context, githubURL string) (string, error) {
	owner, repo, err := s.parseGitHubURL(githubURL)
	if err != nil {
		return "", err
	}

	slog.Debug("analyzing_github_repository", "owner", owner, "repo", repo)

	// Use a reasonable timeout to avoid hanging while allowing GraphQL to complete
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	repoInfo, err := s.githubClient.FetchDetailedRepositoryInfo(ctxWithTimeout, owner, repo)
	if err != nil {
		// Authentication errors should not be retried via fallback.
		if common.IsAuthenticationError(err) {
			return "", common.NewAuthenticationError("failed to fetch repository info", err).
				WithContext("repository", fmt.Sprintf("%s/%s", owner, repo)).
				WithContext("github_url", githubURL)
		}
		// Not-found, rate-limit, timeout, and network errors should fail fast
		// instead of inferring a potentially incorrect PURL via language detection.
		if common.IsResourceNotFoundError(err) || common.IsRateLimitError(err) ||
			common.IsTimeoutError(err) || common.IsNetworkError(err) {
			return "", fmt.Errorf("failed to fetch repository info for %s/%s: %w", owner, repo, err)
		}
		// For other errors (GraphQL field-level errors, insufficient scopes, etc.),
		// fall back to REST API language detection.
		slog.Warn("GraphQL repository info failed, falling back to REST language detection",
			"owner", owner, "repo", repo, "error", err)
		return s.inferPURLFromLanguages(ctxWithTimeout, owner, repo)
	}
	if repoInfo == nil {
		// Token not available for GraphQL; use REST API language detection to infer ecosystem
		slog.Warn("GitHub token not available for GraphQL - using REST language detection",
			"owner", owner, "repo", repo)
		return s.inferPURLFromLanguages(ctxWithTimeout, owner, repo)
	}

	// Extract package managers from dependency manifests
	packageManagers := s.extractPackageManagersFromManifests(repoInfo.DependencyGraphManifests)

	// Convert package managers to PURLs based on repository information
	purls := s.generatePURLsFromPackageManagers(packageManagers, owner, repo)

	// Best-effort: If a Go PURL was generated, normalize github.com/owner/repo to the module root
	// using the Go proxy helper. This improves correctness for repos with v2+ module paths
	// or submodule roots. Vanity domains (e.g., go.uber.org) are not resolvable from a GitHub URL
	// alone and will remain as repo path.
	for i, p := range purls {
		// Only adjust the first matched Go PURL; the function returns a single PURL below
		if strings.HasPrefix(strings.ToLower(p), "pkg:golang/") && s.goProxy != nil {
			// Extract import path after prefix
			raw := strings.TrimPrefix(p, "pkg:golang/")
			if unesc, err := url.PathUnescape(raw); err == nil && unesc != "" {
				raw = unesc
			}
			if mod, _, ok := golangresolve.NormalizePathToModuleRoot(ctx, s.goProxy, raw); ok && mod != "" && mod != raw {
				// Rebuild PURL with module root
				purls[i] = "pkg:golang/" + mod
			}
			break
		}
	}

	if len(purls) == 0 {
		// If no valid package managers found, return error
		slog.Info("no_dependency_manifests_found", "github_url", githubURL)
		return "", common.NewResourceNotFoundError("no supported package managers detected in repository").
			WithContext("repository", fmt.Sprintf("%s/%s", owner, repo)).
			WithContext("github_url", githubURL)
	} else if len(purls) > 1 {
		slog.Debug("multiple_ecosystems_detected", "purls", purls)
	}

	generatedPURL := strings.ToLower(purls[0])
	slog.Debug("generated_purl", "purl", generatedPURL)
	return generatedPURL, nil
}

// inferPURLFromLanguages uses the GitHub REST API (no token required) to detect
// the primary language and generate a best-effort PURL.
func (s *IntegrationService) inferPURLFromLanguages(ctx context.Context, owner, repo string) (string, error) {
	languages, err := s.githubClient.FetchRepoLanguages(ctx, owner, repo)
	if err != nil {
		slog.Warn("Failed to fetch repo languages, falling back to repo name",
			"owner", owner, "repo", repo, "error", err)
		// Ultimate fallback: use repo name as npm package (most common on GitHub)
		return fmt.Sprintf("pkg:npm/%s", repo), nil
	}

	// Find the primary language (most bytes)
	var primaryLang string
	var maxBytes int
	for lang, bytes := range languages {
		if bytes > maxBytes {
			primaryLang = lang
			maxBytes = bytes
		}
	}

	slog.Debug("inferred_primary_language", "owner", owner, "repo", repo, "language", primaryLang)

	// Map language to ecosystem PURL
	switch strings.ToLower(primaryLang) {
	case "javascript", "typescript":
		return fmt.Sprintf("pkg:npm/%s", repo), nil
	case "python":
		return fmt.Sprintf("pkg:pypi/%s", repo), nil
	case "java", "kotlin":
		return fmt.Sprintf("pkg:maven/%s/%s", owner, repo), nil
	case "go":
		p := fmt.Sprintf("pkg:golang/github.com/%s/%s", owner, repo)
		// Best-effort: normalize to module root via Go proxy
		if s.goProxy != nil {
			importPath := fmt.Sprintf("github.com/%s/%s", owner, repo)
			if mod, _, ok := golangresolve.NormalizePathToModuleRoot(ctx, s.goProxy, importPath); ok && mod != "" {
				p = "pkg:golang/" + mod
			}
		}
		return p, nil
	case "rust":
		return fmt.Sprintf("pkg:cargo/%s", repo), nil
	case "ruby":
		return fmt.Sprintf("pkg:gem/%s", repo), nil
	case "c#", "f#", "visual basic .net":
		return fmt.Sprintf("pkg:nuget/%s", repo), nil
	case "php":
		return fmt.Sprintf("pkg:composer/%s/%s", owner, repo), nil
	default:
		// Unknown language; try npm as reasonable default for GitHub repos
		slog.Debug("unknown_language_defaulting_to_npm", "language", primaryLang, "repo", repo)
		return fmt.Sprintf("pkg:npm/%s", repo), nil
	}
}

// parseGitHubURL extracts owner and repo from GitHub URL
func (s *IntegrationService) parseGitHubURL(githubURL string) (string, string, error) {
	owner, repo, err := common.ExtractGitHubOwnerRepo(githubURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid GitHub URL format: %s", githubURL)
	}
	return owner, repo, nil
}

// generateVersionedPURL creates a versioned PURL from base PURL and version
func (s *IntegrationService) generateVersionedPURL(basePURL, version string) string {
	// PURL format: pkg:type/namespace/name@version?qualifiers#subpath
	if strings.Contains(basePURL, "@") {
		// If version already exists, replace it
		parts := strings.Split(basePURL, "@")
		baseWithoutVersion := parts[0]
		return fmt.Sprintf("%s@%s", baseWithoutVersion, version)
	} else {
		// Add version to base PURL
		return fmt.Sprintf("%s@%s", basePURL, version)
	}
}

// extractPackageManagersFromManifests extracts package managers from GitHub dependency manifests
func (s *IntegrationService) extractPackageManagersFromManifests(manifests github.DependencyGraphManifests) []string {
	packageManagersMap := make(map[string]bool)

	for _, manifest := range manifests.Nodes {
		for _, dependency := range manifest.Dependencies.Nodes {
			if dependency.PackageManager != "" {
				// Convert GitHub package manager names to ecosystem names
				if ecosystem := s.mapPackageManagerToEcosystem(dependency.PackageManager); ecosystem != "" {
					packageManagersMap[ecosystem] = true
				}
			}
		}
	}

	// Convert map to slice
	var packageManagers []string
	for pm := range packageManagersMap {
		packageManagers = append(packageManagers, pm)
	}

	return packageManagers
}

// mapPackageManagerToEcosystem converts GitHub package manager names to ecosystem names used in PURLs
func (s *IntegrationService) mapPackageManagerToEcosystem(packageManager string) string {
	ecosystem := purl.MapPackageManagerToEcosystem(packageManager)

	if ecosystem == "" {
		slog.Debug("unknown_package_manager",
			"package_manager", packageManager,
			"supported_ecosystems", purl.SupportedEcosystems())
		return ""
	}

	slog.Debug("mapped_package_manager",
		"package_manager", packageManager,
		"ecosystem", ecosystem)
	return ecosystem
}

// generatePURLsFromPackageManagers generates PURLs based on detected package managers and repository info
func (s *IntegrationService) generatePURLsFromPackageManagers(packageManagers []string, owner, repo string) []string {
	var purls []string

	for _, ecosystem := range packageManagers {
		purl := s.generatePURLForEcosystem(ecosystem, owner, repo)
		if purl != "" {
			purls = append(purls, purl)
		}
	}

	return purls
}

// generatePURLForEcosystem generates a PURL for a specific ecosystem based on repository information
func (s *IntegrationService) generatePURLForEcosystem(ecosystem, owner, repo string) string {
	switch ecosystem {
	case "npm":
		// npm packages typically use the repository name
		return fmt.Sprintf("pkg:npm/%s", repo)
	case "pypi":
		// PyPI packages typically use lowercase repository name
		return fmt.Sprintf("pkg:pypi/%s", strings.ToLower(repo))
	case "golang":
		// Go packages use the full GitHub path
		return fmt.Sprintf("pkg:golang/github.com/%s/%s", owner, repo)
	case "maven":
		// Maven uses groupId:artifactId format, often matching GitHub org:repo
		return fmt.Sprintf("pkg:maven/%s/%s", owner, repo)
	case "nuget":
		// NuGet packages typically use the repository name
		return fmt.Sprintf("pkg:nuget/%s", repo)
	case "cargo":
		// Cargo packages typically use lowercase repository name
		return fmt.Sprintf("pkg:cargo/%s", strings.ToLower(repo))
	case "gem":
		// Ruby gems typically use lowercase repository name
		return fmt.Sprintf("pkg:gem/%s", strings.ToLower(repo))
	case "github":
		// Generic GitHub reference
		return fmt.Sprintf("pkg:github/%s/%s", owner, repo)
	default:
		// Unknown ecosystem, log for debugging and return empty string
		slog.Info("unknown_ecosystem_skip",
			"ecosystem", ecosystem,
			"owner", owner,
			"repo", repo)
		return ""
	}
}
