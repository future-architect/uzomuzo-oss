package integration

import (
	"context"
	"log/slog"
	"sort"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
)

// enrichTransitiveAdvisories fetches advisory keys for transitive dependencies and appends
// them to lifecycle-relevant versions (Stable, MaxSemver) on each Analysis.
//
// It reuses the dependency graph data already fetched by enrichDependencyCounts to avoid
// redundant API calls. For each dependency graph, it calls FetchTransitiveAdvisoryKeys to
// get advisory keys for all non-SELF nodes, then enriches the advisories with severity data.
//
// Advisory deduplication: if a transitive advisory ID already exists as a direct advisory
// on the same VersionDetail, the transitive entry is skipped (DIRECT takes precedence).
//
// DDD Layer: Infrastructure (best-effort enrichment)
func (s *IntegrationService) enrichTransitiveAdvisories(
	ctx context.Context,
	purls []string,
	analyses map[string]*domain.Analysis,
	depsGraphResults map[string]*depsdev.DependenciesResponse,
) {
	if len(depsGraphResults) == 0 {
		return
	}

	// Mark existing direct advisories with explicit Relation before adding transitive ones.
	for _, a := range analyses {
		if a == nil || a.ReleaseInfo == nil {
			continue
		}
		markDirectAdvisories(a.ReleaseInfo.StableVersion)
		markDirectAdvisories(a.ReleaseInfo.MaxSemverVersion)
	}

	// Collect transitive advisory keys for all dependency graphs.
	// Each analysis may have its own dependency graph.
	type transitiveEntry struct {
		advisory domain.Advisory
	}
	perAnalysis := make(map[string][]transitiveEntry, len(depsGraphResults))
	allAdvisoryIDs := make(map[string]struct{})

	for _, p := range purls {
		deps, ok := depsGraphResults[p]
		if !ok || deps == nil {
			continue
		}

		transitiveKeys, err := s.depsdevClient.FetchTransitiveAdvisoryKeys(ctx, deps)
		if err != nil {
			slog.Debug("transitive_advisory_fetch_failed", "purl", p, "error", err)
			continue
		}

		a := analyses[p]
		if a == nil || a.ReleaseInfo == nil {
			continue
		}

		// Build a set of existing direct advisory IDs for dedup.
		directIDs := collectDirectAdvisoryIDSet(a.ReleaseInfo.StableVersion, a.ReleaseInfo.MaxSemverVersion)

		var entries []transitiveEntry
		for depName, advisoryKeys := range transitiveKeys {
			for _, ak := range advisoryKeys {
				if _, isDirect := directIDs[ak.ID]; isDirect {
					continue // skip: already a direct advisory
				}
				srcName, url := classifyAdvisory(ak.ID)
				entries = append(entries, transitiveEntry{
					advisory: domain.Advisory{
						ID:             ak.ID,
						Source:         srcName,
						URL:            url,
						Relation:       domain.AdvisoryRelationTransitive,
						DependencyName: depName,
					},
				})
				allAdvisoryIDs[ak.ID] = struct{}{}
			}
		}
		if len(entries) > 0 {
			perAnalysis[p] = entries
		}
	}

	if len(allAdvisoryIDs) == 0 {
		return
	}

	// Fetch severity details for all transitive advisory IDs.
	ids := make([]string, 0, len(allAdvisoryIDs))
	for id := range allAdvisoryIDs {
		ids = append(ids, id)
	}
	slog.Debug("fetching_transitive_advisory_severity", "unique_ids", len(ids))
	details := s.depsdevClient.FetchAdvisoriesBatch(ctx, ids)

	// Append enriched transitive advisories to lifecycle-relevant versions.
	for p, entries := range perAnalysis {
		a := analyses[p]
		if a == nil || a.ReleaseInfo == nil {
			continue
		}
		for i := range entries {
			adv := &entries[i].advisory
			if detail, ok := details[adv.ID]; ok {
				adv.Title = detail.Title
				adv.CVSS3Score = detail.CVSS3Score
				adv.Severity = domain.SeverityFromCVSS3(detail.CVSS3Score)
			}
		}
		advisories := make([]domain.Advisory, len(entries))
		for i, e := range entries {
			advisories[i] = e.advisory
		}
		appendTransitiveAdvisories(a.ReleaseInfo.StableVersion, advisories)
		appendTransitiveAdvisories(a.ReleaseInfo.MaxSemverVersion, advisories)
	}
}

// markDirectAdvisories sets Relation to DIRECT on all existing advisories in a VersionDetail.
func markDirectAdvisories(vd *domain.VersionDetail) {
	if vd == nil {
		return
	}
	for i := range vd.Advisories {
		if vd.Advisories[i].Relation == "" {
			vd.Advisories[i].Relation = domain.AdvisoryRelationDirect
		}
	}
}

// collectDirectAdvisoryIDSet builds a set of advisory IDs from lifecycle-relevant versions.
func collectDirectAdvisoryIDSet(versions ...*domain.VersionDetail) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, vd := range versions {
		if vd == nil {
			continue
		}
		for _, a := range vd.Advisories {
			ids[a.ID] = struct{}{}
		}
	}
	return ids
}

// appendTransitiveAdvisories appends transitive advisories to a VersionDetail, deduplicating
// by advisory ID against existing entries. Maintains CVSS3 descending sort order.
func appendTransitiveAdvisories(vd *domain.VersionDetail, advisories []domain.Advisory) {
	if vd == nil || len(advisories) == 0 {
		return
	}
	existing := make(map[string]struct{}, len(vd.Advisories))
	for _, a := range vd.Advisories {
		existing[a.ID] = struct{}{}
	}
	for _, a := range advisories {
		if _, ok := existing[a.ID]; ok {
			continue
		}
		vd.Advisories = append(vd.Advisories, a)
		existing[a.ID] = struct{}{}
	}
	sort.SliceStable(vd.Advisories, func(i, j int) bool {
		return vd.Advisories[i].CVSS3Score > vd.Advisories[j].CVSS3Score
	})
}
