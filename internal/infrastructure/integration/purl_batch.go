package integration

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
)

// AnalyzeFromPURLs efficiently fetches analyses for multiple PURLs using optimized batch processing.
//
// DDD Layer: Infrastructure (external API orchestration & enrichment)
// Responsibilities:
//   - Batch fetch deps.dev details
//   - Populate domain.Analysis aggregates
//   - Enhance with GitHub repo metadata (delegates to enhanceAnalysesWithGitHubBatch)
//   - Emit structured observability metrics
//
// NOTE: This was extracted from service.go to reduce file size and isolate the PURL-centric workflow.
func (s *IntegrationService) AnalyzeFromPURLs(ctx context.Context, purls []string) (map[string]*domain.Analysis, error) {
	if len(purls) == 0 {
		return make(map[string]*domain.Analysis), nil
	}

	slog.Debug("starting_purl_batch", "purl_count", len(purls))
	analyses := make(map[string]*domain.Analysis)

	batchResults, err := s.depsdevClient.GetDetailsForPURLs(ctx, purls)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PURL details in batch: %w", err)
	}

	missingRepoCount := 0
	missingProjectCount := 0
	missingScorecardCount := 0

	for _, p := range purls {
		pkg := s.createPackageFromPURL(p)
		// Preserve user input as OriginalPURL; EffectivePURL starts identical and may diverge
		// later (e.g., version resolution, collapsed coordinate expansion).
		analysis := &domain.Analysis{OriginalPURL: p, EffectivePURL: p, Package: pkg, AnalyzedAt: time.Now()}
		analysis.EnsureCanonical()
		if batchResult, ok := batchResults[p]; ok && batchResult != nil {
			s.populateAnalysisFromBatchResult(analysis, batchResult)
			// When deps.dev returned no package data but a repo URL was synthesized
			// (e.g. Go module fallback), mark as not-found so downstream fallback
			// logic (registry / catalog) can recognise the typed error.
			if batchResult.Package == nil && analysis.Error == nil {
				analysis.Error = common.NewResourceNotFoundError(
					fmt.Sprintf("package not found in deps.dev: %s", p),
				)
			}
			if analysis.RepoURL == "" {
				missingRepoCount++
			}
			if batchResult.Project == nil && analysis.RepoURL != "" {
				missingProjectCount++
			}
			if batchResult.Project != nil && len(s.extractScorecardChecks(batchResult.Project)) == 0 {
				missingScorecardCount++
			}
		} else {
			analysis.Error = common.NewResourceNotFoundError(
				fmt.Sprintf("package not found in deps.dev: %s", p),
			)
		}
		analyses[p] = analysis
	}

	// GitHub enrichment (best-effort)
	if err := s.enhanceAnalysesWithGitHubBatch(ctx, analyses); err != nil {
		slog.Debug("github_enhancement_failed", "error", err)
	}

	// Dependent count + dependency count enrichment (best-effort, parallel).
	// These are independent enrichment steps hitting different deps.dev endpoints,
	// so running them concurrently halves wall-clock time for large batches (30k+ PURLs).
	// depsGraphResults is written inside a goroutine; enrichWg.Wait() establishes
	// a happens-before guarantee so the read after Wait is safe.
	var depsGraphResults map[string]*depsdev.DependenciesResponse
	var enrichWg sync.WaitGroup
	enrichWg.Add(2)
	go func() {
		defer enrichWg.Done()
		s.enrichDependentCounts(ctx, purls, analyses)
	}()
	go func() {
		defer enrichWg.Done()
		// Uses latestReleaseVersion (stable > prerelease) instead of resolvedVersion
		// because catalog DB stores version-agnostic package data; the latest release
		// best represents the current dependency surface for OSS selection.
		depsGraphResults = s.enrichDependencyCounts(ctx, purls, analyses)
	}()
	enrichWg.Wait()

	// Advisory severity enrichment (best-effort).
	// Runs after populateReleaseInfo (via populateAnalysisFromBatchResult) so advisory IDs are available.
	s.enrichAdvisorySeverity(ctx, analyses)

	// Transitive advisory enrichment (best-effort).
	// Reuses dependency graph data from enrichDependencyCounts to avoid redundant API calls.
	// Runs after enrichAdvisorySeverity so direct advisories are already enriched.
	s.enrichTransitiveAdvisories(ctx, purls, analyses, depsGraphResults)

	total := len(analyses)
	pct := func(n int) float64 {
		if total == 0 {
			return 0
		}
		return float64(n) / float64(total)
	}
	slog.Debug("batch_summary", "total", total,
		"missing_repo_count", missingRepoCount, "missing_repo_pct", pct(missingRepoCount),
		"missing_project_count", missingProjectCount, "missing_project_pct", pct(missingProjectCount),
		"missing_scorecard_count", missingScorecardCount, "missing_scorecard_pct", pct(missingScorecardCount),
	)

	slog.Debug("completed_purl_batch", "count", total, "missing_repo_count", missingRepoCount, "missing_project_count", missingProjectCount, "missing_scorecard_count", missingScorecardCount)
	return analyses, nil
}

