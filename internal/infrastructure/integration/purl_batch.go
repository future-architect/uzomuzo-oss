package integration

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo/internal/common"
	"github.com/future-architect/uzomuzo/internal/common/purl"
	domain "github.com/future-architect/uzomuzo/internal/domain/analysis"
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

	// Dependent count enrichment (best-effort)
	s.enrichDependentCounts(ctx, purls, analyses)

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
