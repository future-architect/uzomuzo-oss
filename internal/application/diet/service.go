// Package diet orchestrates the 4-phase dependency diet analysis pipeline.
package diet

import (
	"context"
	"fmt"
	"log/slog"
	"math"
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

// DietInput contains the inputs for a diet analysis run.
type DietInput struct {
	SBOMData   []byte
	SBOMPath   string
	SourceRoot string // empty = skip source analysis
}

// Service orchestrates the 4-phase diet pipeline.
type Service struct {
	graphAnalyzer   GraphAnalyzer
	sourceAnalyzer  SourceAnalyzer // nil = skip source analysis
	analysisService *application.AnalysisService
}

// NewService creates a new diet service.
func NewService(
	graphAnalyzer GraphAnalyzer,
	sourceAnalyzer SourceAnalyzer,
	analysisService *application.AnalysisService,
) *Service {
	return &Service{
		graphAnalyzer:   graphAnalyzer,
		sourceAnalyzer:  sourceAnalyzer,
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
	slog.Info("Phase 1 complete",
		"direct", len(graphResult.DirectDeps),
		"totalTransitive", graphResult.TotalTransitive,
	)

	// Phase 2: Static analysis (optional)
	var couplingResults map[string]*domaindiet.CouplingAnalysis
	if s.sourceAnalyzer != nil && input.SourceRoot != "" {
		slog.Info("Phase 2: Analyzing source code coupling", "source", input.SourceRoot)
		importPaths := buildImportPaths(graphResult.DirectDeps)
		couplingResults, err = s.sourceAnalyzer.AnalyzeCoupling(ctx, input.SourceRoot, importPaths)
		if err != nil {
			slog.Warn("Source analysis failed, continuing without coupling data", "error", err)
			couplingResults = nil
		}
		slog.Info("Phase 2 complete", "analyzed", len(couplingResults))
	} else {
		slog.Info("Phase 2: Skipped (no source root provided)")
	}

	// Phase 3: Health signals
	slog.Info("Phase 3: Fetching health signals", "count", len(graphResult.DirectDeps))
	healthResults := make(map[string]*domain.Analysis)
	if s.analysisService != nil && len(graphResult.DirectDeps) > 0 {
		healthResults, err = s.analysisService.ProcessBatchPURLs(ctx, graphResult.DirectDeps)
		if err != nil {
			slog.Warn("Health signal fetch failed, continuing without health data", "error", err)
			healthResults = make(map[string]*domain.Analysis)
		}
	}
	slog.Info("Phase 3 complete", "fetched", len(healthResults))

	// Phase 4: Scoring and prioritization
	slog.Info("Phase 4: Computing scores and ranking")
	entries := s.buildEntries(graphResult, couplingResults, healthResults)
	domaindiet.RankEntries(entries)
	summary := domaindiet.ComputeSummary(entries)

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
) []domaindiet.DietEntry {
	entries := make([]domaindiet.DietEntry, 0, len(graph.DirectDeps))
	for _, purl := range graph.DirectDeps {
		entry := domaindiet.DietEntry{
			PURL:     purl,
			Relation: domaindiet.RelationDirect,
		}

		entry.Name, entry.Ecosystem, entry.Version = parsePURLParts(purl)

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

		if a, ok := health[purl]; ok && a != nil {
			entry.Health = computeHealthSignals(a)
		} else {
			entry.Health = domaindiet.HealthSignals{HealthRisk: 0.5} // unknown = moderate
		}

		entry.Scores = domaindiet.ComputeImpactScore(
			entry.Graph, entry.Coupling, entry.Health, graph.TotalTransitive,
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
		h.MaintenanceStatus = a.FinalMaintenanceStatus().String()
		h.HealthRisk = 0.2
		// Elevate risk for non-active statuses
		switch a.FinalMaintenanceStatus() {
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
			h.MaintenanceStatus = "Archived"
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

// buildImportPaths creates a mapping from PURL to probable import paths.
// This is a best-effort mapping used for source coupling analysis.
func buildImportPaths(purls []string) map[string][]string {
	result := make(map[string][]string, len(purls))
	for _, p := range purls {
		parsed, err := packageurl.FromString(p)
		if err != nil {
			continue
		}
		var importPath string
		switch parsed.Type {
		case "golang":
			if parsed.Namespace != "" {
				importPath = parsed.Namespace + "/" + parsed.Name
			} else {
				importPath = parsed.Name
			}
		case "npm":
			if parsed.Namespace != "" {
				// packageurl-go already includes '@' in scoped namespaces (e.g. "@types")
				importPath = parsed.Namespace + "/" + parsed.Name
			} else {
				importPath = parsed.Name
			}
		case "pypi":
			importPath = parsed.Name
		case "maven":
			if parsed.Namespace != "" {
				importPath = parsed.Namespace + "." + parsed.Name
			} else {
				importPath = parsed.Name
			}
		default:
			importPath = parsed.Name
		}
		if importPath != "" {
			result[p] = []string{importPath}
		}
	}
	return result
}