// enrichDependentCounts fetches dependent counts from deps.dev (npm/Maven/PyPI/Cargo),
// RubyGems, and Packagist, then populates Analysis.DependentCount for each PURL.
//
// DDD Layer: Infrastructure (parallel processing, best-effort enrichment)
func (s *IntegrationService) enrichDependentCounts(ctx context.Context, purls []string, analyses map[string]*domain.Analysis) {
	// Phase 1: deps.dev batch (npm, Maven, PyPI, Cargo)
	// Use EffectivePURL (versioned) when available, because FetchDependentCount
	// skips versionless PURLs. Build a mapping from effective PURL back to original key.
	effectivePURLs := make([]string, 0, len(purls))
	effectiveToOriginal := make(map[string]string, len(purls))
	for _, p := range purls {
		a := analyses[p]
		if a == nil {
			continue
		}
		ep := a.EffectivePURL
		if ep == "" {
			ep = p
		}
		// FetchDependentCount requires a versioned PURL. If the effective PURL
		// is versionless, inject the stable release version (resolved earlier by
		// populateReleaseInfo) using purl.WithVersion to handle qualifiers/subpaths safely.
		if !strings.Contains(ep, "@") {
			if v := resolvedVersion(a); v != "" {
				if versioned, err := purl.WithVersion(ep, v); err == nil {
					ep = versioned
				}
			}
		}
		effectivePURLs = append(effectivePURLs, ep)
		effectiveToOriginal[ep] = p
	}
	// FetchDependentCountBatch returns results keyed by CanonicalKey (versionless,
	// qualifiers preserved). We iterate effectiveToOriginal and canonicalize each ep
	// to match those keys, then map back to the original PURL in the analyses map.
	depsdevResults := s.depsdevClient.FetchDependentCountBatch(ctx, effectivePURLs)
	for ep, originalKey := range effectiveToOriginal {
		a := analyses[originalKey]
		if a == nil {
			continue
		}
		key := purl.CanonicalKey(ep)
		if key == "" {
			key = ep
		}
		if resp, ok := depsdevResults[key]; ok && resp != nil {
			a.DependentCount = resp.DependentCount
		}
	}

	// Phase 2: RubyGems and Packagist (not covered by deps.dev)
	// Each goroutine writes to a distinct *Analysis pointer (one per PURL key),
	// so no mutex is needed for DependentCount writes.
	parser := purl.NewParser()
	var wg sync.WaitGroup
	for _, p := range purls {
		a := analyses[p]
		if a == nil || a.DependentCount > 0 {
			continue // already populated by deps.dev
		}
		if a.Package == nil {
			continue
		}
		eco := strings.ToLower(strings.TrimSpace(a.Package.Ecosystem))

		switch eco {
		case "gem", "rubygems":
			if s.rubygemsClient == nil {
				continue
			}
			parsed, err := parser.Parse(p)
			if err != nil {
				continue
			}
			wg.Add(1)
			go func(analysis *domain.Analysis, name string) {
				defer wg.Done()
				count, err := s.rubygemsClient.GetReverseDependencyCount(ctx, name)
				if err != nil {
					slog.Warn("rubygems_dependent_count_failed", "name", name, "error", err)
					return
				}
				analysis.DependentCount = count
			}(a, parsed.GetPackageName())

		case "packagist", "composer":
			if s.packagistClient == nil {
				continue
			}
			parsed, err := parser.Parse(p)
			if err != nil {
				continue
			}
			vendor := parsed.Namespace()
			name := parsed.GetPackageName()
			if vendor == "" || name == "" {
				continue
			}
			wg.Add(1)
			go func(analysis *domain.Analysis, v, n string) {
				defer wg.Done()
				count, err := s.packagistClient.GetDependentCount(ctx, v, n)
				if err != nil {
					slog.Warn("packagist_dependent_count_failed", "vendor", v, "name", n, "error", err)
					return
				}
				analysis.DependentCount = count
			}(a, vendor, name)
		}
	}
	wg.Wait()
}

