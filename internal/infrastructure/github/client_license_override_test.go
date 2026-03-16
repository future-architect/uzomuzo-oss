package github

import (
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// TestLicenseOverrideNonStandard ensures GitHub SPDX license overrides non-standard deps.dev project license.
func TestLicenseOverrideNonStandard(t *testing.T) {
	analysis := &domain.Analysis{ProjectLicense: domain.ResolvedLicense{Raw: "custom-non-spdx", Source: domain.LicenseSourceDepsDevProjectNonStandard}}
	lic := &LicenseInfo{SpdxID: "MIT", Name: "MIT License"}

	updated, changed := enrichProjectLicenseFromGitHub(analysis.ProjectLicense, lic)
	if !changed {
		t.Fatalf("expected change true")
	}
	analysis.ProjectLicense = updated
	if analysis.ProjectLicense.Identifier != "MIT" {
		t.Fatalf("expected MIT got %#v", analysis.ProjectLicense)
	}
	if analysis.ProjectLicense.Source != domain.LicenseSourceGitHubProjectSPDX {
		t.Fatalf("expected source github got %s", analysis.ProjectLicense.Source)
	}
}

// TestLicenseNoOverrideCanonical ensures a canonical deps.dev SPDX license is not overridden by different GitHub SPDX.
func TestLicenseNoOverrideCanonical(t *testing.T) {
	analysis := &domain.Analysis{ProjectLicense: domain.ResolvedLicense{Identifier: "Apache-2.0", Raw: "Apache-2.0", Source: domain.LicenseSourceDepsDevProjectSPDX, IsSPDX: true}}
	lic := &LicenseInfo{SpdxID: "MIT", Name: "MIT License"}
	updated, changed := enrichProjectLicenseFromGitHub(analysis.ProjectLicense, lic)
	if changed {
		t.Fatalf("unexpected change for canonical project license")
	}
	if updated.Identifier != "Apache-2.0" || updated.Source != domain.LicenseSourceDepsDevProjectSPDX {
		t.Fatalf("expected Apache-2.0/deps.dev got %+v", updated)
	}
}
