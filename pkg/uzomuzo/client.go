// Package uzomuzo provides a simple API for security analysis of software packages
// using OpenSSF Scorecard metrics and PURL (Package URL) specifications.
package uzomuzo

import (
	"context"
	"os"

	"github.com/future-architect/uzomuzo-oss/internal/application"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// Option configures an Evaluator.
type Option = application.Option

// AnalysisEnricher is called between Phase 1 (registry EOL) and Phase 3
// (lifecycle/build-health assessment). It may mutate Analysis.EOL and
// Analysis.Error fields. Use this to inject custom EOL catalog evaluation.
type AnalysisEnricher = application.AnalysisEnricher

// WithEnricher appends an enricher to the pipeline between Phase 1 and Phase 3.
//
// Example:
//
//	evaluator := uzomuzo.NewEvaluator(token, uzomuzo.WithEnricher(myCatalogEnricher))
func WithEnricher(e AnalysisEnricher) Option {
	return application.WithEnricher(e)
}

// Evaluator performs full security & lifecycle evaluation including
// primary-source EOL (registry heuristics / deprecation) integration.
// DDD Layer exposure: public facade over application.AnalysisService.
type Evaluator struct {
	service *application.AnalysisService
}

// NewEvaluator creates a new Evaluator with the specified GitHub token.
// If githubToken is empty, it falls back to the GITHUB_TOKEN environment variable.
//
// Example:
//
//	client := uzomuzo.NewEvaluator(os.Getenv("GITHUB_TOKEN"))
//	client := uzomuzo.NewEvaluator(token, uzomuzo.WithEnricher(myCatalogEnricher))
func NewEvaluator(githubToken string, opts ...Option) *Evaluator {
	if githubToken == "" {
		githubToken = os.Getenv("GITHUB_TOKEN")
	}

	// Create config with defaults and override GitHub token
	cfg := config.NewConfigWithDefaults()
	cfg.GitHub.Token = githubToken

	service := application.NewAnalysisServiceFromConfig(cfg, opts...)
	return &Evaluator{service: service}
}

// NewEvaluatorFromService creates an Evaluator with a pre-configured AnalysisService.
// This allows callers (e.g., a private catalog overlay) to inject custom enrichers
// or other configuration into the service before passing it here.
func NewEvaluatorFromService(service *application.AnalysisService) *Evaluator {
	return &Evaluator{service: service}
}

// EvaluatePURLs performs full evaluation for multiple PURLs.
// Each PURL should follow the Package URL specification (https://github.com/package-url/purl-spec).
// Supported ecosystems: npm, pypi, maven, cargo, golang, gem, nuget.
// EvaluatePURLs performs full evaluation (collection + lifecycle heuristics + EOL) for PURLs.
func (e *Evaluator) EvaluatePURLs(ctx context.Context, purls []string) (map[string]*domain.Analysis, error) {
	return e.service.ProcessBatchPURLs(ctx, purls)
}

// EvaluateGitHubRepos performs full evaluation for multiple GitHub repositories.
// Accepts URL forms like: https://github.com/owner/repo or github.com/owner/repo.
// EvaluateGitHubRepos performs full evaluation for GitHub repository URLs.
func (e *Evaluator) EvaluateGitHubRepos(ctx context.Context, urls []string) (map[string]*domain.Analysis, error) {
	return e.service.ProcessBatchGitHubURLs(ctx, urls)
}

// ExportCSV exports analysis results to a CSV file with comprehensive security metrics.
// The CSV file includes scorecard scores, security classifications, and repository metadata.
//
// Example:
//
//	err := client.ExportCSV(results, "security_report.csv")
//
// ExportCSV writes results to a CSV file with comprehensive metrics.
func (e *Evaluator) ExportCSV(results map[string]*domain.Analysis, filename string) error {
	return e.service.WriteScoreCardCSV(results, filename)
}