// dependenciesSupportedEcosystem reports whether the deps.dev GetDependencies endpoint
// supports the given ecosystem. Only npm, cargo, maven, and pypi are supported.
func dependenciesSupportedEcosystem(eco string) bool {
	switch strings.ToLower(strings.TrimSpace(eco)) {
	case "npm", "cargo", "maven", "pypi":
		return true
	default:
		return false
	}
}

// enrichDependencyCounts fetches dependency counts (direct + transitive) from deps.dev
// and populates Analysis.DirectDepsCount and Analysis.TransitiveDepsCount.
// Returns the raw DependenciesResponse map for downstream use (e.g., transitive advisory enrichment).
//
// Version selection: StableVersion > PrereleaseVersion (latest release for catalog DB).
// Supported ecosystems: npm, cargo, maven, pypi (deps.dev limitation).
// Unsupported ecosystems are filtered out before making API requests to avoid
// wasting HTTP round-trips on guaranteed 404 responses (important for large batches).
//
// DDD Layer: Infrastructure (best-effort enrichment)
func (s *IntegrationService) enrichDependencyCounts(ctx context.Context, purls []string, analyses map[string]*domain.Analysis) map[string]*depsdev.DependenciesResponse {
	effectivePURLs := make([]string, 0, len(purls))
	// Multiple original PURLs may resolve to the same effective PURL (e.g., case
	// variants like pkg:npm/React and pkg:npm/react). Use a slice of original keys
	// so all analyses get populated, not just the last one.
	effectiveToOriginals := make(map[string][]string, len(purls))
	seen := make(map[string]bool, len(purls))
	for _, p := range purls {
		a := analyses[p]
		if a == nil {
			continue
		}
		// Skip ecosystems not supported by the :dependencies endpoint.
		if a.Package == nil || !dependenciesSupportedEcosystem(a.Package.Ecosystem) {
			continue
		}
		ep := a.EffectivePURL
		if ep == "" {
			ep = p
		}
		// The :dependencies endpoint requires a versioned PURL.
		// Use the latest release version (stable > prerelease) for catalog consistency.
		if !strings.Contains(ep, "@") {
			if v := latestReleaseVersion(a); v != "" {
				if versioned, err := purl.WithVersion(ep, v); err == nil {
					ep = versioned
				}
			}
		}
		// Skip entries where version could not be resolved: the deps.dev :dependencies
		// endpoint requires a versioned PURL, and an unresolved version would round-trip
		// through the batch layer only to be silently dropped. Logging here makes the
		// condition diagnosable (distinguishes "no version" from "API returned empty").
		if !strings.Contains(ep, "@") {
			slog.Debug("dependency_count_skipped_versionless", "purl", p, "ecosystem", a.Package.Ecosystem)
			continue
		}
		// Deduplicate effective PURLs to avoid redundant API calls.
		if !seen[ep] {
			effectivePURLs = append(effectivePURLs, ep)
			seen[ep] = true
		}
		effectiveToOriginals[ep] = append(effectiveToOriginals[ep], p)
	}

	slog.Debug("dependency_count_filtered", "total_purls", len(purls), "supported_purls", len(effectivePURLs))

	depsResults := s.depsdevClient.FetchDependenciesBatch(ctx, effectivePURLs)

	// Map results back to original PURLs and populate counts.
	// Also build a per-original-PURL map for downstream transitive advisory enrichment.
	// HasDependencyGraph is set whenever deps.dev returned a response, even when the
	// counts are zero — this lets callers distinguish "genuine leaf package" (e.g.,
	// react@19) from "graph not collected" (unsupported ecosystem, 404, etc.).
	perPURL := make(map[string]*depsdev.DependenciesResponse, len(purls))
	for ep, originalKeys := range effectiveToOriginals {
		key := purl.CanonicalKey(ep)
		if key == "" {
			key = ep
		}
		resp, ok := depsResults[key]
		if !ok || resp == nil {
			continue
		}
		direct, transitive := resp.CountByRelation()
		for _, originalKey := range originalKeys {
			a := analyses[originalKey]
			if a == nil {
				continue
			}
			a.DirectDepsCount, a.TransitiveDepsCount = direct, transitive
			a.HasDependencyGraph = true
			perPURL[originalKey] = resp
		}
	}
	return perPURL
}

