// Package integration provides external API integration services
package integration

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	commonlinks "github.com/future-architect/uzomuzo-oss/internal/common/links"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/clearlydefined"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/crates"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
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
	cratesClient    *crates.Client
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

// WithCratesClient injects a crates.io client used to populate
// Analysis.RegistryState for cargo packages (see ADR-0022). Optional — when
// unset, cargo analyses carry no registry-level withdrawal fact.
func WithCratesClient(c *crates.Client) IntegrationOption {
	return func(s *IntegrationService) { s.cratesClient = c }
}

// WithMavenClient injects a Maven client used by enrichLicenseFromManifest to
// fall back to pom.xml <licenses> when deps.dev and GitHub fail to yield a
// canonical SPDX license.
//
// Optional in the strict sense: when unset the manifest fallback is skipped
// and Maven licenses remain as resolved by upstream sources. In production
// this materially reduces Maven license coverage (~38% baseline per issue
// #327), so library users wiring their own IntegrationService should opt in.
// NewAnalysisServiceFromConfig wires it eagerly.
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
// IntegrationService should opt in. NewAnalysisServiceFromConfig wires it
// eagerly.
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
		Ecosystem: parsed.Ecosystem(),
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
			pkgName := parsed.PackageName()
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
