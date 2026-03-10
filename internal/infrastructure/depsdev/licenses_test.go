package depsdev

import "testing"

// TestCollectVersionLicenses covers normalization, fallback, deduplication and exclusion rules.
// DDD Layer: Infrastructure (tests internal helper behavior pure function semantics)
func TestCollectVersionLicenses(t *testing.T) {
	tests := []struct {
		name    string
		version *Version
		want    []string
	}{
		{
			name:    "single SPDX from details",
			version: &Version{LicenseDetails: []LicenseDetail{{Spdx: "MIT"}}},
			want:    []string{"MIT"},
		},
		{
			name:    "alias in Licenses fallback normalizes",
			version: &Version{Licenses: []string{"Apache License 2.0"}},
			want:    []string{"Apache-2.0"},
		},
		{
			name: "deduplicate mixed casing and alias",
			version: &Version{
				LicenseDetails: []LicenseDetail{{Spdx: "apache-2.0"}, {Spdx: "Apache-2.0"}},
				Licenses:       []string{"APACHE 2.0", "Apache License 2.0"},
			},
			want: []string{"Apache-2.0"},
		},
		{
			name: "noassertion excluded fallback picks MIT",
			version: &Version{
				LicenseDetails: []LicenseDetail{{Spdx: "NOASSERTION"}},
				Licenses:       []string{"MIT"},
			},
			want: []string{"MIT"},
		},
		{
			name:    "multiple distinct SPDX sorted",
			version: &Version{LicenseDetails: []LicenseDetail{{Spdx: "MIT"}, {Spdx: "Apache-2.0"}, {Spdx: "MIT"}}},
			want:    []string{"Apache-2.0", "MIT"},
		},
		{
			name:    "fallback heuristic non-spdx token kept",
			version: &Version{Licenses: []string{"Custom-Lic 1.0"}},
			// NormalizeLicenseIdentifier fallback: spaces -> dash
			want: []string{"Custom-Lic-1.0"},
		},
		{
			name:    "nil version returns nil",
			version: nil,
			want:    nil,
		},
	}

	for _, tc := range tests {
		// capture range variable
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := collectVersionLicenses(tc.version)
			if tc.version == nil {
				if got != nil {
					t.Fatalf("expected nil slice for nil version, got=%v", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("length mismatch got=%v want=%v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("index %d got=%q want=%q full=%v", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}
