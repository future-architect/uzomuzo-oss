package maven

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

const (
	pomSingleSPDXName = `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>org.apache.commons</groupId>
  <artifactId>commons-lang3</artifactId>
  <version>3.12.0</version>
  <licenses>
    <license>
      <name>Apache License, Version 2.0</name>
      <url>https://www.apache.org/licenses/LICENSE-2.0.txt</url>
    </license>
  </licenses>
</project>`

	pomURLOnly = `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>legacy</artifactId>
  <version>1.0</version>
  <licenses>
    <license>
      <url>http://www.opensource.org/licenses/mit-license.php</url>
    </license>
  </licenses>
</project>`

	pomMultiLicense = `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>dual</artifactId>
  <version>1.0</version>
  <licenses>
    <license>
      <name>CDDL 1.1</name>
      <url>https://glassfish.dev.java.net/public/CDDL+GPL_1_1.html</url>
    </license>
    <license>
      <name>GPL-2.0-with-classpath-exception</name>
    </license>
  </licenses>
</project>`

	pomNoLicenses = `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>nolicense</artifactId>
  <version>1.0</version>
</project>`

	pomNonStandardName = `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>internal</artifactId>
  <version>1.0</version>
  <licenses>
    <license>
      <name>Acme Internal License</name>
    </license>
  </licenses>
</project>`

	pomPlaceholderName = `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>placeholder</artifactId>
  <version>1.0</version>
  <properties>
    <license.name>BSD 3-Clause License</license.name>
  </properties>
  <licenses>
    <license>
      <name>${license.name}</name>
    </license>
  </licenses>
</project>`
)

