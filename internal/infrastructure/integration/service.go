// Package integration provides external API integration services
package integration

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/golangresolve"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/goproxy"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/links"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/packagist"
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

// NewIntegrationService creates a new integration service with optional configuration.
func NewIntegrationService(githubClient *github.Client, depsdevClient depsdev.Client, opts ...IntegrationOption) *IntegrationService {
	s := &IntegrationService{githubClient: githubClient, depsdevClient: depsdevClient, goProxy: goproxy.NewClient()}
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
					finalName = group + ":" + pkgName
				case "packagist", "composer", "npm":
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
// Flow: GitHub URL
func (s *IntegrationService) AnalyzeFromGitHubURL(ctx context.Context, githubURL string) (*domain.Analysis, error) {
	slog.Debug("analyze_github_url_called", "github_url", githubURL)

	// Step 1: Convert GitHub URL to basic PURL (without version)
	basePURL, err := s.githubURLToPURL(ctx, githubURL)
	if err != nil {
		return nil, fmt.Errorf("failed to convert GitHub URL to PURL: %w", err)
	}

	slog.Debug("base_purl_generated", "purl", basePURL)

	// Step 2: Fetch default version from deps.dev using GetLatestReleasesForPURLs
	releaseInfo, err := s.depsdevClient.GetLatestReleasesForPURLs(ctx, []string{basePURL})
	if err != nil {
		slog.Debug("fetch_version_info_failed", "error", err)
		// Fallback: proceed with base PURL without version
		//TODO FetchFrom GrraphQL
		return s.FetchAnalysisWithGitHub(ctx, basePURL)
	}

	// Extract stable version from release info
	releaseData, exists := releaseInfo[basePURL]
	if !exists || releaseData == nil || releaseData.Error != nil {
		slog.Debug("no_version_data", "purl", basePURL)
		// Fallback: proceed with base PURL without version
		//TODO FetchFrom GrraphQL
		return s.FetchAnalysisWithGitHub(ctx, basePURL)
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

	// Step 4: Perform full analysis with the versioned PURL
	return s.FetchAnalysisWithGitHub(ctx, versionedPURL)
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
		// GraphQL failed (e.g. invalid/expired token, permission error, rate limit).
		// Fall back to REST language detection instead of giving up entirely.
		slog.Warn("GraphQL failed - falling back to REST language detection",
			"owner", owner, "repo", repo, "error", err)
		return s.inferPURLFromLanguages(ctx, owner, repo)
	}
	if repoInfo == nil {
		// Token not available for GraphQL; use REST API language detection to infer ecosystem
		slog.Warn("GitHub token not available for GraphQL - using REST language detection",
			"owner", owner, "repo", repo)
		return s.inferPURLFromLanguages(ctx, owner, repo)
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
