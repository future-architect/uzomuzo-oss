package cli

import (
	"testing"

	analysispkg "github.com/future-architect/uzomuzo/internal/domain/analysis"
)

func TestPickVersionedPURL(t *testing.T) {
	tests := []struct {
		name     string
		analysis *analysispkg.Analysis
		want     string
	}{
		{
			name:     "nil_analysis",
			analysis: nil,
			want:     "",
		},
		{
			name: "effective_purl_with_version",
			analysis: &analysispkg.Analysis{
				EffectivePURL: "pkg:npm/lodash@4.17.21",
				OriginalPURL:  "pkg:npm/lodash@4.17.20",
			},
			want: "pkg:npm/lodash@4.17.21",
		},
		{
			name: "effective_purl_without_version_fallback_to_original",
			analysis: &analysispkg.Analysis{
				EffectivePURL: "pkg:npm/lodash",
				OriginalPURL:  "pkg:npm/lodash@4.17.20",
			},
			want: "pkg:npm/lodash@4.17.20",
		},
		{
			name: "both_without_version_fallback_to_stable",
			analysis: &analysispkg.Analysis{
				EffectivePURL: "pkg:npm/lodash",
				OriginalPURL:  "pkg:npm/lodash",
				ReleaseInfo: &analysispkg.ReleaseInfo{
					StableVersion: &analysispkg.VersionDetail{Version: "4.17.21"},
				},
			},
			want: "pkg:npm/lodash@4.17.21",
		},
		{
			name: "no_versions_anywhere",
			analysis: &analysispkg.Analysis{
				EffectivePURL: "pkg:npm/lodash",
				OriginalPURL:  "pkg:npm/lodash",
			},
			want: "",
		},
		{
			name: "empty_purls_with_stable_version",
			analysis: &analysispkg.Analysis{
				ReleaseInfo: &analysispkg.ReleaseInfo{
					StableVersion: &analysispkg.VersionDetail{Version: "1.0.0"},
				},
			},
			want: "",
		},
		{
			name:     "empty_purls_no_release_info",
			analysis: &analysispkg.Analysis{},
			want:     "",
		},
		{
			name: "release_info_nil_stable_version",
			analysis: &analysispkg.Analysis{
				OriginalPURL: "pkg:pypi/requests",
				ReleaseInfo:  &analysispkg.ReleaseInfo{StableVersion: nil},
			},
			want: "",
		},
		{
			name: "release_info_empty_stable_version_string",
			analysis: &analysispkg.Analysis{
				OriginalPURL: "pkg:pypi/requests",
				ReleaseInfo: &analysispkg.ReleaseInfo{
					StableVersion: &analysispkg.VersionDetail{Version: ""},
				},
			},
			want: "",
		},
		{
			name: "only_prerelease_in_release_info_no_stable",
			analysis: &analysispkg.Analysis{
				OriginalPURL: "pkg:npm/next",
				ReleaseInfo: &analysispkg.ReleaseInfo{
					PreReleaseVersion: &analysispkg.VersionDetail{Version: "15.0.0-rc.1"},
					MaxSemverVersion:  &analysispkg.VersionDetail{Version: "15.0.0-rc.1"},
				},
			},
			want: "",
		},
		{
			name: "maven_scoped_purl_stable_fallback",
			analysis: &analysispkg.Analysis{
				OriginalPURL: "pkg:maven/org.slf4j/slf4j-api",
				ReleaseInfo: &analysispkg.ReleaseInfo{
					StableVersion: &analysispkg.VersionDetail{Version: "2.0.16"},
				},
			},
			want: "pkg:maven/org.slf4j/slf4j-api@2.0.16",
		},
		{
			name: "golang_purl_effective_versioned",
			analysis: &analysispkg.Analysis{
				EffectivePURL: "pkg:golang/github.com/gin-gonic/gin@v1.9.1",
				OriginalPURL:  "pkg:golang/github.com/gin-gonic/gin",
			},
			want: "pkg:golang/github.com/gin-gonic/gin@v1.9.1",
		},
		{
			name: "purl_with_qualifiers_no_version",
			analysis: &analysispkg.Analysis{
				EffectivePURL: "pkg:npm/%40babel/core?repository_url=https://github.com/babel/babel",
				OriginalPURL:  "pkg:npm/%40babel/core",
				ReleaseInfo: &analysispkg.ReleaseInfo{
					StableVersion: &analysispkg.VersionDetail{Version: "7.25.0"},
				},
			},
			want: "pkg:npm/%40babel/core@7.25.0",
		},
		{
			name: "effective_purl_preferred_over_stable_fallback",
			analysis: &analysispkg.Analysis{
				EffectivePURL: "pkg:npm/express@4.18.2",
				OriginalPURL:  "pkg:npm/express",
				ReleaseInfo: &analysispkg.ReleaseInfo{
					StableVersion: &analysispkg.VersionDetail{Version: "4.21.0"},
				},
			},
			want: "pkg:npm/express@4.18.2",
		},
		{
			name: "use_original_purl_base_for_stable_fallback",
			analysis: &analysispkg.Analysis{
				EffectivePURL: "",
				OriginalPURL:  "pkg:cargo/serde",
				ReleaseInfo: &analysispkg.ReleaseInfo{
					StableVersion: &analysispkg.VersionDetail{Version: "1.0.210"},
				},
			},
			want: "pkg:cargo/serde@1.0.210",
		},
		{
			name: "effective_purl_base_when_original_empty",
			analysis: &analysispkg.Analysis{
				EffectivePURL: "pkg:gem/rails",
				OriginalPURL:  "",
				ReleaseInfo: &analysispkg.ReleaseInfo{
					StableVersion: &analysispkg.VersionDetail{Version: "7.1.3"},
				},
			},
			want: "pkg:gem/rails@7.1.3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pickVersionedPURL(tt.analysis)
			if got != tt.want {
				t.Errorf("pickVersionedPURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPurlHasVersion(t *testing.T) {
	tests := []struct {
		purl string
		want bool
	}{
		{"pkg:npm/lodash@4.17.21", true},
		{"pkg:npm/lodash", false},
		{"pkg:maven/org.slf4j/slf4j-api@2.0.16", true},
		{"pkg:maven/org.slf4j/slf4j-api", false},
		{"pkg:npm/%40babel/core@7.25.0", true},
		{"pkg:npm/%40babel/core", false},
		// '@' in qualifiers should NOT be treated as version
		{"pkg:npm/foo?repo=https://github.com/user@org/repo", false},
		{"", false},
		{"pkg:gem/rails@7.0.0?platform=ruby", true},
	}
	for _, tt := range tests {
		t.Run(tt.purl, func(t *testing.T) {
			got := purlHasVersion(tt.purl)
			if got != tt.want {
				t.Errorf("purlHasVersion(%q) = %v, want %v", tt.purl, got, tt.want)
			}
		})
	}
}

func TestComposerVendorNameFromPURL(t *testing.T) {
	tests := []struct {
		name       string
		purl       string
		wantVendor string
		wantName   string
	}{
		{
			name:       "basic_composer",
			purl:       "pkg:composer/monolog/monolog",
			wantVendor: "monolog",
			wantName:   "monolog",
		},
		{
			name:       "versioned_composer",
			purl:       "pkg:composer/fzaninotto/faker@1.9.2",
			wantVendor: "fzaninotto",
			wantName:   "faker",
		},
		{
			name:       "composer_with_qualifiers",
			purl:       "pkg:composer/vendor/pkg@1.0.0?repository_url=https://example.com",
			wantVendor: "vendor",
			wantName:   "pkg",
		},
		{
			name:       "non_composer_single_segment",
			purl:       "pkg:npm/express",
			wantVendor: "",
			wantName:   "",
		},
		{
			name:       "missing_vendor",
			purl:       "pkg:composer/onlyname",
			wantVendor: "",
			wantName:   "",
		},
		{
			name:       "empty_string",
			purl:       "",
			wantVendor: "",
			wantName:   "",
		},
		{
			name:       "no_pkg_prefix",
			purl:       "invalid",
			wantVendor: "",
			wantName:   "",
		},
		{
			name:       "composer_three_segments",
			purl:       "pkg:composer/a/b/c",
			wantVendor: "a",
			wantName:   "b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, n := composerVendorNameFromPURL(tt.purl)
			if v != tt.wantVendor {
				t.Errorf("composerVendorNameFromPURL(%q) vendor = %q, want %q", tt.purl, v, tt.wantVendor)
			}
			if n != tt.wantName {
				t.Errorf("composerVendorNameFromPURL(%q) name = %q, want %q", tt.purl, n, tt.wantName)
			}
		})
	}
}
