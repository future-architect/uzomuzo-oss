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
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/golangresolve"
)

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
		return s.fetchAndValidateGitHubAnalysis(ctx, basePURL, basePURL, githubURL)
	}

	// Extract stable version from release info
	releaseData, exists := releaseInfo[basePURL]
	if !exists || releaseData == nil || releaseData.Error != nil {
		slog.Debug("no_version_data", "purl", basePURL)
		// Fallback: proceed with base PURL without version
		//TODO FetchFrom GrraphQL
		return s.fetchAndValidateGitHubAnalysis(ctx, basePURL, basePURL, githubURL)
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

	// Step 4: Perform full analysis with the versioned PURL, then validate.
	// basePURL is the requested coordinate: a GitHub URL never carries a caller
	// version, so versionedPURL is uzomuzo's own selection.
	return s.fetchAndValidateGitHubAnalysis(ctx, versionedPURL, basePURL, githubURL)
}

// fetchAndValidateGitHubAnalysis fetches analysis for a PURL derived from a GitHub URL,
// then validates that the resolved package's repository URL matches the original GitHub URL.
// If the resolved repo URL points to a different repository, the deps.dev resolution is
// discarded and a GitHub-only analysis is returned to prevent misattribution.
//
// The two PURL parameters are not interchangeable: analyzePURL is the coordinate
// actually analyzed and may carry a version uzomuzo selected, while requestedPURL is
// the unversioned base the caller's GitHub URL maps to. OriginalPURL is set to
// requestedPURL on the resolved-package path only; the two GitHub-only paths below
// (deps.dev has no such package, or the resolved repo does not match) store
// githubURL instead, because no package coordinate applies. See ADR-0021.
//
// See: https://github.com/future-architect/uzomuzo-oss/issues/99
func (s *IntegrationService) fetchAndValidateGitHubAnalysis(ctx context.Context, analyzePURL, requestedPURL, githubURL string) (*domain.Analysis, error) {
	analysis, err := s.FetchAnalysisWithGitHub(ctx, analyzePURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch analysis for PURL %s (from %s): %w", analyzePURL, githubURL, err)
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
			"purl", analyzePURL, "github_url", githubURL)
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
			"resolved_purl", analyzePURL,
			"resolved_repo_url", analysis.RepoURL,
		)
		return s.buildGitHubOnlyAnalysis(ctx, githubURL)
	}

	analysis.OriginalPURL = requestedPURL
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

// generateVersionedPURL creates a versioned PURL from a base PURL and a version
// string. It uses a structured PURL parser so that npm scoped packages of the
// form pkg:npm/@scope/name are handled correctly — naive strings.Contains(p,
// "@") misidentifies the "@scope" namespace separator as a version delimiter,
// corrupting the base when the PURL is split on "@".
//
// If basePURL already carries a version it is replaced; otherwise the version
// is appended. Parse failures fall back to fmt.Sprintf to preserve the
// pre-existing observable behaviour for malformed inputs.
func (s *IntegrationService) generateVersionedPURL(basePURL, version string) string {
	result, err := purl.WithVersion(basePURL, version)
	if err != nil {
		// Fallback: basePURL is not a valid PURL — append the version directly.
		return fmt.Sprintf("%s@%s", basePURL, version)
	}
	return result
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
