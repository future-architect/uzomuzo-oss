package application

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/clearlydefined"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/crates"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/eolevaluator"
	exportcsv "github.com/future-architect/uzomuzo-oss/internal/infrastructure/export/csv"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/integration"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/maven"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/npmjs"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/nuget"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/osv"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/packagist"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/pypi"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/rdap"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/rubygems"
	// TODO: rename directory successor -> eolevaluator; after physical move adjust import
)

// AnalysisSource is satisfied by *integration.IntegrationService and exists for
// testability. It exposes only the two batch-fetch methods needed by
// AnalysisService; GitHubClient() is deliberately NOT part of this interface —
// putting it there would bake the concrete *github.Client infrastructure type
// into the exported application contract and force every fake to import it.
// ARCHITECT DECISION: keep GitHubClient() outside this interface.
//
// Contract: each returned Analysis must carry the caller's own coordinate in
// OriginalPURL — see ADR-0021. enrichAndAssess repairs an empty value from the
// map key and logs a warning.
type AnalysisSource interface {
	// AnalyzeFromPURLs fetches analysis data for a batch of PURLs.
	AnalyzeFromPURLs(ctx context.Context, purls []string) (map[string]*domain.Analysis, error)
	// AnalyzeFromGitHubURLs fetches analysis data for a batch of GitHub repository URLs.
	AnalyzeFromGitHubURLs(ctx context.Context, urls []string) (map[string]*domain.Analysis, error)
}

// eolBatchEvaluator is the unexported seam for EOL batch evaluation. It is
// satisfied by *eolevaluator.Evaluator and allows tests to inject a fake
// without importing the concrete evaluator type.
type eolBatchEvaluator interface {
	EvaluateBatch(ctx context.Context, analyses map[string]*domain.Analysis) (map[string]domain.EOLStatus, error)
}

// AnalysisEnricher is called between Phase 1 (registry EOL) and Phase 3
// (lifecycle/build-health assessment). It may mutate Analysis.EOL and
// Analysis.Error fields. It MUST NOT modify other aggregate fields.
//
// DDD Layer: Application (contract definition)
// Implementations: Infrastructure layer (e.g., catalog enricher in private repo)
type AnalysisEnricher func(ctx context.Context, analyses map[string]*domain.Analysis) error

// Option configures an AnalysisService.
type Option func(*AnalysisService)

type maintainerDomainChecker interface {
	Check(context.Context, domain.MaintainerAddress, time.Time) domain.MaintainerDomainFinding
}

// rdapBootstrapRefresher is implemented by a checker that can refresh its RDAP
// service list before the first check.
type rdapBootstrapRefresher interface {
	RefreshBootstrap(context.Context) domain.RDAPBootstrapInfo
}

// WithMaintainerDomain enables the optional npm maintainer domain check. The
// RDAP service list is refreshed from IANA once per service and cached under
// the user cache directory. See ADR-0030.
func WithMaintainerDomain() Option {
	return func(s *AnalysisService) {
		client := rdap.NewClient()
		client.SetBootstrapCache(rdap.DefaultBootstrapCachePath())
		s.maintainerDomainChecker = client
	}
}

// RDAPBootstrap reports the RDAP service list the maintainer domain check
// used. It is nil when the check is off, when it found no npm package to
// check, or when its checker cannot refresh the list.
func (s *AnalysisService) RDAPBootstrap() *domain.RDAPBootstrapInfo {
	s.rdapBootstrapMu.Lock()
	defer s.rdapBootstrapMu.Unlock()
	return s.rdapBootstrap
}

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
	integrationService      AnalysisSource
	cfg                     *config.Config
	enrichers               []AnalysisEnricher
	maintainerDomainChecker maintainerDomainChecker
	rdapBootstrapMu         sync.Mutex
	rdapBootstrap           *domain.RDAPBootstrapInfo
	// packagistClient is stored here (unlike other infra clients) because
	// ProcessBatch* methods pass it to eolevaluator.NewEvaluator to share
	// the same instance (and its 5-min TTL cache) across calls.
	packagistClient *packagist.Client
	// pypiClient is shared across IntegrationService (Summary enrichment) and
	// the per-batch eolevaluator instance so both consumers reuse the same
	// 10-min in-memory cache and avoid duplicate PyPI fetches per package.
	pypiClient *pypi.Client
	// newEOLEvaluator is the factory for per-call EOL evaluator instances.
	// Defaulted in both constructors to exactly today's per-call construction
	// (eolevaluator.NewEvaluator + SetPyPIClient + Maven base URL mirroring) so
	// per-call cache semantics are preserved. Tests may inject a fake.
	newEOLEvaluator func() eolBatchEvaluator
}

