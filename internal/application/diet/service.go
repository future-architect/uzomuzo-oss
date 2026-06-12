// Package diet orchestrates the 4-phase dependency diet analysis pipeline.
package diet

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/application"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domaindiet "github.com/future-architect/uzomuzo-oss/internal/domain/diet"
	"github.com/package-url/packageurl-go"
)

// SourceAnalyzer abstracts static analysis of source code against dependencies.
type SourceAnalyzer interface {
	// AnalyzeCoupling scans the source tree and returns coupling data per PURL.
	// importPaths maps PURL -> []string of import paths for that ecosystem.
	AnalyzeCoupling(ctx context.Context, sourceRoot string, importPaths map[string][]string) (map[string]*domaindiet.CouplingAnalysis, error)
}

// GraphAnalyzer abstracts dependency graph analysis from an SBOM.
type GraphAnalyzer interface {
	AnalyzeGraph(ctx context.Context, sbomData []byte) (*domaindiet.GraphResult, error)
}

// PyPIImportResolver resolves Python import names from wheel metadata.
// This is the fallback used when heuristic import path guessing fails.
type PyPIImportResolver interface {
	// ResolveImportNames fetches the actual Python import names for a PyPI
	// package by downloading and inspecting the smallest wheel file.
	// It may return an error if metadata lookup, wheel download, or inspection
	// fails. Callers should treat such errors as non-fatal fallback failures
	// (graceful degradation) and continue with heuristic guesses or an empty
	// result when appropriate.
	ResolveImportNames(ctx context.Context, packageName string) ([]string, error)
}

// DietInput contains the inputs for a diet analysis run.
type DietInput struct {
	SBOMData   []byte
	SBOMPath   string
	SourceRoot string // empty = skip source analysis
	// ToolDeps is a set of module paths declared in go.mod tool directives
	// (Go 1.24+). These are dev/CI executables that intentionally have zero
	// source imports and should not be flagged as unused.
	ToolDeps map[string]struct{}
}

// Service orchestrates the 4-phase diet pipeline.
type Service struct {
	graphAnalyzer   GraphAnalyzer
	sourceAnalyzer  SourceAnalyzer     // nil = skip source analysis
	pypiResolver    PyPIImportResolver // nil = skip wheel fallback
	analysisService *application.AnalysisService
}

// NewService creates a new diet service.
func NewService(
	graphAnalyzer GraphAnalyzer,
	sourceAnalyzer SourceAnalyzer,
	pypiResolver PyPIImportResolver,
	analysisService *application.AnalysisService,
) *Service {
	return &Service{
		graphAnalyzer:   graphAnalyzer,
		sourceAnalyzer:  sourceAnalyzer,
		pypiResolver:    pypiResolver,
		analysisService: analysisService,
	}
}

