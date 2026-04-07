package application

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/integration"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/maven"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/packagist"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/rubygems"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/scorecard"
)

// FetchService provides fetch-only operations for analyses without lifecycle assessment.
//
// DDD Layer: Application (use case orchestration)
// Responsibilities: Delegates data retrieval to Infrastructure layer
// and returns domain models without computing lifecycle assessments.
type FetchService struct {
	integrationService *integration.IntegrationService
}

// NewFetchService creates a FetchService with an IntegrationService dependency.
func NewFetchService(integrationService *integration.IntegrationService) *FetchService {
	return &FetchService{integrationService: integrationService}
}

// NewFetchServiceFromConfig wires Infrastructure clients from configuration.
func NewFetchServiceFromConfig(cfg *config.Config) *FetchService {
	githubClient := github.NewClient(cfg)
	rgClient := rubygems.NewClient()
	pkgClient := packagist.NewClient()
	depsdevClient := depsdev.NewDepsDevClient(&cfg.DepsDev).
		WithRubyGems(rgClient).
		WithPackagist(pkgClient).
		WithMaven(func() *maven.Client {
			mv := maven.NewClient()
			if u := cfg.Maven.BaseURL; strings.TrimSpace(u) != "" {
				mv.SetBaseURL(u)
				slog.Debug("Maven base URL configured", "base_url", u)
			}
			return mv
		}())
	scorecardClient := scorecard.NewClient(&cfg.Scorecard)
	integrationService := integration.NewIntegrationService(githubClient, depsdevClient,
		integration.WithConfig(cfg),
		integration.WithRubyGemsClient(rgClient),
		integration.WithPackagistClient(pkgClient),
		integration.WithScorecardClient(scorecardClient),
	)
	return &FetchService{integrationService: integrationService}
}

// FetchBatchByPURLs fetches analyses for PURLs without lifecycle assessment.
//
// Args: ctx - context, purls - list of Package URLs
// Returns: map of PURL -> *domain.Analysis, error
func (s *FetchService) FetchBatchByPURLs(ctx context.Context, purls []string) (map[string]*domain.Analysis, error) {
	analyses, err := s.integrationService.AnalyzeFromPURLs(ctx, purls)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch analyses for PURLs: %w", err)
	}
	// Note: do not compute lifecycle assessments here (fetch-only contract)
	return analyses, nil
}

// FetchBatchByGitHubURLs fetches analyses for GitHub URLs without lifecycle assessment.
//
// Args: ctx - context, urls - list of GitHub repository URLs
// Returns: map of URL -> *domain.Analysis, error
func (s *FetchService) FetchBatchByGitHubURLs(ctx context.Context, urls []string) (map[string]*domain.Analysis, error) {
	analyses, err := s.integrationService.AnalyzeFromGitHubURLs(ctx, urls)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch analyses for GitHub URLs: %w", err)
	}
	// Note: do not compute lifecycle assessments here (fetch-only contract)
	return analyses, nil
}
