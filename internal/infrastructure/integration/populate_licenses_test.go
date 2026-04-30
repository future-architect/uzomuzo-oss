package integration

import (
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
)

// TestBuildVersionLicenseFromDepsDev exercises the deps.dev → ResolvedLicense
// collapse: priority order (LicenseDetails.Spdx → Licenses fallback),
// OR-joining of multi-entry inputs, NOASSERTION mid-array drop, non-SPDX
// preservation in Raw, and the latent-bug fix where deps.dev returns a
// pre-compounded SPDX expression as a single LicenseDetails.Spdx string.
func TestBuildVersionLicenseFromDepsDev(t *testing.T) {
	cases := []struct {
		name       string
		version    *depsdev.Version
		wantExpr   string
		wantSource string
		wantZero   bool
	}{
		{name: "nil", version: nil, wantZero: true},
		{
			name:       "spdx_details_dedup",
			version:    &depsdev.Version{LicenseDetails: []depsdev.LicenseDetail{{Spdx: "Apache-2.0"}, {Spdx: " apache-2.0 "}, {Spdx: "MIT"}, {Spdx: "NOASSERTION"}}},
			wantExpr:   "Apache-2.0 OR MIT",
			wantSource: domain.LicenseSourceDepsDevVersionSPDX,
		},
		{
			name:       "fallback_to_licenses_when_details_empty",
			version:    &depsdev.Version{Licenses: []string{"mit", "BSD-3-Clause", "NOASSERTION"}},
			wantExpr:   "MIT OR BSD-3-Clause",
			wantSource: domain.LicenseSourceDepsDevVersionSPDX,
		},
		{
			// LicenseDetails[].Spdx contains a single NOASSERTION which the
			// normalizer preserves as the SPDX sentinel. Empty-string entries
			// are filtered out before reaching the normalizer.
			name:       "noassertion_only_in_details_preserved",
			version:    &depsdev.Version{LicenseDetails: []depsdev.LicenseDetail{{Spdx: ""}, {Spdx: "NOASSERTION"}}, Licenses: []string{"   ", "Custom Non SPDX"}},
			wantExpr:   "NOASSERTION",
			wantSource: domain.LicenseSourceDepsDevVersionSPDX,
		},
		{
			// All entries are non-SPDX heuristic strings — JoinExpressions
			// returns "" entry-wise so the result carries Raw only.
			name:       "all_nonstandard_keeps_raw",
			version:    &depsdev.Version{Licenses: []string{"Custom Non SPDX", "Acme Vendor"}},
			wantExpr:   "",
			wantSource: domain.LicenseSourceDepsDevVersionRaw,
		},
		{
			name:       "spdx_details_prevents_fallback",
			version:    &depsdev.Version{LicenseDetails: []depsdev.LicenseDetail{{Spdx: "mpl-2.0"}}, Licenses: []string{"mit"}},
			wantExpr:   "MPL-2.0",
			wantSource: domain.LicenseSourceDepsDevVersionSPDX,
		},
		{
			name:       "depsdev_already_compound_in_single_entry",
			version:    &depsdev.Version{LicenseDetails: []depsdev.LicenseDetail{{Spdx: "MIT OR Apache-2.0"}}},
			wantExpr:   "MIT OR Apache-2.0",
			wantSource: domain.LicenseSourceDepsDevVersionSPDX,
		},
		{
			name:       "with_exception_preserved",
			version:    &depsdev.Version{LicenseDetails: []depsdev.LicenseDetail{{Spdx: "GPL-2.0-only WITH Classpath-exception-2.0"}}},
			wantExpr:   "GPL-2.0-only WITH Classpath-exception-2.0",
			wantSource: domain.LicenseSourceDepsDevVersionSPDX,
		},
		{
			name:       "noassertion_only",
			version:    &depsdev.Version{Licenses: []string{"NOASSERTION"}},
			wantExpr:   "NOASSERTION",
			wantSource: domain.LicenseSourceDepsDevVersionSPDX,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildVersionLicenseFromDepsDev(c.version)
			if c.wantZero {
				if !got.IsZero() {
					t.Fatalf("expected zero, got %+v", got)
				}
				return
			}
			if got.Expression != c.wantExpr {
				t.Errorf("Expression: got %q, want %q", got.Expression, c.wantExpr)
			}
			if got.Source != c.wantSource {
				t.Errorf("Source: got %q, want %q", got.Source, c.wantSource)
			}
		})
	}
}
