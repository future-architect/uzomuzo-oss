// Package audit provides the audit use case: parse dependencies → evaluate → derive verdicts.
//
// DDD Layer: Application (use case orchestration)
package audit

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/future-architect/uzomuzo-oss/internal/application"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
)

// Service orchestrates the audit workflow: parse → evaluate → verdict.
type Service struct {
	analysisService *application.AnalysisService
}

// NewService creates an audit Service.
func NewService(analysisService *application.AnalysisService) *Service {
	return &Service{analysisService: analysisService}
}

// Run executes the full audit pipeline.
//  1. Parse input data using the provided parser
//  2. Extract PURLs and deduplicate
//  3. Evaluate via ProcessBatchPURLs
//  4. Derive verdicts for each dependency
//
// Returns audit entries and a boolean indicating whether any verdict is "replace".
func (s *Service) Run(ctx context.Context, parser depparser.DependencyParser, data []byte) ([]domainaudit.AuditEntry, bool, error) {
	if parser == nil {
		return nil, false, fmt.Errorf("audit service not initialized: parser is nil")
	}

	deps, err := parser.Parse(ctx, data)
	if err != nil {
		return nil, false, fmt.Errorf("failed to parse dependencies (%s): %w", parser.FormatName(), err)
	}
	if len(deps) == 0 {
		return nil, false, nil
	}

	if s.analysisService == nil {
		return nil, false, fmt.Errorf("audit service not initialized: analysisService is nil")
	}

	// Deduplicate and collect PURLs
	seen := make(map[string]struct{}, len(deps))
	var uniqueDeps []depparser.ParsedDependency
	var purls []string
	for _, d := range deps {
		if _, exists := seen[d.PURL]; exists {
			continue
		}
		seen[d.PURL] = struct{}{}
		uniqueDeps = append(uniqueDeps, d)
		purls = append(purls, d.PURL)
	}

	slog.Info("audit: evaluating dependencies", "count", len(purls), "parser", parser.FormatName())

	analyses, err := s.analysisService.ProcessBatchPURLs(ctx, purls)
	if err != nil {
		return nil, false, fmt.Errorf("failed to evaluate dependencies: %w", err)
	}

	// Derive verdicts
	hasReplace := false
	entries := make([]domainaudit.AuditEntry, 0, len(uniqueDeps))
	for _, d := range uniqueDeps {
		a := analyses[d.PURL]
		v := domainaudit.DeriveVerdict(a)

		entry := domainaudit.AuditEntry{
			PURL:     d.PURL,
			Analysis: a,
			Verdict:  v,
		}
		if a != nil && a.Error != nil {
			entry.ErrorMsg = a.Error.Error()
		}
		if v == domainaudit.VerdictReplace {
			hasReplace = true
		}
		entries = append(entries, entry)
	}

	return entries, hasReplace, nil
}
