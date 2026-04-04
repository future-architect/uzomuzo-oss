package integration

import (
	"context"
	"log/slog"
	"sort"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
)

// enrichAdvisorySeverity fetches CVSS3 scores for advisories on lifecycle-relevant
// versions (Stable, MaxSemver) across analyses and populates Title, CVSS3Score,
// and Severity on each Advisory.
// Advisories are sorted by CVSS3 score descending (highest severity first) after enrichment.
//
// Scope: Only StableVersion and MaxSemverVersion are enriched because:
//   - The lifecycle assessor (getStableOrMaxVersionDetail) only inspects these two.
//   - CLI output (JSON/CSV) uses LatestVersionDetail(), which prefers Stable > MaxSemver
//     and usually selects one of those when present.
//   - PreRelease and RequestedVersion are not used for lifecycle classification. They may
//     still be selected as a LatestVersionDetail() fallback when Stable and MaxSemver are nil,
//     and in that case their advisories intentionally remain unenriched by design.
//   - Skipping them reduces API calls proportionally (see ADR-0010).
//
// This is best-effort: fetch failures leave Advisory fields at zero values,
// and the lifecycle assessor falls back to count-based logic for unknown-severity advisories.
func (s *IntegrationService) enrichAdvisorySeverity(ctx context.Context, analyses map[string]*domain.Analysis) {
	// Collect unique advisory IDs from lifecycle-relevant versions only.
	idSet := make(map[string]struct{})
	for _, a := range analyses {
		if a == nil || a.ReleaseInfo == nil {
			continue
		}
		collectAdvisoryIDs(a.ReleaseInfo.StableVersion, idSet)
		collectAdvisoryIDs(a.ReleaseInfo.MaxSemverVersion, idSet)
	}

	if len(idSet) == 0 {
		return
	}

	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}

	slog.Debug("fetching_advisory_severity", "unique_ids", len(ids))
	details := s.depsdevClient.FetchAdvisoriesBatch(ctx, ids)

	// Enrich lifecycle-relevant versions only.
	for _, a := range analyses {
		if a == nil || a.ReleaseInfo == nil {
			continue
		}
		enrichVersionAdvisories(a.ReleaseInfo.StableVersion, details)
		enrichVersionAdvisories(a.ReleaseInfo.MaxSemverVersion, details)
	}
}

// collectAdvisoryIDs adds advisory IDs from a VersionDetail to the set.
func collectAdvisoryIDs(vd *domain.VersionDetail, idSet map[string]struct{}) {
	if vd == nil {
		return
	}
	for _, adv := range vd.Advisories {
		if adv.ID == "" {
			continue
		}
		idSet[adv.ID] = struct{}{}
	}
}

// enrichVersionAdvisories fills severity data from fetched details and sorts by CVSS3 descending.
func enrichVersionAdvisories(vd *domain.VersionDetail, details map[string]*depsdev.AdvisoryDetail) {
	if vd == nil || len(vd.Advisories) == 0 {
		return
	}
	for i := range vd.Advisories {
		adv := &vd.Advisories[i]
		if detail, ok := details[adv.ID]; ok {
			adv.Title = detail.Title
			adv.CVSS3Score = detail.CVSS3Score
			adv.Severity = domain.SeverityFromCVSS3(detail.CVSS3Score)
		}
	}
	// Use a stable sort so advisories with equal scores keep their existing order.
	sort.SliceStable(vd.Advisories, func(i, j int) bool {
		return vd.Advisories[i].CVSS3Score > vd.Advisories[j].CVSS3Score
	})
}