// latestReleaseVersion returns the best version string for dependency count queries.
// Preference order:
//  1. StableVersion       — latest stable release (best representation of the current dependency surface)
//  2. MaxSemverVersion    — highest semver across published versions (often matches stable; useful when deps.dev data lacks IsDefault/stable flags)
//  3. PreReleaseVersion   — latest non-stable release, for ecosystems/packages where no stable exists yet
//  4. RequestedVersion    — user-pinned version from the request, if present
//  5. Package.Version     — version embedded in the original PURL
//
// The first three probe "what does this package currently depend on". The
// last two ensure we still issue a deps.dev query when upstream release
// metadata is sparse but a concrete version is known from the input.
func latestReleaseVersion(a *domain.Analysis) string {
	if a.ReleaseInfo != nil {
		if v := a.ReleaseInfo.StableVersion; v != nil && v.Version != "" {
			return v.Version
		}
		if v := a.ReleaseInfo.MaxSemverVersion; v != nil && v.Version != "" {
			return v.Version
		}
		if v := a.ReleaseInfo.PreReleaseVersion; v != nil && v.Version != "" {
			return v.Version
		}
		if v := a.ReleaseInfo.RequestedVersion; v != nil && v.Version != "" {
			return v.Version
		}
	}
	if a.Package != nil {
		if v := strings.TrimSpace(a.Package.Version); v != "" {
			return v
		}
	}
	return ""
}

// resolvedVersion returns the best available version string from an Analysis.
// Preference: Package.Version > StableVersion > MaxSemverVersion.
func resolvedVersion(a *domain.Analysis) string {
	if a.Package != nil && strings.TrimSpace(a.Package.Version) != "" {
		return strings.TrimSpace(a.Package.Version)
	}
	if a.ReleaseInfo != nil {
		if a.ReleaseInfo.StableVersion != nil && a.ReleaseInfo.StableVersion.Version != "" {
			return a.ReleaseInfo.StableVersion.Version
		}
		if a.ReleaseInfo.MaxSemverVersion != nil && a.ReleaseInfo.MaxSemverVersion.Version != "" {
			return a.ReleaseInfo.MaxSemverVersion.Version
		}
	}
	return ""
}
