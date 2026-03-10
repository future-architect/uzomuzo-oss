package integration

import (
	"testing"

	domain "github.com/future-architect/uzomuzo/internal/domain/analysis"
	"github.com/future-architect/uzomuzo/internal/infrastructure/depsdev"
)

// TestPopulateLicenses_DerivedVersionPromotion ensures that when deps.dev project license is non-standard
// (recorded only as source) and requested version yields a single SPDX, it is promoted to ProjectLicense.
func TestPopulateLicenses_DerivedVersionPromotion(t *testing.T) {
	svc := &IntegrationService{}
	analysis := &domain.Analysis{OriginalPURL: "pkg:npm/example@1.0.0", EffectivePURL: "pkg:npm/example@1.0.0"}
	analysis.EnsureCanonical()
	// Simulate ReleaseInfo with requested version
	analysis.ReleaseInfo = &domain.ReleaseInfo{RequestedVersion: &domain.VersionDetail{Version: "1.0.0"}}
	// Simulate prior step: deps.dev provided non-standard project license string -> we only set source
	analysis.ProjectLicense = domain.ResolvedLicense{Identifier: "", Raw: "non-standard", IsSPDX: false, Source: domain.LicenseSourceDepsDevProjectNonStandard}

	// Batch result with package version that has a single SPDX license
	batch := &depsdev.BatchResult{
		Package: &depsdev.Package{
			Versions: []depsdev.Version{{
				VersionKey: depsdev.VersionKey{Version: "1.0.0"},
				Licenses:   []string{"MIT"},
			}},
		},
		Project: &depsdev.Project{License: "non-standard"},
	}

	svc.populateLicenses(analysis, batch)

	if analysis.ProjectLicense.Identifier != "MIT" {
		t.Fatalf("expected promoted project license MIT got %s", analysis.ProjectLicense.Identifier)
	}
	if analysis.ProjectLicense.Source != domain.LicenseSourceDerivedFromVersion {
		t.Fatalf("expected source derived-version got %s", analysis.ProjectLicense.Source)
	}
	if len(analysis.RequestedVersionLicenses) != 1 || analysis.RequestedVersionLicenses[0].Identifier != "MIT" {
		t.Fatalf("expected requested version licenses [MIT] got %v", analysis.RequestedVersionLicenses)
	}
}
