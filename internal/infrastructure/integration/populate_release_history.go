package integration

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common/links"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// enrichReleaseHistory populates Analysis.ReleaseHistory for npm analyses with
// the registry's publish time for every version, removed ones included. The
// lifecycle assessor reads it against the analysed version to detect a release
// burst after a long silence (ADR-0026). No-op when the npm client is unwired.
//
// Best-effort: a fetch failure leaves ReleaseHistory nil, which the assessor
// reads as "not asked". One request per distinct package name, for versioned PURLs only.
//
// Unlike collectPackageJobs, scoped names are kept: npm namespaces are part of
// the package name ("@solana/web3.js"), not a different package.
//
// DDD Layer: Infrastructure (best-effort parallel enrichment).
func (s *IntegrationService) enrichReleaseHistory(ctx context.Context, analyses map[string]*domain.Analysis) {
	if len(analyses) == 0 || s.npmClient == nil {
		return
	}
	jobs := map[packageJobKey]*packageJob[*domain.ReleaseHistory]{}
	parser := purl.NewParser()
	for _, a := range analyses {
		if a == nil || a.Package == nil {
			continue
		}
		parsed, err := parser.Parse(a.Package.PURL)
		// Without a version there is nothing to judge (Analysis.DormantBurst
		// reads Package.Version), so skip the multi-megabyte packument fetch.
		if err != nil || parsed.Ecosystem() != "npm" || parsed.Version() == "" {
			continue
		}
		name := strings.TrimSpace(parsed.Name())
		if name == "" {
			continue
		}
		full := links.JoinNpmName(parsed.Namespace(), name)
		key := packageJobKey{ecosystem: "npm", name: full}
		job, seen := jobs[key]
		if !seen {
			job = &packageJob[*domain.ReleaseHistory]{fetch: s.fetchNpmReleaseHistory}
			jobs[key] = job
		}
		job.targets = append(job.targets, a)
	}
	runPackageJobs(ctx, "release_history", jobs, func(a *domain.Analysis, h *domain.ReleaseHistory) {
		if h == nil {
			return
		}
		// Each analysis owns its maps: the history is returned to library
		// callers, and an edit to one version's copy must not change another's.
		cp := *h
		cp.PublishedAt = maps.Clone(h.PublishedAt)
		cp.Installable = maps.Clone(h.Installable)
		a.ReleaseHistory = &cp
	})
}

// fetchNpmReleaseHistory asks registry.npmjs.org for the package's publish times.
func (s *IntegrationService) fetchNpmReleaseHistory(ctx context.Context, fullName string) (*domain.ReleaseHistory, bool, error) {
	h, found, err := s.npmClient.GetPublishHistory(ctx, fullName)
	if err != nil {
		return nil, false, fmt.Errorf("release history: %w", err)
	}
	if !found || h == nil {
		return nil, false, nil
	}
	return &domain.ReleaseHistory{
		Registry:    domain.RegistryNpm,
		PublishedAt: h.PublishedAt,
		Installable: h.Installable,
	}, true, nil
}
