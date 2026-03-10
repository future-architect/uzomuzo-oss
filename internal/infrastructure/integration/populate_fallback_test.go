package integration

import (
	"testing"

	domain "github.com/future-architect/uzomuzo/internal/domain/analysis"
)

// TestRequestedVersionLicenseFallbackReplacement verifies replacement of all non-SPDX version licenses
// with the project SPDX license (fallback) and preservation when mixture exists.
func TestRequestedVersionLicenseFallbackReplacement(t *testing.T) {
	// Case: all non-SPDX replaced by project fallback
	an := &domain.Analysis{ProjectLicense: domain.ResolvedLicense{Identifier: "MIT", Raw: "MIT", IsSPDX: true, Source: domain.LicenseSourceDepsDevProjectSPDX},
		RequestedVersionLicenses: []domain.ResolvedLicense{{Identifier: "Custom Non SPDX", Raw: "Custom Non SPDX", IsSPDX: false, Source: domain.LicenseSourceDepsDevVersionRaw}}}
	if !allVersionLicensesNonSPDX(an.RequestedVersionLicenses) {
		t.Fatalf("precondition failed: expected non-SPDX slice")
	}
	if an.ProjectLicense.Identifier != "" && len(an.RequestedVersionLicenses) > 0 && allVersionLicensesNonSPDX(an.RequestedVersionLicenses) {
		an.RequestedVersionLicenses = []domain.ResolvedLicense{{Identifier: an.ProjectLicense.Identifier, Raw: an.ProjectLicense.Identifier, IsSPDX: true, Source: domain.LicenseSourceProjectFallback}}
	}
	if len(an.RequestedVersionLicenses) != 1 || an.RequestedVersionLicenses[0].Identifier != "MIT" || an.RequestedVersionLicenses[0].Source != domain.LicenseSourceProjectFallback {
		t.Fatalf("expected fallback replacement to MIT project-fallback, got %+v", an.RequestedVersionLicenses)
	}

	// Case: mixture retains original
	an2 := &domain.Analysis{ProjectLicense: domain.ResolvedLicense{Identifier: "MIT", Raw: "MIT", IsSPDX: true, Source: domain.LicenseSourceDepsDevProjectSPDX}, RequestedVersionLicenses: []domain.ResolvedLicense{{Identifier: "Apache-2.0", Raw: "Apache-2.0", IsSPDX: true, Source: domain.LicenseSourceDepsDevVersionSPDX}, {Identifier: "Custom Non SPDX", Raw: "Custom Non SPDX", IsSPDX: false, Source: domain.LicenseSourceDepsDevVersionRaw}}}
	if allVersionLicensesNonSPDX(an2.RequestedVersionLicenses) {
		t.Fatalf("should not be all non-SPDX")
	}
	if an2.ProjectLicense.Identifier != "" && len(an2.RequestedVersionLicenses) > 0 && allVersionLicensesNonSPDX(an2.RequestedVersionLicenses) {
		an2.RequestedVersionLicenses = []domain.ResolvedLicense{{Identifier: an2.ProjectLicense.Identifier, Raw: an2.ProjectLicense.Identifier, IsSPDX: true, Source: domain.LicenseSourceProjectFallback}}
	}
	if len(an2.RequestedVersionLicenses) != 2 {
		t.Fatalf("unexpected change: %v", an2.RequestedVersionLicenses)
	}

	// Case: empty slice unchanged
	an3 := &domain.Analysis{ProjectLicense: domain.ResolvedLicense{Identifier: "MIT", Raw: "MIT", IsSPDX: true, Source: domain.LicenseSourceDepsDevProjectSPDX}}
	if an3.ProjectLicense.Identifier != "" && len(an3.RequestedVersionLicenses) > 0 && allVersionLicensesNonSPDX(an3.RequestedVersionLicenses) {
		an3.RequestedVersionLicenses = []domain.ResolvedLicense{{Identifier: an3.ProjectLicense.Identifier, Raw: an3.ProjectLicense.Identifier, IsSPDX: true, Source: domain.LicenseSourceProjectFallback}}
	}
	if len(an3.RequestedVersionLicenses) != 0 {
		t.Fatalf("expected empty, got %v", an3.RequestedVersionLicenses)
	}
}