func TestFetchLicenses(t *testing.T) {
	cases := []struct {
		name      string
		pomBody   string
		statusGet int
		wantFound bool
		want      []domain.ResolvedLicense
	}{
		{
			// "Apache License, Version 2.0" does not normalize via
			// NormalizeLicenseIdentifier (the comma trips it), so resolution
			// falls through to URL lookup. Raw should preserve the URL that
			// produced the SPDX match.
			name:      "single_spdx_via_url_fallback",
			pomBody:   pomSingleSPDXName,
			statusGet: http.StatusOK,
			wantFound: true,
			want: []domain.ResolvedLicense{{
				Identifier: "Apache-2.0",
				Source:     domain.LicenseSourceMavenPOMSPDX,
				Raw:        "https://www.apache.org/licenses/LICENSE-2.0.txt",
				IsSPDX:     true,
			}},
		},
		{
			name:      "url_only_resolves_to_spdx",
			pomBody:   pomURLOnly,
			statusGet: http.StatusOK,
			wantFound: true,
			want: []domain.ResolvedLicense{{
				Identifier: "MIT",
				Source:     domain.LicenseSourceMavenPOMSPDX,
				Raw:        "http://www.opensource.org/licenses/mit-license.php",
				IsSPDX:     true,
			}},
		},
		{
			name:      "multi_license_emits_each_entry",
			pomBody:   pomMultiLicense,
			statusGet: http.StatusOK,
			wantFound: true,
			want: []domain.ResolvedLicense{
				{
					Identifier: "CDDL-1.1",
					Source:     domain.LicenseSourceMavenPOMSPDX,
					Raw:        "CDDL 1.1",
					IsSPDX:     true,
				},
				{
					Identifier: "GPL-2.0-with-classpath-exception",
					Source:     domain.LicenseSourceMavenPOMSPDX,
					Raw:        "GPL-2.0-with-classpath-exception",
					IsSPDX:     true,
				},
			},
		},
		{
			name:      "no_licenses_returns_not_found",
			pomBody:   pomNoLicenses,
			statusGet: http.StatusOK,
			wantFound: false,
		},
		{
			name:      "non_standard_name_emits_nonstandard",
			pomBody:   pomNonStandardName,
			statusGet: http.StatusOK,
			wantFound: true,
			want: []domain.ResolvedLicense{{
				Source: domain.LicenseSourceMavenPOMNonStandard,
				Raw:    "Acme Internal License",
			}},
		},
		{
			name:      "property_placeholder_expanded_then_normalized",
			pomBody:   pomPlaceholderName,
			statusGet: http.StatusOK,
			wantFound: true,
			want: []domain.ResolvedLicense{{
				Identifier: "BSD-3-Clause",
				Source:     domain.LicenseSourceMavenPOMSPDX,
				Raw:        "BSD 3-Clause License",
				IsSPDX:     true,
			}},
		},
		{
			name:      "pom_404_returns_not_found",
			statusGet: http.StatusNotFound,
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, ".pom") {
					http.Error(w, "expected .pom", http.StatusBadRequest)
					return
				}
				if tc.statusGet != 0 && tc.statusGet != http.StatusOK {
					http.Error(w, "x", tc.statusGet)
					return
				}
				w.Header().Set("Content-Type", "application/xml")
				_, _ = w.Write([]byte(tc.pomBody))
			}))
			defer ts.Close()

			c := NewClient()
			c.SetBaseURL(ts.URL)

			got, found, err := c.FetchLicenses(context.Background(), "g", "a", "1.0")
			if err != nil {
				t.Fatalf("FetchLicenses err: %v", err)
			}
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v", found, tc.wantFound)
			}
			if !found {
				if len(got) != 0 {
					t.Fatalf("got %d licenses on not-found, want 0", len(got))
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d licenses, want %d (%+v)", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("license[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestFetchLicenses_RequiredArgs(t *testing.T) {
	c := NewClient()
	_, _, err := c.FetchLicenses(context.Background(), "", "a", "1.0")
	if err == nil {
		t.Fatalf("expected error for empty groupID")
	}
	_, _, err = c.FetchLicenses(context.Background(), "g", "", "1.0")
	if err == nil {
		t.Fatalf("expected error for empty artifactID")
	}
	_, _, err = c.FetchLicenses(context.Background(), "g", "a", "")
	if err == nil {
		t.Fatalf("expected error for empty version")
	}
}

func TestResolvePOMLicense(t *testing.T) {
	tests := []struct {
		name string
		in   struct{ name, urlStr string }
		want domain.ResolvedLicense
	}{
		{
			name: "name_exact_spdx",
			in:   struct{ name, urlStr string }{name: "Apache-2.0"},
			want: domain.ResolvedLicense{
				Identifier: "Apache-2.0",
				Source:     domain.LicenseSourceMavenPOMSPDX,
				Raw:        "Apache-2.0",
				IsSPDX:     true,
			},
		},
		{
			name: "name_alias",
			in:   struct{ name, urlStr string }{name: "Apache License 2.0"},
			want: domain.ResolvedLicense{
				Identifier: "Apache-2.0",
				Source:     domain.LicenseSourceMavenPOMSPDX,
				Raw:        "Apache License 2.0",
				IsSPDX:     true,
			},
		},
		{
			name: "url_only",
			in:   struct{ name, urlStr string }{urlStr: "https://opensource.org/licenses/MIT"},
			want: domain.ResolvedLicense{
				Identifier: "MIT",
				Source:     domain.LicenseSourceMavenPOMSPDX,
				Raw:        "https://opensource.org/licenses/MIT",
				IsSPDX:     true,
			},
		},
		{
			name: "name_takes_precedence_over_url",
			in:   struct{ name, urlStr string }{name: "MIT", urlStr: "https://opensource.org/licenses/Apache-2.0"},
			want: domain.ResolvedLicense{
				Identifier: "MIT",
				Source:     domain.LicenseSourceMavenPOMSPDX,
				Raw:        "MIT",
				IsSPDX:     true,
			},
		},
		{
			name: "name_unknown_url_resolves_raw_preserves_url",
			in:   struct{ name, urlStr string }{name: "Acme Apache License", urlStr: "https://www.apache.org/licenses/LICENSE-2.0"},
			want: domain.ResolvedLicense{
				Identifier: "Apache-2.0",
				Source:     domain.LicenseSourceMavenPOMSPDX,
				Raw:        "https://www.apache.org/licenses/LICENSE-2.0",
				IsSPDX:     true,
			},
		},
		{
			name: "name_unknown_url_unknown_nonstandard",
			in:   struct{ name, urlStr string }{name: "Acme Internal License", urlStr: "https://acme.example/license"},
			want: domain.ResolvedLicense{
				Source: domain.LicenseSourceMavenPOMNonStandard,
				Raw:    "Acme Internal License",
			},
		},
		{
			name: "url_unknown_no_name_nonstandard_with_url_as_raw",
			in:   struct{ name, urlStr string }{urlStr: "https://acme.example/license"},
			want: domain.ResolvedLicense{
				Source: domain.LicenseSourceMavenPOMNonStandard,
				Raw:    "https://acme.example/license",
			},
		},
		{
			name: "all_empty_returns_zero",
			in:   struct{ name, urlStr string }{},
			want: domain.ResolvedLicense{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePOMLicense(tt.in.name, tt.in.urlStr)
			if got != tt.want {
				t.Errorf("resolvePOMLicense(name=%q, url=%q) = %+v, want %+v", tt.in.name, tt.in.urlStr, got, tt.want)
			}
		})
	}
}
