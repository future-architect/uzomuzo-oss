package integration

import (
	"context"
	"log/slog"
	"sort"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
)

// enrichAdvisorySeverity enriches Stable and MaxSemver severity. When
// verifyWithdrawal is true, it also removes advisories whose deps.dev detail is
// missing and whose OSV record confirms withdrawal, including on the selected
// fallback version. See ADR-0010 and ADR-0036.
func (s *IntegrationService) enrichAdvisorySeverity(ctx context.Context, analyses map[string]*domain.Analysis, verifyWithdrawal bool) map[string]struct{} {
	idSet := make(map[string]struct{})
	for _, a := range analyses {
		if a == nil || a.ReleaseInfo == nil {
			continue
		}
		collectAdvisoryIDs(a.ReleaseInfo.StableVersion, idSet)
		collectAdvisoryIDs(a.ReleaseInfo.MaxSemverVersion, idSet)
		if verifyWithdrawal && a.ReleaseInfo.StableVersion == nil && a.ReleaseInfo.MaxSemverVersion == nil {
			collectAdvisoryIDs(a.ReleaseInfo.LatestVersionDetail(), idSet)
		}
	}

	if len(idSet) == 0 {
		return nil
	}

	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}

	slog.Debug("fetching_advisory_severity", "unique_ids", len(ids))
	details, missing := s.depsdevClient.FetchAdvisoriesBatchWithStatus(ctx, ids)
	var withdrawn map[string]struct{}
	if verifyWithdrawal && len(missing) > 0 {
		facts := s.fetchAdvisoryOSVFacts(ctx, sortedIDs(missing))
		for id := range missing {
			if facts[id].lookup == domain.AdvisoryRecordWithdrawn {
				if withdrawn == nil {
					withdrawn = make(map[string]struct{})
				}
				withdrawn[id] = struct{}{}
			}
		}
	}

	// Enrich lifecycle-relevant versions only.
	for _, a := range analyses {
		if a == nil || a.ReleaseInfo == nil {
			continue
		}
		enrichVersionAdvisories(a.ReleaseInfo.StableVersion, details)
		enrichVersionAdvisories(a.ReleaseInfo.MaxSemverVersion, details)
		if verifyWithdrawal {
			filterWithdrawnAdvisories(a.ReleaseInfo.StableVersion, withdrawn)
			filterWithdrawnAdvisories(a.ReleaseInfo.MaxSemverVersion, withdrawn)
			if a.ReleaseInfo.StableVersion == nil && a.ReleaseInfo.MaxSemverVersion == nil {
				filterWithdrawnAdvisories(a.ReleaseInfo.LatestVersionDetail(), withdrawn)
			}
		}
	}
	return withdrawn
}

func filterWithdrawnAdvisories(vd *domain.VersionDetail, withdrawn map[string]struct{}) {
	if vd == nil || len(withdrawn) == 0 {
		return
	}
	kept := make([]domain.Advisory, 0, len(vd.Advisories))
	for _, adv := range vd.Advisories {
		if _, drop := withdrawn[adv.ID]; drop {
			slog.Debug("withdrawn_advisory_removed", "id", adv.ID, "relation", domain.AdvisoryRelationDirect)
			continue
		}
		kept = append(kept, adv)
	}
	vd.Advisories = kept
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
