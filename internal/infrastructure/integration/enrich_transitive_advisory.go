package integration

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
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
// A missing deps.dev detail removes an advisory only when OSV confirms its
// withdrawal. See ADR-0036.
// DDD Layer: Infrastructure (best-effort enrichment)
func (s *IntegrationService) enrichTransitiveAdvisories(
	ctx context.Context,
	purls []string,
	analyses map[string]*domain.Analysis,
	depsGraphResults map[string]*depsdev.DependenciesResponse,
	confirmedDirect map[string]struct{},
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
				if _, removed := confirmedDirect[ak.ID]; removed {
					continue
				}
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
	details, missing := s.depsdevClient.FetchAdvisoriesBatchWithStatus(ctx, ids)
	threshold := assessorHighSeverityThreshold()
	lookupIDs := make(map[string]struct{}, len(missing))
	for id := range missing {
		lookupIDs[id] = struct{}{}
	}
	for id, detail := range details {
		if detail != nil && domain.IsKnownHighCVSS3(detail.CVSS3Score, threshold) {
			lookupIDs[id] = struct{}{}
		}
	}
	osvFacts := s.fetchAdvisoryOSVFacts(ctx, sortedIDs(lookupIDs))

	// Append enriched transitive advisories to lifecycle-relevant versions.
	// Only attach to versions whose dependency graph was actually fetched.
	// The graph is fetched for the PURL's version (requested or effective),
	// which may differ from StableVersion when users specify an older version.
	for p, entries := range perAnalysis {
		a := analyses[p]
		if a == nil || a.ReleaseInfo == nil {
			continue
		}
		kept := entries[:0]
		for i := range entries {
			if _, isMissing := missing[entries[i].ID]; isMissing && osvFacts[entries[i].ID].lookup == domain.AdvisoryRecordWithdrawn {
				slog.Debug("withdrawn_advisory_removed", "id", entries[i].ID, "relation", entries[i].Relation)
				continue
			}
			if detail, ok := details[entries[i].ID]; ok {
				entries[i].Title = detail.Title
				entries[i].CVSS3Score = detail.CVSS3Score
				entries[i].Severity = domain.SeverityFromCVSS3(detail.CVSS3Score)
				if f, ok := osvFacts[entries[i].ID]; ok && domain.IsKnownHighCVSS3(detail.CVSS3Score, threshold) {
					entries[i].RecordLookup = f.lookup
					entries[i].Published = f.rec.Published
					entries[i].ReviewedSeverity = f.rec.Severity
				}
			}
			kept = append(kept, entries[i])
		}
		graphVersion := depsGraphResults[p].SelfVersion()
		for _, vd := range []*domain.VersionDetail{
			a.ReleaseInfo.StableVersion,
			a.ReleaseInfo.MaxSemverVersion,
		} {
			if vd != nil && (graphVersion == "" || vd.Version == graphVersion) {
				appendTransitiveAdvisories(vd, kept)
			}
		}
	}
}

// osvFactWorkers bounds concurrent /v1/vulns/{id} lookups.
const osvFactWorkers = 8

// osvFactsBudget bounds all /v1/vulns/{id} lookups of one enrichment. Each
// lookup retries on its own, so with OSV down a scan would otherwise grow by
// minutes; IDs not answered in time are recorded as failed lookups.
const osvFactsBudget = 30 * time.Second

// osvFact is what the OSV lookup of one advisory ID found.
type osvFact struct {
	rec    domain.AdvisoryRecord
	lookup domain.AdvisoryRecordLookup
}

// fetchDependencyAdvisoryOSVFacts selects known-HIGH dependency IDs for OSV
// publication and severity facts. See ADR-0032 and ADR-0036.
func (s *IntegrationService) fetchDependencyAdvisoryOSVFacts(ctx context.Context, details map[string]*depsdev.AdvisoryDetail) map[string]osvFact {
	if s.osvClient == nil || len(details) == 0 {
		return nil
	}
	threshold := assessorHighSeverityThreshold()
	var ids []string
	for id, d := range details {
		if d != nil && domain.IsKnownHighCVSS3(d.CVSS3Score, threshold) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return s.fetchAdvisoryOSVFacts(ctx, ids)
}

func sortedIDs(ids map[string]struct{}) []string {
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// fetchAdvisoryOSVFacts uses the bounded OSV by-ID worker for selected IDs.
// Callers must require a deps.dev 404 before treating withdrawal as removal.
// See ADR-0036.
func (s *IntegrationService) fetchAdvisoryOSVFacts(ctx context.Context, ids []string) map[string]osvFact {
	if s.osvClient == nil || len(ids) == 0 {
		return nil
	}
	unique := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		unique[id] = struct{}{}
	}
	ids = sortedIDs(unique)
	budget := osvFactsBudget
	if s.osvFactsBudget > 0 {
		budget = s.osvFactsBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	out := make(map[string]osvFact, len(ids))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, osvFactWorkers)
	failRest := func(rest []string) {
		mu.Lock()
		defer mu.Unlock()
		for _, id := range rest {
			out[id] = osvFact{lookup: domain.AdvisoryRecordLookupFailed}
		}
	}
dispatch:
	for i, id := range ids {
		// Checked first: when a slot is also free, select picks at random.
		if ctx.Err() != nil {
			failRest(ids[i:])
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			failRest(ids[i:])
			break dispatch
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			rec, found, err := s.osvClient.GetVuln(ctx, id)
			f := osvFact{rec: rec, lookup: domain.AdvisoryRecordFound}
			switch {
			case err != nil:
				slog.Debug("osv_vuln_lookup_failed", "id", id, "error", err)
				f = osvFact{lookup: domain.AdvisoryRecordLookupFailed}
			case !found:
				f.lookup = domain.AdvisoryRecordNotFound
			case rec.Withdrawn:
				f.lookup = domain.AdvisoryRecordWithdrawn
			}
			mu.Lock()
			out[id] = f
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	failed := 0
	for _, f := range out {
		if f.lookup == domain.AdvisoryRecordLookupFailed {
			failed++
		}
	}
	slog.Debug("osv_dependency_advisory_facts", "ids", len(ids), "failed", failed)
	return out
}

// assessorHighSeverityThreshold is the HIGH threshold the lifecycle assessor
// applies. The assessor reads the default lifecycle config, not the loaded
// one, so this does too: a lookup threshold that differed would leave an
// advisory the assessor counts without a publication date.
func assessorHighSeverityThreshold() float64 {
	c := config.GetDefaultLifecycle()
	config.NormalizeLifecycleConfig(&c)
	return c.HighSeverityCVSSThreshold
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
