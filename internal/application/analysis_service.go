package application

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/eolevaluator"
	exportcsv "github.com/future-architect/uzomuzo-oss/internal/infrastructure/export/csv"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/integration"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/maven"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/npmjs"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/nuget"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/packagist"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/pypi"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/rubygems"
	// TODO: rename directory successor -> eolevaluator; after physical move adjust import
)

// AnalysisEnricher is called between Phase 1 (registry EOL) and Phase 3
// (lifecycle/build-health assessment). It may mutate Analysis.EOL and
// Analysis.Error fields. It MUST NOT modify other aggregate fields.
//
// DDD Layer: Application (contract definition)
// Implementations: Infrastructure layer (e.g., catalog enricher in private repo)
type AnalysisEnricher func(ctx context.Context, analyses map[string]*domain.Analysis) error

// Option configures an AnalysisService.
type Option func(*AnalysisService)

// WithEnricher appends an enricher to the pipeline between Phase 1 and Phase 3.
func WithEnricher(e AnalysisEnricher) Option {
	return func(s *AnalysisService) {
		s.enrichers = append(s.enrichers, e)
	}
}

// AnalysisService provides application-level analysis operations
//
// DDD Layer: Application (use case orchestration)
// Responsibilities: Orchestrates domain objects, coordinates business workflows
type AnalysisService struct {
	integrationService *integration.IntegrationService
	cfg                *config.Config
	enrichers          []AnalysisEnricher
	// packagistClient is stored here (unlike other infra clients) because
	// ProcessBatch* methods pass it to eolevaluator.NewEvaluator to share
	// the same instance (and its 5-min TTL cache) across calls.
	packagistClient *packagist.Client
	// pypiClient is shared across IntegrationService (Summary enrichment) and
	// the per-batch eolevaluator instance so both consumers reuse the same
	// 10-min in-memory cache and avoid duplicate PyPI fetches per package.
	pypiClient *pypi.Client
}

// GitHubClient returns the underlying GitHub client for rate limit inspection.
func (s *AnalysisService) GitHubClient() *github.Client {
	return s.integrationService.GitHubClient()
}

