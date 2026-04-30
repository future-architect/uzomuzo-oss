package integration

import (
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// TestPromoteProjectLicenseFromVersion exercises the leaf-vs-compound rule
// from ADR-0018: only single-leaf version expressions are promoted to
// project-level. Compound expressions ("MIT OR Apache-2.0") are not safe
// project-level claims and must not promote.
func TestPromoteProjectLicenseFromVersion(t *testing.T) {
	tests := []struct {
		name        string
		in          *domain.Analysis
		wantProjExp string
		wantProjSrc string
	}{
		{name: "nil_analysis", in: nil},
		{
			name: "already_has_project_license",
			in: &domain.Analysis{
				ProjectLicense:          domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevProjectSPDX},
				RequestedVersionLicense: domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevVersionSPDX},
			},
			wantProjExp: "MIT",
			wantProjSrc: domain.LicenseSourceDepsDevProjectSPDX,
		},
		{
			name: "compound_version_no_promo",
			in: &domain.Analysis{
				ProjectLicense:          domain.ResolvedLicense{Source: domain.LicenseSourceDepsDevProjectNonStandard, Raw: "non-standard"},
				RequestedVersionLicense: domain.ResolvedLicense{Expression: "MIT OR Apache-2.0", Raw: "MIT OR Apache-2.0", Source: domain.LicenseSourceDepsDevVersionSPDX},
			},
			wantProjExp: "",
			wantProjSrc: domain.LicenseSourceDepsDevProjectNonStandard,
		},
		{
			name: "nonspdx_version_no_promo",
			in: &domain.Analysis{
				ProjectLicense:          domain.ResolvedLicense{Source: domain.LicenseSourceDepsDevProjectNonStandard, Raw: "non-standard"},
				RequestedVersionLicense: domain.ResolvedLicense{Expression: "", Raw: "Custom Non SPDX", Source: domain.LicenseSourceDepsDevVersionRaw},
			},
			wantProjExp: "",
			wantProjSrc: domain.LicenseSourceDepsDevProjectNonStandard,
		},
		{
			name: "single_leaf_promoted_when_project_nonstandard",
			in: &domain.Analysis{
				ProjectLicense:          domain.ResolvedLicense{Source: domain.LicenseSourceDepsDevProjectNonStandard, Raw: "non-standard"},
				RequestedVersionLicense: domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevVersionSPDX},
			},
			wantProjExp: "MIT",
			wantProjSrc: domain.LicenseSourceDerivedFromVersion,
		},
		{
			name: "single_leaf_promoted_when_project_zero",
			in: &domain.Analysis{
				RequestedVersionLicense: domain.ResolvedLicense{Expression: "Apache-2.0", Raw: "Apache-2.0", Source: domain.LicenseSourceDepsDevVersionSPDX},
			},
			wantProjExp: "Apache-2.0",
			wantProjSrc: domain.LicenseSourceDerivedFromVersion,
		},
		{
			name: "with_exception_treated_as_single_leaf_and_promoted",
			in: &domain.Analysis{
				RequestedVersionLicense: domain.ResolvedLicense{
					Expression: "GPL-2.0-only WITH Classpath-exception-2.0",
					Raw:        "GPL-2.0-only WITH Classpath-exception-2.0",
					Source:     domain.LicenseSourceDepsDevVersionSPDX,
				},
			},
			wantProjExp: "GPL-2.0-only WITH Classpath-exception-2.0",
			wantProjSrc: domain.LicenseSourceDerivedFromVersion,
		},
		{
			name: "noassertion_version_not_promoted",
			in: &domain.Analysis{
				RequestedVersionLicense: domain.ResolvedLicense{Expression: "NOASSERTION", Raw: "NOASSERTION", Source: domain.LicenseSourceDepsDevVersionSPDX},
			},
			wantProjExp: "",
			wantProjSrc: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			promoteProjectLicenseFromVersion(tc.in)
			if tc.in == nil {
				return
			}
			if tc.in.ProjectLicense.Expression != tc.wantProjExp {
				t.Errorf("Expression = %q, want %q", tc.in.ProjectLicense.Expression, tc.wantProjExp)
			}
			if tc.in.ProjectLicense.Source != tc.wantProjSrc {
				t.Errorf("Source = %q, want %q", tc.in.ProjectLicense.Source, tc.wantProjSrc)
			}
		})
	}
}
