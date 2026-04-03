// Package scan provides the unified scan use case: resolve input → evaluate → derive verdicts → apply fail policy.
//
// DDD Layer: Application (use case orchestration)
package scan

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

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

// NewService creates a scan Service. analysisService must not be nil.
func NewService(analysisService *application.AnalysisService) (*Service, error) {
	if analysisService == nil {
		return nil, fmt.Errorf("scan.NewService: analysisService must not be nil")
	}
	return &Service{analysisService: analysisService}, nil
}

// AnalysisService returns the underlying analysis service for callers that need
// infrastructure state (e.g. GitHub API rate limits).
func (s *Service) AnalysisService() *application.AnalysisService {
	return s.analysisService
}

// RunFromPURLs executes the scan pipeline from pre-resolved PURLs and GitHub URLs.
func (s *Service) RunFromPURLs(ctx context.Context, purls, githubURLs []string, policy domainscan.FailPolicy) (*Result, error) {
	// Deduplicate inputs while preserving first-seen order.
	purls = dedup(purls)
	githubURLs = dedup(githubURLs)

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

	// Build ordered entry list: PURLs first, then GitHub URLs.
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

// ActionsDiscoverer abstracts the actions discovery infrastructure.
type ActionsDiscoverer interface {
	// DiscoverActions returns direct and transitive action URLs discovered from repository workflows.
	// Direct URLs are Actions referenced in workflow files; transitive actions (map of URL → via parent URL)
	// are discovered by recursively resolving composite action dependencies when resolveTransitive is true.
	DiscoverActions(ctx context.Context, repoURLs []string, resolveTransitive bool) (directURLs []string, transitiveActions map[string]string, errors map[string]error, err error)
}

// ActionsConfig configures optional GitHub Actions health scanning.
type ActionsConfig struct {
	// Enabled activates action scanning for GitHub URL inputs.
	Enabled bool
	// Discoverer performs the actual Actions discovery via GitHub API.
	Discoverer ActionsDiscoverer
	// ShowTransitive includes transitive composite action dependencies in the scan results.
	// When false (default), only direct action references from workflow files are included.
	ShowTransitive bool
}

// RunFromPURLsWithActions extends RunFromPURLs with optional GitHub Actions discovery.
// When actionsCfg.Enabled is true and githubURLs is non-empty, it discovers Actions
// referenced in the repositories' workflows and evaluates them alongside the main results.
func (s *Service) RunFromPURLsWithActions(ctx context.Context, purls, githubURLs []string, policy domainscan.FailPolicy, actionsCfg ActionsConfig) (*Result, error) {
	// Phase A: standard analysis (same as RunFromPURLs).
	purls = dedup(purls)
	githubURLs = dedup(githubURLs)

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

	// Build ordered entry list: PURLs first, then GitHub URLs.
	keys := make([]string, 0, len(purls)+len(githubURLs))
	keys = append(keys, purls...)
	keys = append(keys, githubURLs...)
	entries := buildEntries(keys, allAnalyses)

	// Phase B: actions discovery + analysis (if enabled).
	if actionsCfg.Enabled && actionsCfg.Discoverer == nil {
		return nil, fmt.Errorf("actions discovery is enabled but discoverer is nil")
	}
	if actionsCfg.Enabled && len(githubURLs) > 0 {
		directActionURLs, transitiveActions, discoveryErrors, err := actionsCfg.Discoverer.DiscoverActions(ctx, githubURLs, actionsCfg.ShowTransitive)
		if err != nil {
			return nil, fmt.Errorf("actions discovery failed: %w", err)
		}

		for src, e := range discoveryErrors {
			slog.Warn("actions discovery error", "source", src, "error", e)
		}

		// Evaluate direct action URLs.
		directEntries, err := s.evaluateActionURLs(ctx, directActionURLs, allAnalyses, domainaudit.SourceActions)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate direct actions: %w", err)
		}
		entries = append(entries, directEntries...)

		// Evaluate transitive action URLs (only when --show-transitive is set).
		if actionsCfg.ShowTransitive && len(transitiveActions) > 0 {
			// Extract URLs from the map, sorted for deterministic output.
			transitiveURLs := make([]string, 0, len(transitiveActions))
			for u := range transitiveActions {
				transitiveURLs = append(transitiveURLs, u)
			}
			sort.Strings(transitiveURLs)

			transitiveEntries, err := s.evaluateActionURLs(ctx, transitiveURLs, allAnalyses, domainaudit.SourceActionsTransitive)
			if err != nil {
				return nil, fmt.Errorf("failed to evaluate transitive actions: %w", err)
			}
			// Populate Via on each transitive entry.
			for i := range transitiveEntries {
				transitiveEntries[i].Via = transitiveActions[transitiveEntries[i].PURL]
			}
			entries = append(entries, transitiveEntries...)
		}
	}

	hasFailure := policy.Evaluate(entries)
	return &Result{Entries: entries, HasFailure: hasFailure}, nil
}

// evaluateActionURLs filters, evaluates, and tags action URLs with the given source.
// URLs already present in existingAnalyses are skipped. Newly evaluated analyses are
// added to existingAnalyses to prevent double-evaluation across direct/transitive sets.
func (s *Service) evaluateActionURLs(ctx context.Context, urls []string, existingAnalyses map[string]*analysis.Analysis, source domainaudit.EntrySource) ([]domainaudit.AuditEntry, error) {
	var newURLs []string
	for _, u := range urls {
		if _, exists := existingAnalyses[u]; !exists {
			newURLs = append(newURLs, u)
		}
	}
	newURLs = dedup(newURLs)

	if len(newURLs) == 0 {
		return nil, nil
	}

	slog.Info("scan: evaluating discovered Actions", "count", len(newURLs), "source", string(source))
	actRes, err := s.analysisService.ProcessBatchGitHubURLs(ctx, newURLs)
	if err != nil {
		return nil, fmt.Errorf("evaluate discovered Actions (%s): %w", source, err)
	}

	for k, v := range actRes {
		existingAnalyses[k] = v
	}

	actionEntries := buildEntries(newURLs, actRes)
	for i := range actionEntries {
		actionEntries[i].Source = source
	}
	return actionEntries, nil
}

// dedup removes duplicate strings while preserving first-seen order.
func dedup(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))
	result := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, exists := seen[s]; exists {
			continue
		}
		seen[s] = struct{}{}
		result = append(result, s)
	}
	return result
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
