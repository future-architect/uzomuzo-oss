package analysis

import "testing"

func TestNormalizeLicenseIdentifier(t *testing.T) {
	cases := []struct {
		in   string
		want string
		spdx bool
	}{
		{"MIT", "MIT", true},
		{"mit", "MIT", true},
		{"Apache-2.0", "Apache-2.0", true},
		{"apache 2.0", "Apache-2.0", true},
		{"Apache License 2.0", "Apache-2.0", true},
		{"BSD 3-Clause", "BSD-3-Clause", true},
		{"BSD 3-Clause License", "BSD-3-Clause", true},
		{"GNU General Public License v3", "GPL-3.0-only", true},
		{"Unknown Custom License", "Unknown-Custom-License", false},
		{"", "", false},
		{"   MIT License  ", "MIT", true},
		{"NOASSERTION", "NOASSERTION", false},
	}
	for _, c := range cases {
		got, spdx := NormalizeLicenseIdentifier(c.in)
		if got != c.want || spdx != c.spdx {
			t.Errorf("NormalizeLicenseIdentifier(%q) = (%q,%v) want (%q,%v)", c.in, got, spdx, c.want, c.spdx)
		}
	}
}