// NewAnalysisService creates a new AnalysisService that orchestrates
// analysis operations using the provided IntegrationService.
// It does not perform any external I/O at construction time.
func NewAnalysisService(integrationService *integration.IntegrationService, opts ...Option) *AnalysisService {
	s := &AnalysisService{
		integrationService: integrationService,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// NewAnalysisServiceFromConfig creates an AnalysisService from the given config.
//
// DDD Layer: Application (constructs Infrastructure dependencies)
// Note: Application layer encapsulates Infrastructure wiring to keep
// interfaces thin and domain pure.
func NewAnalysisServiceFromConfig(cfg *config.Config, opts ...Option) *AnalysisService {
	// Infrastructure layer components creation (responsibility of Application layer)
	githubClient := github.NewClient(cfg)
	rgClient := rubygems.NewClient()
	pkgClient := packagist.NewClient()
	pyClient := pypi.NewClient()
	mvClient := maven.NewClient()
	if u := strings.TrimSpace(cfg.Maven.BaseURL); u != "" {
		mvClient.SetBaseURL(u)
		slog.Debug("Maven base URL configured", "base_url", u)
	}
	depsdevClient := depsdev.NewDepsDevClient(&cfg.DepsDev)
	// Attach npmjs, RubyGems and Packagist clients to enable repository URL fallbacks
	depsdevClient = depsdevClient.
		WithNPM(npmjs.NewClient()).
		WithNuGet(nuget.NewClient()).
		WithMaven(mvClient).
		WithRubyGems(rgClient).
		WithPackagist(pkgClient).
		WithPyPI(pyClient)
	integrationService := integration.NewIntegrationService(githubClient, depsdevClient,
		integration.WithConfig(cfg),
		integration.WithRubyGemsClient(rgClient),
		integration.WithPackagistClient(pkgClient),
		integration.WithPyPIClient(pyClient),
		integration.WithMavenClient(mvClient),
	)

	s := &AnalysisService{
		integrationService: integrationService,
		cfg:                cfg,
		packagistClient:    pkgClient,
		pypiClient:         pyClient,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ProcessBatchPURLs processes multiple PURLs and returns domain Analysis results
//
// DDD Layer: Application (use case orchestration)
// Business Logic: Orchestrates batch processing, applies lifecycle assessment logic
func (s *AnalysisService) ProcessBatchPURLs(ctx context.Context, purls []string) (map[string]*domain.Analysis, error) {
	if len(purls) == 0 {
		return make(map[string]*domain.Analysis), nil
	}

	// Delegate parallel processing to Infrastructure layer
	analyses, err := s.integrationService.AnalyzeFromPURLs(ctx, purls)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch batch analyses: %w", err)
	}

	// Phase 1: Evaluate base EOL from primary (non-catalog) deterministic sources
	eolEvaluator := eolevaluator.NewEvaluator(s.packagistClient)
	if s.pypiClient != nil {
		// Reuse the integration-phase PyPI client so the cache populated by
		// enrichPyPISummary is reused here, eliminating duplicate fetches per package.
		eolEvaluator.SetPyPIClient(s.pypiClient)
	}
	if s.cfg != nil { // mirror alignment
		if u := s.cfg.Maven.BaseURL; strings.TrimSpace(u) != "" {
			mv := maven.NewClient()
			mv.SetBaseURL(u)
			eolEvaluator.SetMavenClient(mv)
			slog.Debug("Maven base URL configured for EOL evaluator", "base_url", u)
		}
	}
	eolMap, evalErr := eolEvaluator.EvaluateBatch(ctx, analyses)
	if evalErr != nil {
		slog.Warn("base_eol_evaluate_failed", "error", evalErr)
	}
	for key, analysis := range analyses { // only assign base EOL now
		if analysis == nil {
			continue
		}
		e, ok := eolMap[key]
		if !ok {
			continue
		}
		if analysis.Error == nil {
			analysis.EOL = e
			continue
		}
		// Registry fallback: when deps.dev cannot find a package but a registry-based
		// evaluator (PyPI classifier, Packagist abandoned, NuGet deprecated, npm
		// deprecated, Maven relocated) determines a terminal EOL state, apply the
		// result and clear the not-found error so the analysis enters the normal
		// assessment pipeline.
		if common.IsResourceNotFoundError(analysis.Error) && isRegistryResolvedEOL(e) {
			analysis.EOL = e
			slog.Info("registry_fallback_resolved",
				"purl", key,
				"eol_state", string(e.State),
				"source", eolEvidenceSource(e),
			)
			analysis.Error = nil
		}
	}

	// Repo-URL fallback: when deps.dev could not find a package but a fallback
	// resolver chain resolved a repository URL and project data was fetched,
	// clear the not-found error so lifecycle assessment can proceed.
	for key, analysis := range analyses {
		if analysis == nil || analysis.Error == nil {
			continue
		}
		if !common.IsResourceNotFoundError(analysis.Error) {
			continue
		}
		if analysis.RepoURL != "" && analysis.Repository != nil {
			slog.Info("repo_url_fallback_resolved",
				"purl", key,
				"repo_url", analysis.RepoURL,
			)
			analysis.Error = nil
		}
	}

	// Phase 2: Run enrichers (catalog EOL, etc.) before lifecycle/build assessments
	for _, enrich := range s.enrichers {
		if err := enrich(ctx, analyses); err != nil {
			slog.Warn("enricher_failed", "error", err)
		}
	}

	// Phase 3: Run assessments (now seeing enricher-influenced EOL state)
	composite := domain.NewCompositeAssessor(
		domain.NewLifecycleAssessorService(),
		domain.NewBuildHealthAssessorService(),
	)
	for key, analysis := range analyses {
		if analysis == nil || analysis.Error != nil {
			continue
		}
		in := domain.AssessmentInput{Analysis: analysis, Scores: analysis.Scores, EOL: analysis.EOL}
		axisMap, err := composite.AssessAll(ctx, in)
		if err != nil {
			slog.Debug("Assess composite failed", "purl", key, "error", err)
			continue
		}
		if len(axisMap) == 0 {
			continue
		}
		if analysis.AxisResults == nil {
			analysis.AxisResults = make(map[domain.AssessmentAxis]*domain.AssessmentResult)
		}
		for ax, r := range axisMap {
			analysis.AxisResults[ax] = r
		}
	}

	return analyses, nil
}

// ProcessBatchGitHubURLs processes multiple GitHub URLs and returns domain Analysis results
//
// DDD Layer: Application (use case orchestration)
// Business Logic: Batch GitHub URL processing, lifecycle assessment application
func (s *AnalysisService) ProcessBatchGitHubURLs(ctx context.Context, githubURLs []string) (map[string]*domain.Analysis, error) {
	if len(githubURLs) == 0 {
		return make(map[string]*domain.Analysis), nil
	}
	// Delegate to Infrastructure layer for batch processing
	analyses, err := s.integrationService.AnalyzeFromGitHubURLs(ctx, githubURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch batch GitHub URL analyses: %w", err)
	}

	// Phase 1: Base EOL
	eolEvaluator := eolevaluator.NewEvaluator(s.packagistClient)
	if s.pypiClient != nil {
		eolEvaluator.SetPyPIClient(s.pypiClient)
	}
	if s.cfg != nil {
		if u := s.cfg.Maven.BaseURL; strings.TrimSpace(u) != "" {
			mv := maven.NewClient()
			mv.SetBaseURL(u)
			eolEvaluator.SetMavenClient(mv)
			slog.Debug("Maven base URL configured for EOL evaluator", "base_url", u)
		}
	}
	eolMap, evalErr := eolEvaluator.EvaluateBatch(ctx, analyses)
	if evalErr != nil {
		slog.Warn("base_eol_evaluate_failed", "error", evalErr)
	}
	for key, a := range analyses {
		if a == nil {
			continue
		}
		e, ok := eolMap[key]
		if !ok {
			continue
		}
		if a.Error == nil {
			a.EOL = e
			continue
		}
		if common.IsResourceNotFoundError(a.Error) && isRegistryResolvedEOL(e) {
			a.EOL = e
			slog.Info("registry_fallback_resolved",
				"url", key,
				"eol_state", string(e.State),
				"source", eolEvidenceSource(e),
			)
			a.Error = nil
		}
	}

	// Repo-URL fallback: clear not-found error when a repo URL and project data exist.
	for key, a := range analyses {
		if a == nil || a.Error == nil {
			continue
		}
		if !common.IsResourceNotFoundError(a.Error) {
			continue
		}
		if a.RepoURL != "" && a.Repository != nil {
			slog.Info("repo_url_fallback_resolved",
				"url", key,
				"repo_url", a.RepoURL,
			)
			a.Error = nil
		}
	}

	// Phase 2: Run enrichers (catalog EOL, etc.)
	for _, enrich := range s.enrichers {
		if err := enrich(ctx, analyses); err != nil {
			slog.Warn("enricher_failed", "error", err)
		}
	}

	// Phase 3: Assess
	composite := domain.NewCompositeAssessor(
		domain.NewLifecycleAssessorService(),
		domain.NewBuildHealthAssessorService(),
	)
	for key, a := range analyses {
		if a == nil || a.Error != nil {
			continue
		}
		in := domain.AssessmentInput{Analysis: a, Scores: a.Scores, EOL: a.EOL}
		axisMap, err := composite.AssessAll(ctx, in)
		if err != nil {
			slog.Debug("Failed composite assessment", "url", key, "error", err)
			continue
		}
		if len(axisMap) == 0 {
			continue
		}
		if a.AxisResults == nil {
			a.AxisResults = make(map[domain.AssessmentAxis]*domain.AssessmentResult)
		}
		for ax, r := range axisMap {
			a.AxisResults[ax] = r
		}
	}

	return analyses, nil
}

// WriteScoreCardCSV exports analysis results to CSV file
// This method encapsulates Infrastructure layer CSV writing functionality
func (s *AnalysisService) WriteScoreCardCSV(results map[string]*domain.Analysis, filename string) error {
	return exportcsv.ExportScorecard(results, filename)
}

// WriteLicenseCSV exports extended license analysis data to CSV file.
// DDD Layer: Application (delegates to Infrastructure exporter)
// Args: results - map of PURL->Analysis, filename - destination path
// Returns: error if export fails
func (s *AnalysisService) WriteLicenseCSV(results map[string]*domain.Analysis, filename string) error {
	return exportcsv.ExportLicenses(results, filename)
}

// ================= Registry Fallback Helpers =================

// isRegistryResolvedEOL returns true when the EOL evaluation produced a terminal
// state (EOL or Scheduled) from a registry-based primary source. This indicates
// the package was successfully evaluated even though deps.dev did not find it.
func isRegistryResolvedEOL(e domain.EOLStatus) bool {
	return e.State == domain.EOLEndOfLife || e.State == domain.EOLScheduled
}

// eolEvidenceSource returns the Source of the first evidence (for logging).
func eolEvidenceSource(e domain.EOLStatus) string {
	if len(e.Evidences) > 0 {
		return e.Evidences[0].Source
	}
	return "unknown"
}