// GitHubClient returns the underlying GitHub client for rate limit inspection
// and composition-root wiring (cmd/ uses it to pass the client to the actions
// discovery service). The accessor uses an unexported capability assertion so
// that the AnalysisSource interface does not need to embed the concrete
// *github.Client type — test fakes are never required to implement it.
func (s *AnalysisService) GitHubClient() *github.Client {
	if p, ok := s.integrationService.(interface{ GitHubClient() *github.Client }); ok {
		return p.GitHubClient()
	}
	return nil
}

// GitHubRateLimitSummary returns the GitHub API remaining quota and reset
// timestamp by delegating to the underlying GitHub client. It returns (0, "")
// when no API calls have been made or when the integration service does not
// expose a GitHub client (e.g., in tests using a fake AnalysisSource).
func (s *AnalysisService) GitHubRateLimitSummary() (remaining int, resetAt string) {
	if c := s.GitHubClient(); c != nil {
		return c.RateLimitSummary()
	}
	return 0, ""
}

// NewAnalysisService creates a new AnalysisService that orchestrates
// analysis operations using the provided AnalysisSource.
// It does not perform any external I/O at construction time.
func NewAnalysisService(src AnalysisSource, opts ...Option) *AnalysisService {
	s := &AnalysisService{
		integrationService: src,
	}
	for _, o := range opts {
		o(s)
	}
	// Default EOL evaluator factory: per-call construction matches legacy behavior.
	// packagistClient and pypiClient are nil for callers that use this constructor
	// without clients, so the evaluator simply has no Packagist/PyPI support.
	s.newEOLEvaluator = func() eolBatchEvaluator {
		ev := eolevaluator.NewEvaluator(s.packagistClient)
		if s.pypiClient != nil {
			ev.SetPyPIClient(s.pypiClient)
		}
		if s.cfg != nil {
			if u := s.cfg.Maven.BaseURL; strings.TrimSpace(u) != "" {
				mv := maven.NewClient()
				mv.SetBaseURL(u)
				ev.SetMavenClient(mv)
				slog.Debug("Maven base URL configured for EOL evaluator", "base_url", u)
			}
		}
		return ev
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
	crClient := crates.NewClient()
	osvClient := osv.NewClient()
	cdClient := clearlydefined.NewClient()
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
		integration.WithCratesClient(crClient),
		integration.WithOSVClient(osvClient),
		integration.WithNpmClient(npmjs.NewPackumentClient()),
		integration.WithMavenClient(mvClient),
		integration.WithClearlyDefinedClient(cdClient),
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
	// Default EOL evaluator factory: exactly today's per-call construction so
	// per-call cache semantics are preserved byte-for-byte.
	s.newEOLEvaluator = func() eolBatchEvaluator {
		ev := eolevaluator.NewEvaluator(s.packagistClient)
		if s.pypiClient != nil {
			// Reuse the integration-phase PyPI client so the cache populated by
			// enrichPyPISummary is reused here, eliminating duplicate fetches per package.
			ev.SetPyPIClient(s.pypiClient)
		}
		if s.cfg != nil { // mirror alignment
			if u := s.cfg.Maven.BaseURL; strings.TrimSpace(u) != "" {
				mv := maven.NewClient()
				mv.SetBaseURL(u)
				ev.SetMavenClient(mv)
				slog.Debug("Maven base URL configured for EOL evaluator", "base_url", u)
			}
		}
		return ev
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

	if err := s.enrichAndAssess(ctx, analyses, "purl"); err != nil {
		return nil, err
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

	if err := s.enrichAndAssess(ctx, analyses, "url"); err != nil {
		return nil, err
	}
	return analyses, nil
}

// enrichAndAssess runs the shared Phase 1-3 pipeline on the given analyses map.
// refLogKey is the slog field name used for per-item telemetry ("purl" or "url").
//
// Phase 1: EOL evaluation + registry-fallback and repo-URL-fallback error clearing.
// Phase 2: AnalysisEnricher hooks (e.g., catalog EOL override).
// Phase 3: Composite lifecycle/build-health assessment.
func (s *AnalysisService) enrichAndAssess(ctx context.Context, analyses map[string]*domain.Analysis, refLogKey string) error {
	// Phase 0: repair the AnalysisSource contract. The map key is what the caller
	// requested, so it is their coordinate by construction. Deliberately not a
	// fallback inside applyRegistryYanked — a fallback there is what this PR's
	// ADR-0021 removed. The built-in IntegrationService always populates the
	// field, so a warning here means an AnalysisSource broke its contract.
	for key, analysis := range analyses {
		if analysis != nil && analysis.OriginalPURL == "" {
			slog.Warn("analysis_source_missing_original_purl", refLogKey, key)
			analysis.OriginalPURL = key
		}
	}

	// Phase 1: Evaluate base EOL from primary (non-catalog) deterministic sources.
	// newEOLEvaluator constructs a fresh per-call instance so its internal caches
	// are not shared across concurrent batch calls.
	eolEval := s.newEOLEvaluator()
	eolMap, evalErr := eolEval.EvaluateBatch(ctx, analyses)
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
			slog.Debug("registry_fallback_resolved",
				refLogKey, key,
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
			slog.Debug("repo_url_fallback_resolved",
				refLogKey, key,
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
	// One clock for the lifecycle rules, the dormant-release marker, the
	// young-release marker and the maintainer domain check. The dormant windows
	// are the same default thresholds the assessor reads, so the lifecycle rules
	// and the dormant-release marker agree on whether a release is still fresh;
	// the young-release window comes from configuration (ADR-0031).
	now := time.Now()
	s.checkMaintainerDomains(ctx, analyses, now)
	lifecycleConfig := config.GetDefaultLifecycle()
	if s.cfg != nil {
		lifecycleConfig.YoungReleaseDays = s.cfg.Lifecycle.YoungReleaseDays
	}
	config.NormalizeLifecycleConfig(&lifecycleConfig)
	silence, flag := lifecycleConfig.DormantWindows()
	for key, analysis := range analyses {
		if analysis == nil {
			continue
		}
		// Evaluated before the error check: the release history comes from the
		// registry, not deps.dev, so it can be present when deps.dev failed.
		analysis.DormantRelease = analysis.EvaluateDormantRelease(now, silence, flag)
		analysis.ReleaseAge = analysis.EvaluateReleaseAge(now, lifecycleConfig.YoungReleaseWindow())
		if analysis.Error != nil {
			continue
		}
		in := domain.AssessmentInput{Analysis: analysis, Scores: analysis.Scores, EOL: analysis.EOL, Now: now}
		axisMap, err := composite.AssessAll(ctx, in)
		if err != nil {
			slog.Debug("composite_assessment_failed", refLogKey, key, "error", err)
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

	return nil
}

// WriteScoreCardCSV exports analysis results to CSV file
// This method encapsulates Infrastructure layer CSV writing functionality
func (s *AnalysisService) WriteScoreCardCSV(results map[string]*domain.Analysis, filename string) error {
	return exportcsv.ExportScorecard(results, filename)
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

// checkMaintainerDomains fills Analysis.MaintainerDomain for npm analyses with
// a release history when the check is enabled. Packages are visited in key
// order so a cancelled run stops at a predictable point. See ADR-0030.
func (s *AnalysisService) checkMaintainerDomains(ctx context.Context, analyses map[string]*domain.Analysis, now time.Time) {
	if s.maintainerDomainChecker == nil {
		return
	}
	// Publisher evidence draws on every npm packument fetched in this batch; a
	// scan analyses PURL and GitHub URL inputs in separate batches.
	var histories []domain.PublisherPackageHistory
	for _, a := range analyses {
		if a != nil && a.ReleaseHistory != nil && a.ReleaseHistory.Registry == domain.RegistryNpm {
			histories = append(histories, a.ReleaseHistory.PublisherHistory)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(analyses)) {
		if ctx.Err() != nil {
			return
		}
		a := analyses[key]
		if a == nil || a.ReleaseHistory == nil || a.ReleaseHistory.Registry != domain.RegistryNpm {
			continue
		}
		s.refreshRDAPBootstrap(ctx)
		result := &domain.MaintainerDomainResult{}
		for _, address := range a.ReleaseHistory.Maintainers {
			finding := s.maintainerDomainChecker.Check(ctx, address, now)
			if finding.Kind.WarrantsNotice() {
				finding.PublisherEmailEvidence = domain.PublisherEmailEvidenceFor(finding, domain.PublisherScopeScanPackuments, a.ReleaseHistory.PublisherHistory, histories)
			}
			result.Findings = append(result.Findings, finding)
			// A package interrupted by cancellation would report the cut-short
			// lookups as RDAP failures, so it is left without a result.
			if ctx.Err() != nil {
				return
			}
		}
		a.MaintainerDomain = result
	}
}

// refreshRDAPBootstrap refreshes the checker's RDAP service list once, before
// the first package is checked, so a scan without npm packages fetches nothing.
func (s *AnalysisService) refreshRDAPBootstrap(ctx context.Context) {
	refresher, ok := s.maintainerDomainChecker.(rdapBootstrapRefresher)
	if !ok {
		return
	}
	s.rdapBootstrapMu.Lock()
	defer s.rdapBootstrapMu.Unlock()
	if s.rdapBootstrap != nil {
		return
	}
	info := refresher.RefreshBootstrap(ctx)
	s.rdapBootstrap = &info
}
