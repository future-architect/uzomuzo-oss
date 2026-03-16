package integration

import (
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
)

// TestBuildVersionDetail_AdvisoryExtraction verifies all advisory keys are extracted into VersionDetail.Advisories
func TestBuildVersionDetail_AdvisoryExtraction(t *testing.T) {
	svc := &IntegrationService{}
	published := time.Now().Add(-24 * time.Hour)
	src := &depsdev.Version{
		VersionKey:   depsdev.VersionKey{Version: "1.2.3"},
		PublishedAt:  published,
		AdvisoryKeys: []depsdev.AdvisoryKey{{ID: "GHSA-aaaa-bbbb-cccc"}, {ID: "CVE-2024-1234"}},
		IsDeprecated: false,
	}

	vd := svc.buildVersionDetail(src, &domain.Analysis{})
	if vd == nil {
		t.Fatalf("expected VersionDetail, got nil")
	}
	if len(vd.Advisories) != 2 {
		t.Fatalf("expected 2 advisories, got %d (%v)", len(vd.Advisories), vd.Advisories)
	}
	// Find GHSA and CVE entries and validate classification
	var ghsaAdv, cveAdv *domain.Advisory
	for i := range vd.Advisories {
		adv := vd.Advisories[i]
		switch adv.Source {
		case "GHSA":
			ghsaAdv = &adv
		case "CVE":
			cveAdv = &adv
		}
	}
	if ghsaAdv == nil || ghsaAdv.ID != "GHSA-aaaa-bbbb-cccc" || !containsSubstring(ghsaAdv.URL, ghsaAdv.ID) {
		t.Fatalf("GHSA advisory not classified correctly: %#v", ghsaAdv)
	}
	if cveAdv == nil || cveAdv.ID != "CVE-2024-1234" || !containsSubstring(cveAdv.URL, "CVE-2024-1234") {
		t.Fatalf("CVE advisory not classified correctly: %#v", cveAdv)
	}
}

// TestReleaseInfo_LatestAdvisories verifies priority order and counting (Stable > PreRelease fallback).
func TestReleaseInfo_LatestAdvisories(t *testing.T) {
	ri := &domain.ReleaseInfo{
		StableVersion:     &domain.VersionDetail{Advisories: []domain.Advisory{{ID: "GHSA-1", Source: "GHSA", URL: "https://github.com/advisories/GHSA-1"}}},
		PreReleaseVersion: &domain.VersionDetail{Advisories: []domain.Advisory{{ID: "GHSA-2", Source: "GHSA", URL: "https://github.com/advisories/GHSA-2"}}},
	}
	c, advs := ri.LatestAdvisories()
	if c != 1 || len(advs) != 1 || advs[0].ID != "GHSA-1" {
		t.Fatalf("unexpected result: count=%d advisories=%v", c, advs)
	}

	// Remove stable to force fallback
	ri.StableVersion = nil
	c, advs = ri.LatestAdvisories()
	if c != 1 || advs[0].ID != "GHSA-2" {
		t.Fatalf("expected fallback to pre-release advisory GHSA-2, got count=%d advisories=%v", c, advs)
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (string([]byte(s)[len(s)-len(sub):]) == sub || string([]byte(s)[:len(sub)]) == sub || containsAnywhere(s, sub))
}

func containsAnywhere(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestBuildVersionDetail_SetsRegistryURL_ForNPMScoped ensures version-specific registry URL is built for scoped npm packages.
func TestBuildVersionDetail_SetsRegistryURL_ForNPMScoped(t *testing.T) {
	svc := &IntegrationService{}

	analysis := &domain.Analysis{
		Package: &domain.Package{
			PURL:      "pkg:npm/%40types/lodash@4.14.195",
			Ecosystem: "npm",
			Version:   "4.14.195",
		},
	}

	src := &depsdev.Version{
		VersionKey: depsdev.VersionKey{Version: "4.14.195"},
	}

	vd := svc.buildVersionDetail(src, analysis)
	if vd == nil {
		t.Fatalf("buildVersionDetail returned nil")
	}
	if got, want := vd.RegistryURL, "https://www.npmjs.com/package/@types/lodash/v/4.14.195"; got != want {
		t.Fatalf("RegistryURL = %q, want %q", got, want)
	}
}
