package integration

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// enrichPyPISummary overrides Repository.Summary with PyPI info.summary for analyses
// whose ecosystem is pypi. No-op when the PyPI client is unwired or no PyPI analyses
// are present. Best-effort: per-package fetch failures are logged at debug level and
// leave the existing Summary untouched.
//
// Precondition: analyses without a populated Repository (e.g., a PyPI package whose
// deps.dev Project lookup returned no repo URL) are skipped. Summary lives on
// Repository per the domain model; if we have no Repository to host it, there is
// nothing to override and we don't synthesise a Repository just for the field.
// This matches the behavior of Description, which is only written when a Repository
// already exists.
//
// DDD Layer: Infrastructure (parallel best-effort enrichment, mirroring the
// WaitGroup-only fan-out used by enrichDependentCounts/enrichDependencyCounts).
// Concurrency is bounded by the underlying httpclient transport limits and the
// pypi.Client in-memory cache; we do not impose an additional in-process cap to
// stay consistent with sibling enrichers.
//
// Ordering: must run AFTER deps.dev populate and GitHub enrichment, so PyPI's
// canonical short summary takes precedence over the repo-level fallback.
func (s *IntegrationService) enrichPyPISummary(ctx context.Context, analyses map[string]*domain.Analysis) {
	if s.pypiClient == nil || len(analyses) == 0 {
		return
	}

	parser := purl.NewParser()
	// Deduplicate by lowercased PyPI package name. Multiple analyses can share a
	// package (e.g., case-variant PURLs); a single fetch resolves them all and
	// avoids redundant cache writes.
	jobs := make(map[string][]*domain.Analysis)
	for _, a := range analyses {
		if a == nil || a.Repository == nil || a.Package == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(a.Package.Ecosystem), "pypi") {
			continue
		}
		parsed, err := parser.Parse(a.Package.PURL)
		if err != nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(parsed.GetPackageName()))
		if name == "" {
			continue
		}
		jobs[name] = append(jobs[name], a)
	}
	if len(jobs) == 0 {
		return
	}

	var wg sync.WaitGroup
	for name, targets := range jobs {
		wg.Add(1)
		go func(name string, targets []*domain.Analysis) {
			defer wg.Done()
			info, found, err := s.pypiClient.GetProject(ctx, name)
			if err != nil {
				slog.Debug("pypi_summary_fetch_failed", "name", name, "error", err)
				return
			}
			if !found || info == nil {
				return
			}
			summary := domain.NormalizeSummary(info.Summary)
			if summary == "" {
				return
			}
			for _, a := range targets {
				a.Repository.Summary = summary
			}
		}(name, targets)
	}
	wg.Wait()
}
