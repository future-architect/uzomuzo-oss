package integration

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/links"
)

// populateProjectScorecard extracts project metadata & scorecard signals.
func (s *IntegrationService) populateProjectScorecard(analysis *domain.Analysis, batchResult *depsdev.BatchResult) {
	project := batchResult.Project
	if analysis.Repository == nil {
		analysis.Repository = &domain.Repository{}
	}
	analysis.Repository.URL = analysis.RepoURL
	if analysis.Repository.Name == "" && analysis.RepoURL != "" {
		if owner, name, err := common.ExtractGitHubOwnerRepo(analysis.RepoURL); err == nil && owner != "" && name != "" {
			if analysis.Repository.Owner == "" { // don't overwrite if already set upstream
				analysis.Repository.Owner = owner
			}
			analysis.Repository.Name = name
		}
	}
	analysis.Repository.StarsCount = project.StarsCount
	analysis.Repository.ForksCount = project.ForksCount
	analysis.Repository.Description = project.Description
	analysis.OverallScore = project.Scorecard.OverallScore
	projectKey := project.ProjectKey.ID
	if projectKey != "" {
		analysis.ScorecardURL = fmt.Sprintf("https://scorecard.dev/viewer/?uri=%s", url.QueryEscape(projectKey))
		analysis.ScorecardAPIURL = fmt.Sprintf("https://api.scorecard.dev/projects/%s", projectKey)
	}
	checks := s.extractScorecardChecks(project)
	if len(checks) > 0 {
		scores := make(map[string]*domain.ScoreEntity)
		for _, check := range checks {
			scores[check.Name] = domain.NewScoreEntity(check.Name, check.Score, 10, check.Reason)
		}
		analysis.Scores = scores
	}

	// Detect archived status from Scorecard "Maintained" check reason.
	// When GITHUB_TOKEN is unavailable, this is the only source of archive detection.
	for _, check := range checks {
		if check.Name == "Maintained" && strings.Contains(strings.ToLower(check.Reason), "project is archived") {
			if analysis.RepoState == nil {
				analysis.RepoState = &domain.RepoState{}
			}
			analysis.RepoState.IsArchived = true
			break
		}
	}
}

// populateReleaseInfo builds domain.ReleaseInfo & related links.
func (s *IntegrationService) populateReleaseInfo(analysis *domain.Analysis, batchResult *depsdev.BatchResult) {
	releaseInfo := batchResult.ReleaseInfo
	if releaseInfo.StableVersion.VersionKey.Version == "" && releaseInfo.PreReleaseVersion.VersionKey.Version == "" && releaseInfo.MaxSemverVersion.VersionKey.Version == "" && releaseInfo.RequestedVersion.VersionKey.Version == "" {
		return
	}
	analysis.ReleaseInfo = &domain.ReleaseInfo{}
	if releaseInfo.StableVersion.VersionKey.Version != "" {
		analysis.ReleaseInfo.StableVersion = s.buildVersionDetail(&releaseInfo.StableVersion, analysis)
	}
	if releaseInfo.PreReleaseVersion.VersionKey.Version != "" {
		if vd := s.buildVersionDetail(&releaseInfo.PreReleaseVersion, analysis); vd != nil {
			vd.IsPrerelease = true
			analysis.ReleaseInfo.PreReleaseVersion = vd
		}
	}
	if releaseInfo.MaxSemverVersion.VersionKey.Version != "" {
		analysis.ReleaseInfo.MaxSemverVersion = s.buildVersionDetail(&releaseInfo.MaxSemverVersion, analysis)
		if analysis.ReleaseInfo.MaxSemverVersion != nil {
			analysis.ReleaseInfo.MaxSemverVersion.IsPrerelease = !purl.IsStableVersion(releaseInfo.MaxSemverVersion.VersionKey.Version)
		}
	}
	if releaseInfo.RequestedVersion.VersionKey.Version != "" {
		analysis.ReleaseInfo.RequestedVersion = s.buildVersionDetail(&releaseInfo.RequestedVersion, analysis)
		if analysis.ReleaseInfo.RequestedVersion != nil {
			analysis.ReleaseInfo.RequestedVersion.IsPrerelease = !purl.IsStableVersion(releaseInfo.RequestedVersion.VersionKey.Version)
		}
	}

	if analysis.PackageLinks == nil {
		analysis.PackageLinks = &domain.PackageLinks{}
	}
	var candidates []*depsdev.Version
	if releaseInfo.StableVersion.VersionKey.Version != "" {
		candidates = append(candidates, &releaseInfo.StableVersion)
	}
	if releaseInfo.PreReleaseVersion.VersionKey.Version != "" {
		candidates = append(candidates, &releaseInfo.PreReleaseVersion)
	}
	if releaseInfo.MaxSemverVersion.VersionKey.Version != "" {
		candidates = append(candidates, &releaseInfo.MaxSemverVersion)
	}
	if releaseInfo.RequestedVersion.VersionKey.Version != "" {
		candidates = append(candidates, &releaseInfo.RequestedVersion)
	}

	if analysis.PackageLinks.HomepageURL == "" {
		for _, v := range candidates {
			if v == nil {
				continue
			}
			for _, l := range v.Links {
				ll := strings.ToLower(l.Label)
				if strings.Contains(ll, "home") || strings.Contains(ll, "project") {
					analysis.PackageLinks.HomepageURL = l.URL
					break
				}
			}
			if analysis.PackageLinks.HomepageURL != "" {
				break
			}
		}
	}
	if batchResult.Project != nil && analysis.PackageLinks.HomepageURL == "" && batchResult.Project.Homepage != "" {
		analysis.PackageLinks.HomepageURL = batchResult.Project.Homepage
	}

	if analysis.Package != nil {
		parser := purl.NewParser()
		raw := analysis.Package.PURL
		if u, err := url.PathUnescape(raw); err == nil && u != "" {
			raw = u
		}
		if parsed, err := parser.Parse(raw); err == nil {
			pkgName := parsed.GetPackageName()
			group := parsed.Namespace()
			finalName := pkgName
			if group != "" {
				switch strings.ToLower(strings.TrimSpace(analysis.Package.Ecosystem)) {
				case "maven":
					finalName = group + ":" + pkgName
				case "packagist", "composer", "npm":
					finalName = group + "/" + pkgName
				}
			}
			if analysis.PackageLinks.RegistryURL == "" {
				analysis.PackageLinks.RegistryURL = links.BuildPackageRegistryURL(analysis.Package.Ecosystem, finalName)
			}
		}
	}

	if batchResult.Package != nil && len(batchResult.Package.Versions) > 0 {
		advisoryIndex := make(map[string][]depsdev.AdvisoryKey, len(batchResult.Package.Versions))
		for _, v := range batchResult.Package.Versions {
			if len(v.AdvisoryKeys) > 0 {
				advisoryIndex[v.VersionKey.Version] = v.AdvisoryKeys
			}
		}
		enrich := func(vd *domain.VersionDetail) {
			if vd == nil || len(vd.Advisories) > 0 {
				return
			}
			if keys, ok := advisoryIndex[vd.Version]; ok {
				for _, adv := range keys {
					src, url := classifyAdvisory(adv.ID)
					vd.Advisories = append(vd.Advisories, domain.Advisory{ID: adv.ID, Source: src, URL: url})
				}
			}
		}
		enrich(analysis.ReleaseInfo.StableVersion)
		enrich(analysis.ReleaseInfo.MaxSemverVersion)
		enrich(analysis.ReleaseInfo.PreReleaseVersion)
		enrich(analysis.ReleaseInfo.RequestedVersion)
	}
}
