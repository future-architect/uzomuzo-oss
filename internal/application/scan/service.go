// Package scan provides the unified scan use case: resolve input → evaluate → derive verdicts → apply fail policy.
//
// DDD Layer: Application (use case orchestration)
package scan

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/future-architect/uzomuzo-oss/internal/application"
	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	domainscan "github.com/future-architect/uzomuzo-oss/internal/domain/scan"
)

// Result contains the output of a scan operation.
type Result struct {
	// Entries holds per-dependency verdict and analysis data.
	Entries []domainaudit.AuditEntry
	// HasFailure is true if any entry matches the fail policy.
	HasFailure bool
}

// Service orchestrates the unified scan pipeline.
type Service struct {
	analysisService *application.AnalysisService
}

// NewService creates a scan Service.
func NewService(analysisService *application.AnalysisService) *Service {
	return &Service{analysisService: analysisService}
}

// AnalysisService returns the underlying analysis service for callers that need
// infrastructure state (e.g. GitHub API rate limits).
func (s *Service) AnalysisService() *application.AnalysisService {
	return s.analysisService
}

// RunFromPURLs executes the scan pipeline from pre-resolved PURLs and GitHub URLs.
func (s *Service) RunFromPURLs(ctx context.Context, purls, githubURLs []string, policy domainscan.FailPolicy) (*Result, error) {
	allAnalyses := make(map[string]*analysis.Analysis)

	if len(purls) > 0 {
		slog.Info("scan: evaluating PURLs", "count", len(purls))
		res, err := s.analysisService.ProcessBatchPURLs(ctx, purls)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate PURLs: %w", err)
		}
		for k, v := range res {
			allAnalyses[k] = v
		}
	}

	if len(githubURLs) > 0 {
		slog.Info("scan: evaluating GitHub URLs", "count", len(githubURLs))
		res, err := s.analysisService.ProcessBatchGitHubURLs(ctx, githubURLs)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate GitHub URLs: %w", err)
		}
		for k, v := range res {
			allAnalyses[k] = v
		}
	}

	// Build ordered entry list: PURLs first, then GitHub URLs (preserves input order).
	keys := make([]string, 0, len(purls)+len(githubURLs))
	keys = append(keys, purls...)
	keys = append(keys, githubURLs...)

	entries := buildEntries(keys, allAnalyses)
	hasFailure := policy.Evaluate(entries)

	return &Result{Entries: entries, HasFailure: hasFailure}, nil
}

// RunFromParser executes the scan pipeline from a dependency parser (SBOM/go.mod).
func (s *Service) RunFromParser(ctx context.Context, parser depparser.DependencyParser, data []byte, policy domainscan.FailPolicy) (*Result, error) {
	if parser == nil {
		return nil, fmt.Errorf("scan service: parser is nil")
	}

	deps, err := parser.Parse(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dependencies (%s): %w", parser.FormatName(), err)
	}
	if len(deps) == 0 {
		return &Result{}, nil
	}

	// Deduplicate
	seen := make(map[string]struct{}, len(deps))
	var purls []string
	for _, d := range deps {
		if _, exists := seen[d.PURL]; exists {
			continue
		}
		seen[d.PURL] = struct{}{}
		purls = append(purls, d.PURL)
	}

	slog.Info("scan: evaluating dependencies", "count", len(purls), "parser", parser.FormatName())

	analyses, err := s.analysisService.ProcessBatchPURLs(ctx, purls)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate dependencies: %w", err)
	}

	entries := buildEntries(purls, analyses)
	hasFailure := policy.Evaluate(entries)

	return &Result{Entries: entries, HasFailure: hasFailure}, nil
}

// buildEntries creates AuditEntry slice from keys and analyses in order.
func buildEntries(keys []string, analyses map[string]*analysis.Analysis) []domainaudit.AuditEntry {
	entries := make([]domainaudit.AuditEntry, 0, len(keys))
	for _, key := range keys {
		a := analyses[key]
		v := domainaudit.DeriveVerdict(a)
		entry := domainaudit.AuditEntry{
			PURL:     key,
			Analysis: a,
			Verdict:  v,
		}
		if a != nil && a.Error != nil {
			entry.ErrorMsg = a.Error.Error()
		}
		entries = append(entries, entry)
	}
	return entries
}
