package github

import (
	"strings"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

func TestEnrichProjectLicenseFromGitHub(t *testing.T) {
	tests := []struct {
		name    string
		current domain.ResolvedLicense
		license *LicenseInfo
		want    domain.ResolvedLicense
		changed bool
	}{
		{
			name:    "nil license no change",
			current: domain.ResolvedLicense{},
			license: nil,
			want:    domain.ResolvedLicense{},
			changed: false,
		},
		{
			name:    "spdx id fills empty",
			current: domain.ResolvedLicense{},
			license: &LicenseInfo{SpdxID: "MIT"},
			want:    domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceGitHubProjectSPDX},
			changed: true,
		},
		{
			name:    "name fallback canonical",
			current: domain.ResolvedLicense{},
			license: &LicenseInfo{Name: "Apache License 2.0"},
			want:    domain.ResolvedLicense{Expression: "Apache-2.0", Raw: "Apache License 2.0", Source: domain.LicenseSourceGitHubProjectSPDX},
			changed: true,
		},
		{
			name:    "no override canonical deps.dev",
			current: domain.ResolvedLicense{Expression: "Apache-2.0", Source: domain.LicenseSourceDepsDevProjectSPDX, Raw: "Apache-2.0"},
			license: &LicenseInfo{SpdxID: "MIT"},
			want:    domain.ResolvedLicense{Expression: "Apache-2.0", Source: domain.LicenseSourceDepsDevProjectSPDX, Raw: "Apache-2.0"},
			changed: false,
		},
		{
			name:    "override non-standard deps.dev",
			current: domain.ResolvedLicense{Expression: "", Raw: "custom-non-spdx", Source: domain.LicenseSourceDepsDevProjectNonStandard},
			license: &LicenseInfo{SpdxID: "GPL-3.0-only"},
			want:    domain.ResolvedLicense{Expression: "GPL-3.0-only", Raw: "GPL-3.0-only", Source: domain.LicenseSourceGitHubProjectSPDX},
			changed: true,
		},
		{
			name:    "NOASSERTION ignored",
			current: domain.ResolvedLicense{},
			license: &LicenseInfo{SpdxID: "NOASSERTION"},
			want:    domain.ResolvedLicense{},
			changed: false,
		},
		{
			name:    "name non-spdx captured",
			current: domain.ResolvedLicense{},
			license: &LicenseInfo{Name: "Custom Non SPDX License Foo"},
			want:    domain.ResolvedLicense{Expression: "", Raw: "Custom Non SPDX License Foo", Source: domain.LicenseSourceGitHubProjectNonStandard},
			changed: true,
		},
		{
			name:    "spdx id casing normalized",
			current: domain.ResolvedLicense{},
			license: &LicenseInfo{SpdxID: strings.ToLower("Apache-2.0")},
			want:    domain.ResolvedLicense{Expression: "Apache-2.0", Raw: strings.ToLower("Apache-2.0"), Source: domain.LicenseSourceGitHubProjectSPDX},
			changed: true,
		},
		{
			name:    "github nonspdx name captured when empty",
			current: domain.ResolvedLicense{},
			license: &LicenseInfo{Name: "Custom Non SPDX License"},
			want:    domain.ResolvedLicense{Expression: "", Raw: "Custom Non SPDX License", Source: domain.LicenseSourceGitHubProjectNonStandard},
			changed: true,
		},
		{
			name:    "depsdev nonstandard overridden by github nonspdx",
			current: domain.ResolvedLicense{Expression: "", Raw: "placeholder", Source: domain.LicenseSourceDepsDevProjectNonStandard},
			license: &LicenseInfo{Name: "Another Custom Non SPDX"},
			want:    domain.ResolvedLicense{Expression: "", Raw: "Another Custom Non SPDX", Source: domain.LicenseSourceGitHubProjectNonStandard},
			changed: true,
		},
		{
			name:    "spdx current not overridden by github nonspdx",
			current: domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevProjectSPDX},
			license: &LicenseInfo{Name: "Custom Non SPDX"},
			want:    domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevProjectSPDX},
			changed: false,
		},
		{
			name:    "depsdev nonstandard overridden by github spdx",
			current: domain.ResolvedLicense{Expression: "", Raw: "placeholder", Source: domain.LicenseSourceDepsDevProjectNonStandard},
			license: &LicenseInfo{SpdxID: "Apache-2.0"},
			want:    domain.ResolvedLicense{Expression: "Apache-2.0", Raw: "Apache-2.0", Source: domain.LicenseSourceGitHubProjectSPDX},
			changed: true,
		},
	}

	for _, tc := range tests {
		c := tc
		t.Run(c.name, func(t *testing.T) {
			updated, changed := enrichProjectLicenseFromGitHub(c.current, c.license)
			if updated != c.want || changed != c.changed {
				t.Fatalf("got (%+v,%v) want (%+v,%v)", updated, changed, c.want, c.changed)
			}
		})
	}
}
