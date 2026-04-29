package licenses

import "testing"

func TestLookupLicenseURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "apache_https", in: "https://www.apache.org/licenses/LICENSE-2.0", want: "Apache-2.0"},
		{name: "apache_http_txt", in: "http://www.apache.org/licenses/LICENSE-2.0.txt", want: "Apache-2.0"},
		{name: "apache_no_www", in: "http://apache.org/licenses/LICENSE-2.0", want: "Apache-2.0"},
		{name: "apache_html", in: "https://www.apache.org/licenses/LICENSE-2.0.html", want: "Apache-2.0"},
		{name: "apache_trailing_slash", in: "https://www.apache.org/licenses/LICENSE-2.0/", want: "Apache-2.0"},
		{name: "mit_opensource", in: "https://opensource.org/licenses/MIT", want: "MIT"},
		{name: "mit_opensource_lower", in: "https://opensource.org/licenses/mit", want: "MIT"},
		{name: "mit_org", in: "https://mit-license.org", want: "MIT"},
		{name: "mit_org_path", in: "https://mit-license.org/", want: "MIT"},
		{name: "mit_legacy_php", in: "http://www.opensource.org/licenses/mit-license.php", want: "MIT"},
		{name: "bsd2", in: "https://opensource.org/licenses/BSD-2-Clause", want: "BSD-2-Clause"},
		{name: "bsd3", in: "https://opensource.org/licenses/BSD-3-Clause", want: "BSD-3-Clause"},
		{name: "gpl2", in: "https://www.gnu.org/licenses/gpl-2.0.html", want: "GPL-2.0-only"},
		{name: "gpl3", in: "https://www.gnu.org/licenses/gpl-3.0", want: "GPL-3.0-only"},
		{name: "lgpl21", in: "https://www.gnu.org/licenses/old-licenses/lgpl-2.1.html", want: "LGPL-2.1-only"},
		{name: "mpl2", in: "https://www.mozilla.org/MPL/2.0/", want: "MPL-2.0"},
		{name: "mpl2_en_us", in: "https://www.mozilla.org/en-US/MPL/2.0/", want: "MPL-2.0"},
		{name: "epl_v10", in: "https://www.eclipse.org/legal/epl-v10.html", want: "EPL-1.0"},
		{name: "epl_2", in: "https://www.eclipse.org/legal/epl-2.0", want: "EPL-2.0"},
		{name: "cc0", in: "https://creativecommons.org/publicdomain/zero/1.0/", want: "CC0-1.0"},
		{name: "unlicense", in: "https://unlicense.org/", want: "Unlicense"},
		{name: "no_scheme", in: "apache.org/licenses/LICENSE-2.0", want: "Apache-2.0"},
		{name: "with_query", in: "https://www.apache.org/licenses/LICENSE-2.0?foo=1", want: "Apache-2.0"},
		{name: "with_fragment", in: "https://opensource.org/licenses/MIT#section1", want: "MIT"},
		{name: "unknown_url", in: "https://example.com/some/license", want: ""},
		{name: "empty", in: "", want: ""},
		{name: "whitespace", in: "   ", want: ""},
		{name: "github_raw_unknown", in: "https://raw.githubusercontent.com/foo/bar/main/LICENSE", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LookupLicenseURL(tt.in); got != tt.want {
				t.Errorf("LookupLicenseURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
