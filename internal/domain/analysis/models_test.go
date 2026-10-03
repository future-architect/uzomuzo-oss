package analysis

import (
	"testing"
)

// --- ResolvedLicense helpers ---

func TestResolvedLicense_IsZero(t *testing.T) {
	tests := []struct {
		name string
		in   ResolvedLicense
		want bool
	}{
		{name: "zero_value", in: ResolvedLicense{}, want: true},
		{name: "spdx_expression", in: ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: LicenseSourceDepsDevProjectSPDX}, want: false},
		{name: "noassertion_expression", in: ResolvedLicense{Expression: "NOASSERTION", Raw: "NOASSERTION", Source: LicenseSourceDepsDevVersionSPDX}, want: false},
		{name: "nonstandard_raw_only", in: ResolvedLicense{Expression: "", Raw: "Custom License", Source: LicenseSourceDepsDevProjectNonStandard}, want: false},
		{name: "compound_expression", in: ResolvedLicense{Expression: "MIT OR Apache-2.0", Raw: "MIT OR Apache-2.0", Source: LicenseSourceDepsDevVersionSPDX}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v (input=%+v)", got, tt.want, tt.in)
			}
		})
	}
}

func TestResolvedLicense_IsNonStandard(t *testing.T) {
	tests := []struct {
		name string
		in   ResolvedLicense
		want bool
	}{
		{name: "zero_value", in: ResolvedLicense{}, want: false},
		{name: "spdx_project", in: ResolvedLicense{Expression: "Apache-2.0", Raw: "Apache-2.0", Source: LicenseSourceDepsDevProjectSPDX}, want: false},
		{name: "noassertion_is_recognized", in: ResolvedLicense{Expression: "NOASSERTION", Raw: "NOASSERTION", Source: LicenseSourceDepsDevVersionSPDX}, want: false},
		{name: "compound_is_recognized", in: ResolvedLicense{Expression: "MIT OR Apache-2.0", Raw: "MIT, Apache-2.0", Source: LicenseSourceDepsDevVersionSPDX}, want: false},
		{name: "project_nonstandard", in: ResolvedLicense{Expression: "", Raw: "Custom License", Source: LicenseSourceDepsDevProjectNonStandard}, want: true},
		{name: "github_nonstandard", in: ResolvedLicense{Expression: "", Raw: "See LICENSE", Source: LicenseSourceGitHubProjectNonStandard}, want: true},
		{name: "version_raw", in: ResolvedLicense{Expression: "", Raw: "Proprietary Vendor License", Source: LicenseSourceDepsDevVersionRaw}, want: true},
		{name: "maven_pom_nonstandard", in: ResolvedLicense{Expression: "", Raw: "Custom Internal License", Source: LicenseSourceMavenPOMNonStandard}, want: true},
		{name: "maven_pom_spdx", in: ResolvedLicense{Expression: "Apache-2.0", Raw: "Apache-2.0", Source: LicenseSourceMavenPOMSPDX}, want: false},
		{name: "clearlydefined_nonstandard", in: ResolvedLicense{Expression: "", Raw: "LicenseRef-scancode-unknown", Source: LicenseSourceClearlyDefinedNonStandard}, want: true},
		{name: "clearlydefined_spdx", in: ResolvedLicense{Expression: "Apache-2.0", Raw: "Apache-2.0", Source: LicenseSourceClearlyDefinedSPDX}, want: false},
		{name: "fallback_from_project", in: ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: LicenseSourceProjectFallback}, want: false},
		{name: "derived_from_version", in: ResolvedLicense{Expression: "BSD-3-Clause", Raw: "BSD-3-Clause", Source: LicenseSourceDerivedFromVersion}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.IsNonStandard(); got != tt.want {
				t.Errorf("IsNonStandard() = %v, want %v (input=%+v)", got, tt.want, tt.in)
			}
		})
	}
}

func TestResolvedLicense_IsUsableSPDX(t *testing.T) {
	tests := []struct {
		name string
		in   ResolvedLicense
		want bool
	}{
		{name: "zero_value", in: ResolvedLicense{}, want: false},
		{name: "single_leaf_spdx", in: ResolvedLicense{Expression: "MIT", Source: LicenseSourceDepsDevProjectSPDX, Raw: "MIT"}, want: true},
		{name: "compound_or_spdx", in: ResolvedLicense{Expression: "MIT OR Apache-2.0", Source: LicenseSourceDepsDevVersionSPDX}, want: true},
		{name: "compound_with_exception", in: ResolvedLicense{Expression: "GPL-2.0-only WITH Classpath-exception-2.0", Source: LicenseSourceDepsDevProjectSPDX}, want: true},
		{name: "noassertion_recognized_but_not_usable", in: ResolvedLicense{Expression: "NOASSERTION", Raw: "NOASSERTION", Source: LicenseSourceDepsDevVersionSPDX}, want: false},
		{name: "non_standard_only_raw", in: ResolvedLicense{Expression: "", Raw: "Proprietary", Source: LicenseSourceDepsDevVersionRaw}, want: false},
		{name: "github_non_standard", in: ResolvedLicense{Expression: "", Raw: "See LICENSE", Source: LicenseSourceGitHubProjectNonStandard}, want: false},
		{name: "project_fallback_spdx_is_usable", in: ResolvedLicense{Expression: "MIT", Source: LicenseSourceProjectFallback, Raw: "MIT"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.IsUsableSPDX(); got != tt.want {
				t.Errorf("IsUsableSPDX() = %v, want %v (input=%+v)", got, tt.want, tt.in)
			}
		})
	}
}
