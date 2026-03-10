package integration

import (
	"testing"

	domain "github.com/future-architect/uzomuzo/internal/domain/analysis"
)

// TestPromoteProjectLicenseFromVersion_Table consolidates promotion scenarios including edge cases.
func TestPromoteProjectLicenseFromVersion(t *testing.T) {
	base := func(ids []string, src string, proj string) *domain.Analysis {
		var rvl []domain.ResolvedLicense
		for _, id := range ids {
			if id == "" {
				continue
			}
			rvl = append(rvl, domain.ResolvedLicense{Identifier: id, Raw: id, IsSPDX: true, Source: "test"})
		}
		al := domain.ResolvedLicense{}
		if proj != "" {
			al = domain.ResolvedLicense{Identifier: proj, Raw: proj, IsSPDX: true, Source: src}
		} else if src == domain.LicenseSourceDepsDevProjectNonStandard {
			al = domain.ResolvedLicense{Identifier: "", Raw: "non-standard", IsSPDX: false, Source: src}
		}
		return &domain.Analysis{RequestedVersionLicenses: rvl, ProjectLicense: al}
	}
	tests := []struct {
		name    string
		in      *domain.Analysis
		wantPL  string
		wantSrc string
	}{
		{name: "nil_analysis", in: nil, wantPL: "", wantSrc: ""},
		{name: "already_has_project_license", in: base([]string{"MIT"}, domain.LicenseSourceDepsDevProjectSPDX, "MIT"), wantPL: "MIT", wantSrc: domain.LicenseSourceDepsDevProjectSPDX},
		{name: "multiple_version_licenses_no_promo", in: base([]string{"MIT", "Apache-2.0"}, domain.LicenseSourceDepsDevProjectNonStandard, ""), wantPL: "", wantSrc: domain.LicenseSourceDepsDevProjectNonStandard},
		{name: "single_nonspdx_no_promo", in: &domain.Analysis{RequestedVersionLicenses: []domain.ResolvedLicense{{Identifier: "Custom Non SPDX", Raw: "Custom Non SPDX", IsSPDX: false, Source: "test"}}, ProjectLicense: domain.ResolvedLicense{Identifier: "", Raw: "non-standard", IsSPDX: false, Source: domain.LicenseSourceDepsDevProjectNonStandard}}, wantPL: "", wantSrc: domain.LicenseSourceDepsDevProjectNonStandard},
		{name: "single_spdx_nonstandard_source_promoted", in: base([]string{"MIT"}, domain.LicenseSourceDepsDevProjectNonStandard, ""), wantPL: "MIT", wantSrc: domain.LicenseSourceDerivedFromVersion},
		{name: "single_spdx_no_source_promoted", in: base([]string{"Apache-2.0"}, "", ""), wantPL: "Apache-2.0", wantSrc: domain.LicenseSourceDerivedFromVersion},
		{name: "no_source_noassertion_not_promoted", in: &domain.Analysis{RequestedVersionLicenses: []domain.ResolvedLicense{{Identifier: "NOASSERTION", Raw: "NOASSERTION", IsSPDX: true, Source: "test"}}}, wantPL: "", wantSrc: ""},
	}
	for _, tc := range tests {
		promoteProjectLicenseFromVersion(tc.in)
		if tc.in == nil {
			if tc.wantPL != "" || tc.wantSrc != "" {
				t.Fatalf("%s: expected empty outputs for nil", tc.name)
			}
			continue
		}
		if tc.in.ProjectLicense.Identifier != tc.wantPL || tc.in.ProjectLicense.Source != tc.wantSrc {
			t.Fatalf("%s: got (%s,%s) want (%s,%s)", tc.name, tc.in.ProjectLicense.Identifier, tc.in.ProjectLicense.Source, tc.wantPL, tc.wantSrc)
		}
	}
}
