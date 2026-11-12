package integration

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// enrichHexAdvisories records hex.pm security advisories on Hex analyses.
// deps.dev does not index Hex, so these analyses otherwise have no ReleaseInfo
// and no advisories. The latest stable release becomes StableVersion, the
// version every advisory consumer reads (ReleaseInfo.LatestVersionDetail and
// the lifecycle assessor). An existing StableVersion is left untouched.
//
// hex.pm scores are CVSS v4.0 and are stored in CVSS3Score: the two versions
// share the severity bands (LOW < 4.0, MEDIUM < 7.0, HIGH < 9.0, CRITICAL), but
// a v4.0 score is not numerically equal to the v3.1 score of the same advisory.
//
// Must run after enrichAdvisorySeverity: deps.dev returns GHSA records for Hex
// advisories with cvss3Score 0, which would overwrite the hex.pm score.
// Namespaced PURLs (private Hex organizations) are skipped.
func (s *IntegrationService) enrichHexAdvisories(ctx context.Context, analyses map[string]*domain.Analysis) {
	if s.hexClient == nil || len(analyses) == 0 {
		return
	}
	jobs := collectPackageJobs(analyses, func(parsed *purl.ParsedPURL) (packageJobKey, packageFetch[*domain.VersionDetail]) {
		name := strings.ToLower(strings.TrimSpace(parsed.PackageName()))
		if parsed.Ecosystem() != "hex" || parsed.Namespace() != "" || name == "" {
			return packageJobKey{}, nil
		}
		return packageJobKey{ecosystem: "hex", name: name}, s.fetchHexStable
	})
	runPackageJobs(ctx, "hex_advisories", jobs, func(a *domain.Analysis, vd *domain.VersionDetail) {
		if vd == nil {
			return
		}
		if a.ReleaseInfo == nil {
			a.ReleaseInfo = &domain.ReleaseInfo{}
		}
		if a.ReleaseInfo.StableVersion != nil {
			return
		}
		// Copy per analysis: later enrichments mutate advisories in place.
		cp := *vd
		cp.Advisories = slices.Clone(vd.Advisories)
		a.ReleaseInfo.StableVersion = &cp
	})
}

// fetchHexStable returns the latest stable Hex release with its advisories,
// sorted by score descending as enrichVersionAdvisories does for deps.dev.
func (s *IntegrationService) fetchHexStable(ctx context.Context, name string) (*domain.VersionDetail, bool, error) {
	version, advs, err := s.hexClient.GetStableAdvisories(ctx, name)
	if err != nil || version == "" {
		return nil, false, err
	}
	vd := &domain.VersionDetail{Version: version}
	for _, adv := range advs {
		src, u := classifyAdvisory(adv.ID)
		vd.Advisories = append(vd.Advisories, domain.Advisory{
			ID: adv.ID, Source: src, URL: u, Title: adv.Summary,
			CVSS3Score: adv.CVSSScore, Severity: domain.SeverityFromCVSS3(adv.CVSSScore),
		})
	}
	sort.SliceStable(vd.Advisories, func(i, j int) bool {
		return vd.Advisories[i].CVSS3Score > vd.Advisories[j].CVSS3Score
	})
	return vd, true, nil
}