// Run executes the full 4-phase diet pipeline.
func (s *Service) Run(ctx context.Context, input DietInput) (*domaindiet.DietPlan, error) {
	// Phase 1: Graph analysis
	slog.Info("Phase 1: Analyzing dependency graph from SBOM")
	graphResult, err := s.graphAnalyzer.AnalyzeGraph(ctx, input.SBOMData)
	if err != nil {
		return nil, fmt.Errorf("graph analysis failed: %w", err)
	}
	// Filter workspace-local packages (npm monorepo internals) before analysis.
	preFilterCount := len(graphResult.DirectDeps)
	graphResult.DirectDeps = filterWorkspaceDeps(graphResult.DirectDeps)

	slog.Info("Phase 1 complete",
		"direct", len(graphResult.DirectDeps),
		"totalTransitive", graphResult.TotalTransitive,
		"workspaceDepsFiltered", preFilterCount-len(graphResult.DirectDeps),
	)

	// Phase 2 & 3: run concurrently (both only depend on graphResult from Phase 1)
	var couplingResults map[string]*domaindiet.CouplingAnalysis
	var healthResults map[string]*domain.Analysis
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Phase 2: Static analysis (optional)
		if s.sourceAnalyzer != nil && input.SourceRoot != "" {
			slog.Info("Phase 2: Analyzing source code coupling", "source", input.SourceRoot)
			importPaths := buildImportPaths(graphResult.DirectDeps)
			var couplingErr error
			couplingResults, couplingErr = s.sourceAnalyzer.AnalyzeCoupling(ctx, input.SourceRoot, importPaths)
			if couplingErr != nil {
				slog.Warn("Phase 2 failed, continuing without coupling data", "error", couplingErr)
				couplingResults = nil
			} else if len(couplingResults) == 0 {
				slog.Warn("Phase 2: no imports matched any dependency — verify --source points to the correct directory", "source", input.SourceRoot)
			} else {
				slog.Info("Phase 2 complete", "analyzed", len(couplingResults))
			}

			// Phase 2.5: Wheel-based fallback for PyPI packages with zero matches.
			// Gate on couplingErr (not couplingResults) because AnalyzeCoupling
			// returns (nil, nil) when no imports matched — that nil map is exactly
			// the scenario the wheel fallback should recover from.
			if s.pypiResolver != nil && couplingErr == nil {
				retryPaths := s.resolveUnmatchedPyPI(ctx, graphResult.DirectDeps, couplingResults)
				if len(retryPaths) > 0 {
					slog.Info("Phase 2.5: Retrying coupling with wheel-resolved import names", "count", len(retryPaths))

					// Build combined importPaths (original heuristic + wheel-resolved)
					// so AnalyzeCoupling sees the full dependency set and collision
					// attribution stays consistent with Phase 2.
					combinedImportPaths := make(map[string][]string, len(importPaths))
					for purl, paths := range importPaths {
						copiedPaths := make([]string, len(paths))
						copy(copiedPaths, paths)
						combinedImportPaths[purl] = copiedPaths
					}
					for purl, paths := range retryPaths {
						seen := make(map[string]struct{}, len(combinedImportPaths[purl])+len(paths))
						mergedPaths := make([]string, 0, len(combinedImportPaths[purl])+len(paths))
						for _, p := range combinedImportPaths[purl] {
							if _, ok := seen[p]; ok {
								continue
							}
							seen[p] = struct{}{}
							mergedPaths = append(mergedPaths, p)
						}
						for _, p := range paths {
							if _, ok := seen[p]; ok {
								continue
							}
							seen[p] = struct{}{}
							mergedPaths = append(mergedPaths, p)
						}
						combinedImportPaths[purl] = mergedPaths
					}

					retryCoupling, retryErr := s.sourceAnalyzer.AnalyzeCoupling(ctx, input.SourceRoot, combinedImportPaths)
					if retryErr != nil {
						slog.Warn("Phase 2.5 failed, continuing with heuristic results", "error", retryErr)
					} else {
						couplingResults = retryCoupling
						slog.Info("Phase 2.5 complete", "resolved", len(retryCoupling))
					}
				}
			}
		} else {
			slog.Info("Phase 2: Skipped (no source root provided)")
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Phase 3: Health signals
		slog.Info("Phase 3: Fetching health signals", "count", len(graphResult.DirectDeps))
		healthResults = make(map[string]*domain.Analysis)
		if s.analysisService != nil && len(graphResult.DirectDeps) > 0 {
			var healthErr error
			healthResults, healthErr = s.analysisService.ProcessBatchPURLs(ctx, graphResult.DirectDeps)
			if healthErr != nil {
				slog.Warn("Health signal fetch failed, continuing without health data", "error", healthErr)
				healthResults = make(map[string]*domain.Analysis)
			}
		}
		slog.Info("Phase 3 complete", "fetched", len(healthResults))
	}()

	wg.Wait()

	// Phase 4: Scoring and prioritization
	slog.Info("Phase 4: Computing scores and ranking")
	maxExclusive := graphResult.MaxExclusiveTransitiveCount()
	entries := s.buildEntries(graphResult, couplingResults, healthResults, maxExclusive, input.ToolDeps)
	domaindiet.RankEntries(entries)
	summary := domaindiet.ComputeSummary(entries, graphResult.TotalTransitive)

	plan := &domaindiet.DietPlan{
		Entries:    entries,
		Summary:    summary,
		SBOMPath:   input.SBOMPath,
		SourceRoot: input.SourceRoot,
		AnalyzedAt: time.Now(),
	}

	slog.Info("Diet analysis complete",
		"entries", len(entries),
		"easyWins", summary.EasyWins,
		"unusedDirect", summary.UnusedDirect,
	)

	return plan, nil
}

func (s *Service) buildEntries(
	graph *domaindiet.GraphResult,
	coupling map[string]*domaindiet.CouplingAnalysis,
	health map[string]*domain.Analysis,
	maxExclusive int,
	toolDeps map[string]struct{},
) []domaindiet.DietEntry {
	entries := make([]domaindiet.DietEntry, 0, len(graph.DirectDeps))
	for _, purl := range graph.DirectDeps {
		entry := domaindiet.DietEntry{
			PURL:     purl,
			Relation: domaindiet.RelationDirect,
		}

		entry.Name, entry.Ecosystem, entry.Version = parsePURLParts(purl)

		// Check if this dependency is a Go tool directive dep.
		if toolDeps != nil {
			if _, ok := toolDeps[entry.Name]; ok {
				entry.Scope = domaindiet.ScopeTool
			}
		}

		// Check if this dependency is a Maven runtime dep (reflection-loaded).
		if entry.Scope == "" && entry.Ecosystem == "maven" {
			if _, ok := mavenRuntimeDeps[strings.ToLower(entry.Name)]; ok {
				entry.Scope = domaindiet.ScopeRuntime
			}
		}

		if m, ok := graph.Metrics[purl]; ok {
			entry.Graph = *m
		}

		if coupling != nil {
			if c, ok := coupling[purl]; ok {
				entry.Coupling = *c
			} else {
				entry.Coupling = domaindiet.CouplingAnalysis{IsUnused: true}
			}
		}

		// Non-static-import scopes (tool directives, runtime/reflection deps)
		// intentionally have zero source imports. Override IsUnused so they are
		// not flagged as trivial removals, and provide a minimal synthetic
		// coupling signal so downstream scoring does not treat them as zero-effort.
		if entry.Scope == domaindiet.ScopeTool || entry.Scope == domaindiet.ScopeRuntime {
			if entry.Coupling.IsUnused {
				entry.Coupling.IsUnused = false
			}
			if entry.Coupling.ImportFileCount == 0 &&
				entry.Coupling.CallSiteCount == 0 &&
				entry.Coupling.APIBreadth == 0 {
				entry.Coupling.APIBreadth = 1
			}
		}

		if a, ok := health[purl]; ok && a != nil {
			entry.Health = computeHealthSignals(a)
		} else {
			entry.Health = domaindiet.HealthSignals{HealthRisk: 0.5} // unknown = moderate
		}

		entry.Scores = domaindiet.ComputeImpactScore(
			entry.Graph, entry.Coupling, entry.Health, maxExclusive,
		)

		entries = append(entries, entry)
	}
	return entries
}

