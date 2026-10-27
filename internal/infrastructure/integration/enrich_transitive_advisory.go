package integration

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

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
	// Multiple original PURLs may resolve to the same effective dependency graph.
	// Cache transitive advisory keys per graph to avoid repeating external API calls.
	perAnalysis := make(map[string][]domain.Advisory, len(depsGraphResults))
	allAdvisoryIDs := make(map[string]struct{})
	transitiveKeysByDeps := make(map[*depsdev.DependenciesResponse]map[string][]depsdev.AdvisoryKey, len(depsGraphResults))

	for _, p := range purls {
		deps, ok := depsGraphResults[p]
		if !ok || deps == nil {
			continue
		}

		transitiveKeys, cached := transitiveKeysByDeps[deps]
		if !cached {
			var err error
			transitiveKeys, err = s.depsdevClient.FetchTransitiveAdvisoryKeys(ctx, deps)
			if err != nil {
				slog.Debug("transitive_advisory_fetch_failed", "purl", p, "error", err)
				continue
			}
			transitiveKeysByDeps[deps] = transitiveKeys
		}

		a := analyses[p]
		if a == nil || a.ReleaseInfo == nil {
			continue
		}

		// Collect all transitive entries without pre-filtering by direct advisory IDs.
		// Deduplication against direct advisories is performed per-VersionDetail in
		// appendTransitiveAdvisories, ensuring each version is evaluated independently.
		// Sort dependency names for deterministic output order.
		depNames := make([]string, 0, len(transitiveKeys))
		for depName := range transitiveKeys {
			depNames = append(depNames, depName)
		}
		sort.Strings(depNames)

		var entries []domain.Advisory
		for _, depName := range depNames {
			advisoryKeys := append([]depsdev.AdvisoryKey(nil), transitiveKeys[depName]...)
			sort.Slice(advisoryKeys, func(i, j int) bool {
				return advisoryKeys[i].ID < advisoryKeys[j].ID
			})
			for _, ak := range advisoryKeys {
				srcName, url := classifyAdvisory(ak.ID)
				entries = append(entries, domain.Advisory{
					ID:             ak.ID,
					Source:         srcName,
					URL:            url,
					Relation:       domain.AdvisoryRelationTransitive,
					DependencyName: depName,
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
	published := s.fetchHighAdvisoryPublished(ctx, details)

	// Append enriched transitive advisories to lifecycle-relevant versions.
	// Only attach to versions whose dependency graph was actually fetched.
	// The graph is fetched for the PURL's version (requested or effective),
	// which may differ from StableVersion when users specify an older version.
	for p, entries := range perAnalysis {
		a := analyses[p]
		if a == nil || a.ReleaseInfo == nil {
			continue
		}
		for i := range entries {
			if detail, ok := details[entries[i].ID]; ok {
				entries[i].Title = detail.Title
				entries[i].CVSS3Score = detail.CVSS3Score
				entries[i].Severity = domain.SeverityFromCVSS3(detail.CVSS3Score)
			}
			entries[i].Published = published[entries[i].ID]
		}
		graphVersion := graphSELFVersion(depsGraphResults[p])
		for _, vd := range []*domain.VersionDetail{
			a.ReleaseInfo.StableVersion,
			a.ReleaseInfo.MaxSemverVersion,
		} {
			if vd != nil && (graphVersion == "" || vd.Version == graphVersion) {
				appendTransitiveAdvisories(vd, entries)
			}
		}
	}
}

// osvPublishedWorkers bounds concurrent /v1/vulns/{id} lookups.
const osvPublishedWorkers = 8

// fetchHighAdvisoryPublished looks up the OSV publication date of every
// dependency advisory deps.dev rates HIGH or CRITICAL. Only those can make a
// dormant package EOL-Effective through a dependency (see ADR-0032), so the
// lower ones are not fetched. A failed or missing lookup leaves the date zero,
// which never qualifies. A withdrawn record is also left zero.
func (s *IntegrationService) fetchHighAdvisoryPublished(ctx context.Context, details map[string]*depsdev.AdvisoryDetail) map[string]time.Time {
	if s.osvClient == nil || len(details) == 0 {
		return nil
	}
	var ids []string
	for id, d := range details {
		if d == nil {
			continue
		}
		if sev := domain.SeverityFromCVSS3(d.CVSS3Score); sev == "HIGH" || sev == "CRITICAL" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make(map[string]time.Time, len(ids))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, osvPublishedWorkers)
	failed := 0
	for _, id := range ids {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return out
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			rec, found, err := s.osvClient.GetVuln(ctx, id)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				slog.Debug("osv_vuln_lookup_failed", "id", id, "error", err)
				return
			}
			if found && !rec.Withdrawn && !rec.Published.IsZero() {
				out[id] = rec.Published
			}
		}(id)
	}
	wg.Wait()
	slog.Debug("osv_dependency_advisory_published", "high_ids", len(ids), "dated", len(out), "failed", failed)
	return out
}

// graphSELFVersion extracts the version of the root (SELF) node from a dependency graph.
// Returns "" if the graph is nil or has no SELF node.
func graphSELFVersion(deps *depsdev.DependenciesResponse) string {
	if deps == nil {
		return ""
	}
	for _, n := range deps.Nodes {
		if n.Relation == "SELF" {
			return n.VersionKey.Version
		}
	}
	return ""
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
		if vd.Advisories[i].CVSS3Score != vd.Advisories[j].CVSS3Score {
			return vd.Advisories[i].CVSS3Score > vd.Advisories[j].CVSS3Score
		}
		return vd.Advisories[i].ID < vd.Advisories[j].ID
	})
}
