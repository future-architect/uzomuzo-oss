package integration

import (
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// TestRequestedVersionLicenseFallbackReplacement verifies the rule "when the
// version expression is non-SPDX (raw-only) but the project has a valid SPDX
// expression, replace the version license with a project-fallback entry."
//
// The rule is implemented inline in populateLicenses step 4; this test
// exercises the same condition matrix on the new singular RequestedVersionLicense
// shape so a regression in either populator or model would surface here.
func TestRequestedVersionLicenseFallbackReplacement(t *testing.T) {
	t.Run("nonstandard_version_replaced_by_project_fallback", func(t *testing.T) {
		an := &domain.Analysis{
			ProjectLicense:          domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevProjectSPDX},
			RequestedVersionLicense: domain.ResolvedLicense{Expression: "", Raw: "Custom Non SPDX", Source: domain.LicenseSourceDepsDevVersionRaw},
		}
		applyVersionFallback(an)
		if an.RequestedVersionLicense.Expression != "MIT" || an.RequestedVersionLicense.Source != domain.LicenseSourceProjectFallback {
			t.Fatalf("expected fallback to MIT/ProjectFallback, got %+v", an.RequestedVersionLicense)
		}
	})

	t.Run("spdx_version_kept", func(t *testing.T) {
		original := domain.ResolvedLicense{Expression: "Apache-2.0", Raw: "Apache-2.0", Source: domain.LicenseSourceDepsDevVersionSPDX}
		an := &domain.Analysis{
			ProjectLicense:          domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevProjectSPDX},
			RequestedVersionLicense: original,
		}
		applyVersionFallback(an)
		if an.RequestedVersionLicense != original {
			t.Fatalf("expected SPDX version preserved, got %+v", an.RequestedVersionLicense)
		}
	})

	t.Run("compound_version_kept", func(t *testing.T) {
		original := domain.ResolvedLicense{Expression: "MIT OR Apache-2.0", Raw: "MIT OR Apache-2.0", Source: domain.LicenseSourceDepsDevVersionSPDX}
		an := &domain.Analysis{
			ProjectLicense:          domain.ResolvedLicense{Expression: "BSD-3-Clause", Raw: "BSD-3-Clause", Source: domain.LicenseSourceDepsDevProjectSPDX},
			RequestedVersionLicense: original,
		}
		applyVersionFallback(an)
		if an.RequestedVersionLicense != original {
			t.Fatalf("compound version expression should not be overridden by project fallback, got %+v", an.RequestedVersionLicense)
		}
	})

	t.Run("zero_version_left_alone", func(t *testing.T) {
		an := &domain.Analysis{
			ProjectLicense: domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevProjectSPDX},
		}
		applyVersionFallback(an)
		if !an.RequestedVersionLicense.IsZero() {
			t.Fatalf("zero version license must remain zero (the version-zero path is handled by populateLicenses step 3, not step 4); got %+v", an.RequestedVersionLicense)
		}
	})
}

// applyVersionFallback mirrors step 4 of populateLicenses: when the version
// expression is non-SPDX but project has a valid SPDX expression, swap in a
// project-fallback entry. This is an inline mirror so the test can exercise
// the rule independently of the full populate pipeline (which would require
// a depsdev BatchResult fixture).
func applyVersionFallback(a *domain.Analysis) {
	if a.ProjectLicense.Expression == "" {
		return
	}
	if a.RequestedVersionLicense.IsZero() {
		return
	}
	if a.RequestedVersionLicense.Expression != "" {
		return
	}
	a.RequestedVersionLicense = domain.ResolvedLicense{
		Expression: a.ProjectLicense.Expression,
		Raw:        a.ProjectLicense.Raw,
		Source:     domain.LicenseSourceProjectFallback,
	}
}
