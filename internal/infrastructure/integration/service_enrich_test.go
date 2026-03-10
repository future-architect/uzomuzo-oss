package integration

import (
	"testing"

	domain "github.com/future-architect/uzomuzo/internal/domain/analysis"
	"github.com/future-architect/uzomuzo/internal/infrastructure/depsdev"
)

// TestPopulateAnalysisFromBatchResult_EnrichAdvisories ensures enrichment picks up advisory keys from package versions.
func TestPopulateAnalysisFromBatchResult_EnrichAdvisories(t *testing.T) {
	svc := &IntegrationService{}
	analysis := &domain.Analysis{OriginalPURL: "pkg:npm/example@1.0.0", EffectivePURL: "pkg:npm/example@1.0.0"}
	analysis.EnsureCanonical()

	batch := &depsdev.BatchResult{
		PURL: "pkg:npm/example@1.0.0",
		Package: &depsdev.Package{
			PackageKey: depsdev.PackageKey{System: "npm", Name: "example"},
			PURL:       "pkg:npm/example",
			Versions: []depsdev.Version{
				{VersionKey: depsdev.VersionKey{Version: "1.0.0"}, AdvisoryKeys: []depsdev.AdvisoryKey{{ID: "GHSA-test-enrich-1234"}}},
			},
		},
		ReleaseInfo: depsdev.ReleaseInfo{StableVersion: depsdev.Version{VersionKey: depsdev.VersionKey{Version: "1.0.0"}}},
	}

	svc.populateAnalysisFromBatchResult(analysis, batch)

	if analysis.ReleaseInfo == nil || analysis.ReleaseInfo.StableVersion == nil {
		t.Fatalf("expected release info stable version populated")
	}
	if len(analysis.ReleaseInfo.StableVersion.Advisories) != 1 {
		t.Fatalf("expected 1 advisory, got %d", len(analysis.ReleaseInfo.StableVersion.Advisories))
	}
	adv := analysis.ReleaseInfo.StableVersion.Advisories[0]
	if adv.ID != "GHSA-test-enrich-1234" || adv.Source != "GHSA" {
		t.Fatalf("unexpected advisory: %#v", adv)
	}
}
