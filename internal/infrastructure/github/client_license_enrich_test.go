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
			want:    domain.ResolvedLicense{Identifier: "MIT", Raw: "MIT", IsSPDX: true, Source: domain.LicenseSourceGitHubProjectSPDX},
			changed: true,
		},
		{
			name:    "name fallback canonical",
			current: domain.ResolvedLicense{},
			license: &LicenseInfo{Name: "Apache License 2.0"},
			want:    domain.ResolvedLicense{Identifier: "Apache-2.0", Raw: "Apache License 2.0", IsSPDX: true, Source: domain.LicenseSourceGitHubProjectSPDX},
			changed: true,
		},
		{
			name:    "no override canonical deps.dev",
			current: domain.ResolvedLicense{Identifier: "Apache-2.0", Source: domain.LicenseSourceDepsDevProjectSPDX, Raw: "Apache-2.0", IsSPDX: true},
			license: &LicenseInfo{SpdxID: "MIT"},
			want:    domain.ResolvedLicense{Identifier: "Apache-2.0", Source: domain.LicenseSourceDepsDevProjectSPDX, Raw: "Apache-2.0", IsSPDX: true},
			changed: false,
		},
		{
			name:    "override non-standard deps.dev",
			current: domain.ResolvedLicense{Identifier: "", Raw: "custom-non-spdx", Source: domain.LicenseSourceDepsDevProjectNonStandard, IsSPDX: false},
			license: &LicenseInfo{SpdxID: "GPL-3.0-only"},
			want:    domain.ResolvedLicense{Identifier: "GPL-3.0-only", Raw: "GPL-3.0-only", IsSPDX: true, Source: domain.LicenseSourceGitHubProjectSPDX},
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
			want:    domain.ResolvedLicense{Identifier: "", Raw: "Custom Non SPDX License Foo", IsSPDX: false, Source: domain.LicenseSourceGitHubProjectNonStandard},
			changed: true,
		},
		{
			name:    "spdx id casing normalized",
			current: domain.ResolvedLicense{},
			license: &LicenseInfo{SpdxID: strings.ToLower("Apache-2.0")},
			want:    domain.ResolvedLicense{Identifier: "Apache-2.0", Raw: strings.ToLower("Apache-2.0"), IsSPDX: true, Source: domain.LicenseSourceGitHubProjectSPDX},
			changed: true,
		},
		{
			name:    "github nonspdx name captured when empty",
			current: domain.ResolvedLicense{},
			license: &LicenseInfo{Name: "Custom Non SPDX License"},
			want:    domain.ResolvedLicense{Identifier: "", Raw: "Custom Non SPDX License", IsSPDX: false, Source: domain.LicenseSourceGitHubProjectNonStandard},
			changed: true,
		},
		{
			name:    "depsdev nonstandard overridden by github nonspdx",
			current: domain.ResolvedLicense{Identifier: "", Raw: "placeholder", IsSPDX: false, Source: domain.LicenseSourceDepsDevProjectNonStandard},
			license: &LicenseInfo{Name: "Another Custom Non SPDX"},
			want:    domain.ResolvedLicense{Identifier: "", Raw: "Another Custom Non SPDX", IsSPDX: false, Source: domain.LicenseSourceGitHubProjectNonStandard},
			changed: true,
		},
		{
			name:    "spdx current not overridden by github nonspdx",
			current: domain.ResolvedLicense{Identifier: "MIT", Raw: "MIT", IsSPDX: true, Source: domain.LicenseSourceDepsDevProjectSPDX},
			license: &LicenseInfo{Name: "Custom Non SPDX"},
			want:    domain.ResolvedLicense{Identifier: "MIT", Raw: "MIT", IsSPDX: true, Source: domain.LicenseSourceDepsDevProjectSPDX},
			changed: false,
		},
		{
			name:    "depsdev nonstandard overridden by github spdx",
			current: domain.ResolvedLicense{Identifier: "", Raw: "placeholder", IsSPDX: false, Source: domain.LicenseSourceDepsDevProjectNonStandard},
			license: &LicenseInfo{SpdxID: "Apache-2.0"},
			want:    domain.ResolvedLicense{Identifier: "Apache-2.0", Raw: "Apache-2.0", IsSPDX: true, Source: domain.LicenseSourceGitHubProjectSPDX},
			changed: true,
		},
	}

	for _, tc := range tests {
		// capture range var
		c := tc
		t.Run(c.name, func(t *testing.T) {
			updated, changed := enrichProjectLicenseFromGitHub(c.current, c.license)
			if updated != c.want || changed != c.changed {
				// deep compare fields manually (struct has no slices)
				if updated.Identifier != c.want.Identifier || updated.Source != c.want.Source || updated.Raw != c.want.Raw || updated.IsSPDX != c.want.IsSPDX || changed != c.changed {
					t.Fatalf("got (%+v,%v) want (%+v,%v)", updated, changed, c.want, c.changed)
				}
			}
		})
	}
}
