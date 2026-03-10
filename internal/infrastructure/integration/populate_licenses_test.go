package integration

import (
	"testing"

	"github.com/future-architect/uzomuzo/internal/infrastructure/depsdev"
)

// TestBuildVersionLicensesFromVersion validates normalization, deduplication, fallback, and SPDX filtering
// logic implemented in buildVersionLicensesFromVersion. We only assert the resulting sorted Identifier list
// (the helper under test guarantees lexical ordering of identifiers in its output slice).
func TestBuildVersionLicensesFromVersion(t *testing.T) {
	cases := []struct {
		name    string
		version *depsdev.Version
		want    []string
	}{
		{name: "nil", version: nil, want: []string{}},
		{name: "spdx_details_dedup_sorted", version: &depsdev.Version{LicenseDetails: []depsdev.LicenseDetail{{Spdx: "Apache-2.0"}, {Spdx: " apache-2.0 "}, {Spdx: "MIT"}, {Spdx: "NOASSERTION"}}}, want: []string{"Apache-2.0", "MIT"}},
		{name: "fallback_to_licenses", version: &depsdev.Version{Licenses: []string{"mit", "BSD-3-Clause", "NOASSERTION"}}, want: []string{"BSD-3-Clause", "MIT"}},
		{name: "non_normalizable_only", version: &depsdev.Version{LicenseDetails: []depsdev.LicenseDetail{{Spdx: ""}, {Spdx: "NOASSERTION"}}, Licenses: []string{"   ", "Custom Non SPDX"}}, want: []string{"Custom-Non-SPDX"}},
		{name: "spdx_details_prevents_fallback", version: &depsdev.Version{LicenseDetails: []depsdev.LicenseDetail{{Spdx: "mpl-2.0"}}, Licenses: []string{"mit"}}, want: []string{"MPL-2.0"}},
	}
	for _, c := range cases {
		c := c
		gotVLs := buildResolvedLicensesFromVersion(c.version)
		got := make([]string, 0, len(gotVLs))
		for _, vl := range gotVLs {
			got = append(got, vl.Identifier)
		}
		if !equalStringSlices(got, c.want) {
			t.Fatalf("%s: got=%v want=%v", c.name, got, c.want)
		}
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