func computeHealthSignals(a *domain.Analysis) domaindiet.HealthSignals {
	h := domaindiet.HealthSignals{
		OverallScore: a.OverallScore,
	}

	// Map EOL state to health signals.
	// domain.EOLEndOfLife is the terminal state; we use FinalMaintenanceStatus()
	// to get the refined label (EOL-Confirmed vs EOL-Effective).
	switch a.EOL.State {
	case domain.EOLEndOfLife:
		h.IsEOL = true
		ms := a.FinalMaintenanceStatus()
		h.MaintenanceStatus = ms.String()
		h.HealthRisk = 0.9
		if ms == domain.LabelEOLEffective {
			h.HealthRisk = 0.85
		}
	case domain.EOLScheduled:
		h.MaintenanceStatus = domain.LabelEOLScheduled.String()
		h.HealthRisk = 0.7
	default:
		ms := a.FinalMaintenanceStatus()
		h.MaintenanceStatus = ms.String()
		h.HealthRisk = 0.2
		// Elevate risk for non-active statuses
		switch ms {
		case domain.LabelStalled:
			h.IsStalled = true
			h.HealthRisk = 0.6
		case domain.LabelLegacySafe:
			h.HealthRisk = 0.4
		case domain.LabelReviewNeeded:
			h.HealthRisk = 0.5
		}
	}

	// Check repo state for archived/stalled
	if a.RepoState != nil {
		if a.RepoState.IsArchived {
			h.MaintenanceStatus = domaindiet.MaintenanceStatusArchived
			h.HealthRisk = math.Max(h.HealthRisk, 0.85)
		}
		if a.RepoState.DaysSinceLastCommit > 365 {
			h.IsStalled = true
			h.HealthRisk = math.Max(h.HealthRisk, 0.6)
		}
	}

	// Vulnerability info from the latest version detail
	if a.ReleaseInfo != nil {
		if vd := a.ReleaseInfo.LatestVersionDetail(); vd != nil {
			for _, adv := range vd.Advisories {
				h.VulnerabilityCount++
				if adv.CVSS3Score > h.MaxCVSSScore {
					h.MaxCVSSScore = adv.CVSS3Score
				}
			}
			if h.VulnerabilityCount > 0 {
				h.HasVulnerabilities = true
				h.HealthRisk = math.Min(h.HealthRisk+h.MaxCVSSScore/10.0*0.2, 1.0)
			}
		}
	}

	// Low Scorecard score increases risk
	if a.OverallScore > 0 {
		h.HealthRisk = math.Min(h.HealthRisk+(1.0-a.OverallScore/10.0)*0.1, 1.0)
	}

	return h
}

// parsePURLParts extracts name, ecosystem, version from a PURL string.
func parsePURLParts(purlStr string) (name, ecosystem, version string) {
	parsed, err := packageurl.FromString(purlStr)
	if err != nil {
		return purlStr, "", ""
	}
	n := parsed.Name
	if parsed.Namespace != "" {
		n = parsed.Namespace + "/" + parsed.Name
	}
	return n, parsed.Type, parsed.Version
}

// resolveUnmatchedPyPI identifies PyPI PURLs that had zero coupling matches
// and attempts to resolve their import names via wheel metadata. Returns
// a map of PURL → resolved import paths suitable for a retry AnalyzeCoupling call.
func (s *Service) resolveUnmatchedPyPI(
	ctx context.Context,
	directDeps []string,
	couplingResults map[string]*domaindiet.CouplingAnalysis,
) map[string][]string {
	retryPaths := make(map[string][]string)
	for _, purl := range directDeps {
		parsed, err := packageurl.FromString(purl)
		if err != nil || parsed.Type != "pypi" {
			continue
		}
		// Only retry packages that got zero matches from heuristic paths.
		if _, found := couplingResults[purl]; found {
			continue
		}
		names, resolveErr := s.pypiResolver.ResolveImportNames(ctx, parsed.Name)
		if resolveErr != nil {
			slog.Debug("pypi_wheel: resolve failed, skipping", "package", parsed.Name, "error", resolveErr)
			continue
		}
		if len(names) > 0 {
			retryPaths[purl] = names
		}
	}
	return retryPaths
}
