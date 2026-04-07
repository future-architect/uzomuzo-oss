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
	entries := s.buildEntries(graphResult, couplingResults, healthResults)
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
			// Normalize PyPI distribution name to Python import convention:
			// replace hyphens with underscores and lowercase (e.g., "PyYAML" → "pyyaml").
			importPath = strings.ToLower(strings.ReplaceAll(parsed.Name, "-", "_"))
		case "maven":
			if paths := buildMavenImportPaths(parsed); len(paths) > 0 {
				result[p] = paths
			}
			continue
		default:
			importPath = parsed.Name
		}
		if importPath != "" {
			result[p] = []string{importPath}
		}
	}
	return result
}

// mavenPackageOverrides maps "groupId/artifactId" to known Java package
// prefixes for libraries where the Maven groupId does not match the actual
// Java package name.  Add entries as real-world mismatches are discovered.
var mavenPackageOverrides = map[string][]string{
	"cglib/cglib":                             {"net.sf.cglib"},
	"com.google.code.gson/gson":               {"com.google.gson"},
	"commons-beanutils/commons-beanutils":     {"org.apache.commons.beanutils"},
	"commons-codec/commons-codec":             {"org.apache.commons.codec"},
	"commons-collections/commons-collections": {"org.apache.commons.collections"},
	"commons-io/commons-io":                   {"org.apache.commons.io"},
	"commons-logging/commons-logging":         {"org.apache.commons.logging"},
	"junit/junit":                             {"junit", "org.junit"},
	"log4j/log4j":                             {"org.apache.log4j"},
}

// buildMavenImportPaths generates candidate import path prefixes for a Maven PURL.
// It combines well-known overrides with heuristic candidates (groupId, groupId.artifactId).
func buildMavenImportPaths(parsed packageurl.PackageURL) []string {
	key := parsed.Namespace + "/" + parsed.Name
	seen := make(map[string]struct{})
	var paths []string

	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}

	// 1. Well-known overrides take priority.
	for _, p := range mavenPackageOverrides[key] {
		add(p)
	}

	// 2. groupId (namespace) — the most common convention.
	// Skip when the namespace contains characters invalid in Java package names
	// (e.g. "commons-io"), since such candidates can never match real imports.
	if isJavaDottedPackageSafe(parsed.Namespace) {
		add(parsed.Namespace)
	}

	// 3. groupId.artifactId — covers cases where the package mirrors the full coordinate.
	// Skip when namespace == name (e.g. cglib/cglib → "cglib.cglib" is not a real package),
	// and skip when artifactId contains characters invalid in Java package names (e.g. hyphens).
	if parsed.Namespace != "" && parsed.Name != "" && parsed.Namespace != parsed.Name && isJavaPackageSafe(parsed.Name) {
		add(parsed.Namespace + "." + parsed.Name)
	}

	if len(paths) == 0 {
		// Fallback to artifactId only when nothing else is available.
		add(parsed.Name)
	}

	return paths
}

// isJavaPackageSafe reports whether s is a valid Java package name segment.
// The first character must be a letter, underscore, or dollar sign; subsequent
// characters may also include digits.  Maven artifactIds often contain hyphens
// (e.g. "commons-lang3") or start with digits (e.g. "3scale") which are not
// valid in Java identifiers and would never match a real import statement.
func isJavaPackageSafe(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '$' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// isJavaDottedPackageSafe reports whether s is a valid dot-separated Java
// package prefix (e.g. "org.apache.commons").  Each segment between dots must
// satisfy isJavaPackageSafe.
func isJavaDottedPackageSafe(s string) bool {
	if s == "" {
		return false
	}
	for _, seg := range strings.Split(s, ".") {
		if !isJavaPackageSafe(seg) {
			return false
		}
	}
	return true
}
