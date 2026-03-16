package integration

import (
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
)

// TestPopulateAnalysisFromBatchResult covers registry URL derivation, repo URL fallback, and project absence.
func TestPopulateAnalysisFromBatchResult(t *testing.T) {
	svc := &IntegrationService{}
	purlStr := "pkg:npm/@scope/example@1.0.0"
	analysis := &domain.Analysis{OriginalPURL: purlStr, EffectivePURL: purlStr, Package: &domain.Package{PURL: purlStr, Ecosystem: "npm"}}
	analysis.EnsureCanonical()
	repoURL := "https://github.com/acme/example"
	batch := &depsdev.BatchResult{PURL: purlStr, RepoURL: repoURL}

	svc.populateAnalysisFromBatchResult(analysis, batch)

	if analysis.RepoURL != repoURL {
		t.Fatalf("expected RepoURL %s got %s", repoURL, analysis.RepoURL)
	}
	if analysis.PackageLinks == nil || analysis.PackageLinks.RegistryURL == "" {
		t.Fatalf("expected RegistryURL to be set")
	}
}

// TestPopulateReleaseInfoAdvisories ensures advisory enrichment from version list when ReleaseInfo versions lack advisory keys.
func TestPopulateReleaseInfoAdvisories(t *testing.T) {
	svc := &IntegrationService{}
	purlStr := "pkg:pypi/sample@2.0.0"
	analysis := &domain.Analysis{OriginalPURL: purlStr, EffectivePURL: purlStr, Package: &domain.Package{PURL: purlStr, Ecosystem: "pypi"}}
	analysis.EnsureCanonical()

	// Version with advisory
	vWithAdv := depsdev.Version{VersionKey: depsdev.VersionKey{Version: "2.0.0"}, PublishedAt: time.Now().AddDate(-1, 0, 0), AdvisoryKeys: []depsdev.AdvisoryKey{{ID: "GHSA-XXXX"}}}
	relInfo := depsdev.ReleaseInfo{StableVersion: depsdev.Version{VersionKey: depsdev.VersionKey{Version: "2.0.0"}, PublishedAt: vWithAdv.PublishedAt}}
	batch := &depsdev.BatchResult{PURL: purlStr, Package: &depsdev.Package{Versions: []depsdev.Version{vWithAdv}}, ReleaseInfo: relInfo}

	svc.populateReleaseInfo(analysis, batch)

	if analysis.ReleaseInfo == nil || analysis.ReleaseInfo.StableVersion == nil {
		t.Fatalf("expected stable version populated")
	}
	if len(analysis.ReleaseInfo.StableVersion.Advisories) != 1 {
		t.Fatalf("expected 1 advisory, got %d", len(analysis.ReleaseInfo.StableVersion.Advisories))
	}
}

// TestPopulateHomepagePriority verifies homepage selection priority between version links and project homepage.
func TestPopulateHomepagePriority(t *testing.T) {
	svc := &IntegrationService{}
	purlStr := "pkg:maven/com.acme/example@1.2.3"
	analysis := &domain.Analysis{OriginalPURL: purlStr, EffectivePURL: purlStr, Package: &domain.Package{PURL: purlStr, Ecosystem: "maven"}}
	analysis.EnsureCanonical()

	// Version has a link labelled Home
	ver := depsdev.Version{VersionKey: depsdev.VersionKey{Version: "1.2.3"}, Links: []depsdev.Link{{Label: "Homepage", URL: "https://example.org"}}}
	relInfo := depsdev.ReleaseInfo{StableVersion: ver}
	project := &depsdev.Project{Homepage: "https://project-home.example"}
	batch := &depsdev.BatchResult{PURL: purlStr, Package: &depsdev.Package{Versions: []depsdev.Version{ver}}, ReleaseInfo: relInfo, Project: project}

	svc.populateReleaseInfo(analysis, batch)
	// Homepage should prefer version link over project homepage
	if analysis.PackageLinks == nil || analysis.PackageLinks.HomepageURL != "https://example.org" {
		t.Fatalf("expected homepage from version link, got %+v", analysis.PackageLinks)
	}
}

