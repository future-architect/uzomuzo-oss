package integration

import (
	"context"
	"log/slog"
	"sort"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
)

// enrichAdvisorySeverity enriches Stable and MaxSemver severity. When
// verifyWithdrawal is true, it makes OSV by-ID requests to confirm withdrawal
// (including on the selected fallback version) and to score zero-v3 details
// on Stable and MaxSemver. When false, it preserves prior v4 evidence without
// making by-ID requests.
// See ADR-0010, ADR-0036, and ADR-0037.
func (s *IntegrationService) enrichAdvisorySeverity(ctx context.Context, analyses map[string]*domain.Analysis, verifyWithdrawal bool) map[string]struct{} {
	idSet := make(map[string]struct{})
	v4Scope := make(map[string]struct{})
	for _, a := range analyses {
		if a == nil || a.ReleaseInfo == nil {
			continue
		}
		collectAdvisoryIDs(a.ReleaseInfo.StableVersion, idSet)
		collectAdvisoryIDs(a.ReleaseInfo.MaxSemverVersion, idSet)
		collectAdvisoryIDs(a.ReleaseInfo.StableVersion, v4Scope)
		collectAdvisoryIDs(a.ReleaseInfo.MaxSemverVersion, v4Scope)
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
	var facts map[string]osvFact
	if verifyWithdrawal {
		v4IDs := make(map[string]struct{})
		for id, detail := range details {
			if _, selected := v4Scope[id]; selected && detail != nil && detail.CVSS3Score == 0 {
				v4IDs[id] = struct{}{}
			}
		}
		facts = s.fetchAdvisoryOSVFacts(ctx, orderedOSVFactIDs(missing, nil, v4IDs))
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
		enrichVersionAdvisories(a.ReleaseInfo.StableVersion, details, facts)
		enrichVersionAdvisories(a.ReleaseInfo.MaxSemverVersion, details, facts)
		if verifyWithdrawal {
			filterSelectedWithdrawnAdvisories(a.ReleaseInfo, withdrawn)
		}
	}
	return withdrawn
}

// filterSelectedWithdrawnAdvisories applies the same version scope to either
// pass's confirmed IDs. See ADR-0036.
func filterSelectedWithdrawnAdvisories(ri *domain.ReleaseInfo, withdrawn map[string]struct{}) {
	if ri == nil || len(withdrawn) == 0 {
		return
	}
	filterWithdrawnAdvisories(ri.StableVersion, withdrawn)
	filterWithdrawnAdvisories(ri.MaxSemverVersion, withdrawn)
	if ri.StableVersion == nil && ri.MaxSemverVersion == nil {
		filterWithdrawnAdvisories(ri.LatestVersionDetail(), withdrawn)
	}
}

func filterWithdrawnAdvisories(vd *domain.VersionDetail, withdrawn map[string]struct{}) {
	if vd == nil || len(withdrawn) == 0 {
		return
	}
	kept := make([]domain.Advisory, 0, len(vd.Advisories))
	for _, adv := range vd.Advisories {
		if _, drop := withdrawn[adv.ID]; drop {
			relation := adv.Relation
			if relation == "" {
				relation = domain.AdvisoryRelationDirect
			}
			slog.Debug("withdrawn_advisory_removed", "id", adv.ID, "relation", relation)
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

// enrichVersionAdvisories fills severity data and sorts by effective score. See ADR-0037.
func enrichVersionAdvisories(vd *domain.VersionDetail, details map[string]*depsdev.AdvisoryDetail, facts ...map[string]osvFact) {
	if vd == nil || len(vd.Advisories) == 0 {
		return
	}
	for i := range vd.Advisories {
		adv := &vd.Advisories[i]
		if detail, ok := details[adv.ID]; ok {
			adv.Title = detail.Title
			adv.CVSS3Score = detail.CVSS3Score
			if detail.CVSS3Score > 0 {
				adv.CVSS4LookupFailed = false
			}
			if detail.CVSS3Score == 0 && len(facts) > 0 {
				if f, ok := facts[0][adv.ID]; ok {
					adv.CVSS4Score, adv.CVSS4Known = f.rec.CVSS4Score, f.rec.CVSS4Known
					adv.CVSS4LookupFailed = f.lookup == domain.AdvisoryRecordLookupFailed
				}
			}
			score, known := adv.EffectiveSeverityScore()
			adv.Severity = domain.SeverityFromScore(score, known)
		}
	}
	// Use a stable sort so advisories with equal scores keep their existing order.
	sort.SliceStable(vd.Advisories, func(i, j int) bool {
		left, _ := vd.Advisories[i].EffectiveSeverityScore()
		right, _ := vd.Advisories[j].EffectiveSeverityScore()
		return left > right
	})
}
