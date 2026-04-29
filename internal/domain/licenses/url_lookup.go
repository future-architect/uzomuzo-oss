package licenses

import (
	"net/url"
	"strings"
)

// LookupLicenseURL maps a license URL (e.g. from a Maven pom.xml <license><url>
// element or a NuGet .nuspec <licenseUrl>) to a canonical SPDX identifier when
// the URL is a well-known licence reference. Returns the empty string when the
// URL is not recognized.
//
// Comparison is case-insensitive on host and path. Scheme, query, fragment,
// userinfo, port, and trailing ".txt"/".html"/"/" are ignored. Recognized
// hosts include the canonical license publishers (apache.org, opensource.org,
// gnu.org, mozilla.org, eclipse.org, creativecommons.org, unlicense.org,
// mit-license.org, www.gnu.org variants, etc.).
//
// The table is intentionally small and hand-curated. Coverage targets the
// long tail of legacy Maven <license><url>-only entries and pre-2019 NuGet
// <licenseUrl> values where no <license> expression is present.
func LookupLicenseURL(rawURL string) string {
	key := normalizeLicenseURL(rawURL)
	if key == "" {
		return ""
	}
	return licenseURLToSPDX[key]
}

// normalizeLicenseURL produces the lookup key for a license URL.
// Returns empty string when the input cannot be parsed.
func normalizeLicenseURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Some pom.xml <license><url> entries omit the scheme entirely.
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	host = strings.TrimPrefix(host, "www.")
	path := strings.ToLower(u.EscapedPath())
	// Strip trailing /, .txt, .html, .htm extensions which are interchangeable.
	path = strings.TrimSuffix(path, "/")
	for _, suf := range []string{".txt", ".html", ".htm"} {
		if strings.HasSuffix(path, suf) {
			path = strings.TrimSuffix(path, suf)
			break
		}
	}
	if path == "" {
		return host
	}
	return host + path
}

// licenseURLToSPDX is the curated lookup table. Keys are produced by
// normalizeLicenseURL — host (without "www." prefix) + path, both lowercased,
// with trailing slash and .txt/.html/.htm stripped.
var licenseURLToSPDX = map[string]string{
	// Apache
	"apache.org/licenses/license-2.0":      "Apache-2.0",
	"apache.org/licenses":                  "Apache-2.0",
	"opensource.org/licenses/apache-2.0":   "Apache-2.0",
	"opensource.org/licenses/apache2.0":    "Apache-2.0",
	"opensource.org/licenses/apachepl-1.1": "Apache-1.1",

	// MIT
	"opensource.org/licenses/mit":             "MIT",
	"opensource.org/licenses/mit-license":     "MIT",
	"opensource.org/licenses/mit-license.php": "MIT",
	"mit-license.org":                         "MIT",

	// BSD family
	"opensource.org/licenses/bsd-2-clause":    "BSD-2-Clause",
	"opensource.org/licenses/bsd-3-clause":    "BSD-3-Clause",
	"opensource.org/licenses/bsd-license":     "BSD-2-Clause",
	"opensource.org/licenses/bsd-license.php": "BSD-2-Clause",

	// GPL family (gnu.org and opensource.org)
	"gnu.org/licenses/gpl-2.0":               "GPL-2.0-only",
	"gnu.org/licenses/gpl-3.0":               "GPL-3.0-only",
	"gnu.org/licenses/gpl":                   "GPL-3.0-only",
	"gnu.org/licenses/lgpl-2.1":              "LGPL-2.1-only",
	"gnu.org/licenses/lgpl-3.0":              "LGPL-3.0-only",
	"gnu.org/licenses/lgpl":                  "LGPL-3.0-only",
	"gnu.org/licenses/agpl-3.0":              "AGPL-3.0-only",
	"gnu.org/licenses/agpl":                  "AGPL-3.0-only",
	"gnu.org/licenses/old-licenses/gpl-2.0":  "GPL-2.0-only",
	"gnu.org/licenses/old-licenses/lgpl-2.1": "LGPL-2.1-only",
	"opensource.org/licenses/gpl-2.0":        "GPL-2.0-only",
	"opensource.org/licenses/gpl-3.0":        "GPL-3.0-only",
	"opensource.org/licenses/lgpl-2.1":       "LGPL-2.1-only",
	"opensource.org/licenses/lgpl-3.0":       "LGPL-3.0-only",
	"opensource.org/licenses/agpl-3.0":       "AGPL-3.0-only",

	// Mozilla
	"mozilla.org/mpl/2.0":             "MPL-2.0",
	"mozilla.org/en-us/mpl/2.0":       "MPL-2.0",
	"opensource.org/licenses/mpl-2.0": "MPL-2.0",
	"mozilla.org/mpl/1.1":             "MPL-1.1",

	// Eclipse
	"eclipse.org/legal/epl-v10":       "EPL-1.0",
	"eclipse.org/legal/epl-1.0":       "EPL-1.0",
	"eclipse.org/legal/epl-2.0":       "EPL-2.0",
	"opensource.org/licenses/epl-1.0": "EPL-1.0",
	"opensource.org/licenses/epl-2.0": "EPL-2.0",

	// Creative Commons / public domain
	"creativecommons.org/publicdomain/zero/1.0": "CC0-1.0",
	"creativecommons.org/licenses/by/4.0":       "CC-BY-4.0",
	"creativecommons.org/licenses/by-sa/4.0":    "CC-BY-SA-4.0",
	"unlicense.org":                             "Unlicense",

	// ISC
	"opensource.org/licenses/isc":         "ISC",
	"opensource.org/licenses/isc-license": "ISC",
}