// TestPopulateLicensesDirect ensures version SPDX licenses are captured without fallback.
func TestPopulateLicensesDirect(t *testing.T) {
	svc := &IntegrationService{}
	purlStr := "pkg:npm/%40scope/example@1.0.0"
	analysis := &domain.Analysis{OriginalPURL: purlStr, EffectivePURL: purlStr, Package: &domain.Package{PURL: purlStr, Ecosystem: "npm", Version: "1.0.0"}}
	analysis.EnsureCanonical()
	// Requested version release info
	rel := depsdev.ReleaseInfo{RequestedVersion: depsdev.Version{VersionKey: depsdev.VersionKey{Version: "1.0.0"}}}
	analysis.ReleaseInfo = &domain.ReleaseInfo{RequestedVersion: &domain.VersionDetail{Version: "1.0.0"}}
	// Batch result with matching version that has license details
	ver := depsdev.Version{VersionKey: depsdev.VersionKey{Version: "1.0.0"}, LicenseDetails: []depsdev.LicenseDetail{{Spdx: "MIT"}}}
	batch := &depsdev.BatchResult{PURL: purlStr, Package: &depsdev.Package{Versions: []depsdev.Version{ver}}, ReleaseInfo: rel, Project: &depsdev.Project{License: "Apache-2.0"}}

	svc.populateLicenses(analysis, batch)
	if analysis.ProjectLicense.Identifier != "Apache-2.0" { // canonical SPDX casing
		t.Fatalf("expected project license Apache-2.0 got %+v", analysis.ProjectLicense)
	}
	if analysis.ProjectLicense.Source != domain.LicenseSourceDepsDevProjectSPDX {
		t.Fatalf("expected project license source %s got %s", domain.LicenseSourceDepsDevProjectSPDX, analysis.ProjectLicense.Source)
	}
	if len(analysis.RequestedVersionLicenses) != 1 || analysis.RequestedVersionLicenses[0].Identifier != "MIT" {
		t.Fatalf("expected requested version license MIT got %+v", analysis.RequestedVersionLicenses)
	}
}

// TestPopulateLicensesFallback ensures fallback to project license when version lacks licenses.
func TestPopulateLicensesFallback(t *testing.T) {
	svc := &IntegrationService{}
	purlStr := "pkg:npm/example@2.0.0"
	analysis := &domain.Analysis{OriginalPURL: purlStr, EffectivePURL: purlStr, Package: &domain.Package{PURL: purlStr, Ecosystem: "npm", Version: "2.0.0"}}
	analysis.EnsureCanonical()
	rel := depsdev.ReleaseInfo{RequestedVersion: depsdev.Version{VersionKey: depsdev.VersionKey{Version: "2.0.0"}}}
	analysis.ReleaseInfo = &domain.ReleaseInfo{RequestedVersion: &domain.VersionDetail{Version: "2.0.0"}}
	ver := depsdev.Version{VersionKey: depsdev.VersionKey{Version: "2.0.0"}} // no license details
	batch := &depsdev.BatchResult{PURL: purlStr, Package: &depsdev.Package{Versions: []depsdev.Version{ver}}, ReleaseInfo: rel, Project: &depsdev.Project{License: "BSD-3-Clause"}}

	svc.populateLicenses(analysis, batch)
	if len(analysis.RequestedVersionLicenses) != 1 || analysis.RequestedVersionLicenses[0].Identifier != "BSD-3-Clause" {
		t.Fatalf("expected fallback BSD-3-Clause got %+v", analysis.RequestedVersionLicenses)
	}
	if analysis.ProjectLicense.Source != domain.LicenseSourceDepsDevProjectSPDX {
		t.Fatalf("expected project license source %s got %s", domain.LicenseSourceDepsDevProjectSPDX, analysis.ProjectLicense.Source)
	}
}
