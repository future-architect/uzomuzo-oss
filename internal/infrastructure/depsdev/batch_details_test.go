package depsdev

import (
	"errors"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

func TestBuildFinalResults_MissingPackageInfo(t *testing.T) {
	const purl = "pkg:pypi/example@2.0.0"
	missingRelease := errors.New("release lookup failed")
	tests := []struct {
		name           string
		releaseInfo    *ReleaseInfo
		wantEntry      bool
		wantError      string
		wantMaxSemver  string
		wantStable     string
		wantRequested  string
		wantPreRelease string
	}{
		{
			name: "release info with version",
			releaseInfo: &ReleaseInfo{
				MaxSemverVersion: Version{VersionKey: VersionKey{Version: "3.0.0"}},
				StableVersion:    Version{VersionKey: VersionKey{Version: "2.5.0"}},
			},
			wantMaxSemver: "3.0.0",
			wantStable:    "2.5.0",
			wantEntry:     true,
		},
		{
			name: "requested version only",
			releaseInfo: &ReleaseInfo{
				RequestedVersion: Version{VersionKey: VersionKey{Version: "2.0.0"}},
			},
			wantEntry:     true,
			wantRequested: "2.0.0",
		},
		{
			name: "pre-release version only",
			releaseInfo: &ReleaseInfo{
				PreReleaseVersion: Version{VersionKey: VersionKey{Version: "3.0.0-rc.1"}},
			},
			wantEntry:      true,
			wantPreRelease: "3.0.0-rc.1",
		},
		{
			name: "release info with error",
			releaseInfo: &ReleaseInfo{
				MaxSemverVersion: Version{VersionKey: VersionKey{Version: "3.0.0"}},
				Error:            missingRelease,
			},
			wantError: "package not found in deps.dev",
			wantEntry: true,
		},
		{
			name:      "no release info",
			wantError: "package not found in deps.dev",
			wantEntry: true,
		},
		{
			name:        "release info with no version",
			releaseInfo: &ReleaseInfo{Endpoint: "https://api.deps.dev/v3alpha/systems/pypi/packages/example"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "http://localhost"})
			releaseInfoMap := make(map[string]ReleaseInfo)
			if tt.releaseInfo != nil {
				releaseInfoMap[purl] = *tt.releaseInfo
			}

			results := c.buildFinalResults(
				[]string{purl},
				map[string]*PackageResponse{},
				nil,
				map[string][]string{},
				map[string]*Project{},
				releaseInfoMap,
			)

			result, ok := results[purl]
			if ok != tt.wantEntry {
				t.Fatalf("result entry for %q = %v, want %v", purl, ok, tt.wantEntry)
			}
			wantLen := 0
			if tt.wantEntry {
				wantLen = 1
			}
			if len(results) != wantLen {
				t.Errorf("len(results) = %d, want %d", len(results), wantLen)
			}
			if !tt.wantEntry {
				return
			}
			if result.PURL != purl {
				t.Errorf("PURL = %q, want %q", result.PURL, purl)
			}
			if result.Package != nil {
				t.Errorf("Package = %+v, want nil", result.Package)
			}
			if tt.wantError == "" {
				if result.Error != nil {
					t.Errorf("Error = %q, want nil", *result.Error)
				}
			} else if result.Error == nil {
				t.Errorf("Error = nil, want %q", tt.wantError)
			} else if *result.Error != tt.wantError {
				t.Errorf("Error = %q, want %q", *result.Error, tt.wantError)
			}
			if got := result.ReleaseInfo.MaxSemverVersion.VersionKey.Version; got != tt.wantMaxSemver {
				t.Errorf("MaxSemverVersion = %q, want %q", got, tt.wantMaxSemver)
			}
			if got := result.ReleaseInfo.StableVersion.VersionKey.Version; got != tt.wantStable {
				t.Errorf("StableVersion = %q, want %q", got, tt.wantStable)
			}
			if got := result.ReleaseInfo.RequestedVersion.VersionKey.Version; got != tt.wantRequested {
				t.Errorf("RequestedVersion = %q, want %q", got, tt.wantRequested)
			}
			if got := result.ReleaseInfo.PreReleaseVersion.VersionKey.Version; got != tt.wantPreRelease {
				t.Errorf("PreReleaseVersion = %q, want %q", got, tt.wantPreRelease)
			}
		})
	}
}

// releaseInfoStableAndDevEmptyMaxPopulated builds the exact ReleaseInfo shape
// that the pre-ADR-0023 gate (`Stable != "" || PreRelease != ""`) dropped
// entirely: both Stable and PreRelease are empty (the PyPI bound excluded
// every deps.dev version, and every excluded version was a keyword-free
// final release, so none landed in the Dev bucket either — see
// TestPickStableDevAndMax_Hint_AllCandidatesKeywordFreeFinal_LeavesStableAndDevEmpty
// in selection_test.go), while MaxSemverVersion stays populated.
func releaseInfoStableAndDevEmptyMaxPopulated() ReleaseInfo {
	return ReleaseInfo{
		Endpoint:         "https://api.deps.dev/v3alpha/systems/pypi/packages/example",
		MaxSemverVersion: Version{VersionKey: VersionKey{Version: "3.0.0"}},
	}
}

// TestBuildFinalResults_StableAndDevEmpty_MaxPopulated_ReleaseInfoAttached is
// the regression guard for issue #490's gate fix: when Stable and Dev are
// both empty but MaxSemverVersion is populated, buildFinalResults must still
// attach ReleaseInfo to the result. This test fails if the gate in
// buildFinalResults reverts from releaseInfo.HasAnyVersion() to the old
// `releaseInfo.StableVersion.VersionKey.Version != "" ||
// releaseInfo.PreReleaseVersion.VersionKey.Version != ""` check.
func TestBuildFinalResults_StableAndDevEmpty_MaxPopulated_ReleaseInfoAttached(t *testing.T) {
	c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "http://localhost"})

	const purl = "pkg:pypi/example@2.0.0"
	releaseInfo := releaseInfoStableAndDevEmptyMaxPopulated()

	results := c.buildFinalResults(
		[]string{purl},
		map[string]*PackageResponse{purl: {Version: Version{PURL: purl, VersionKey: VersionKey{System: "pypi", Name: "example", Version: "2.0.0"}}}},
		[]string{purl}, // purlsWithoutRepo
		map[string][]string{},
		map[string]*Project{},
		map[string]ReleaseInfo{purl: releaseInfo},
	)

	res, ok := results[purl]
	if !ok {
		t.Fatalf("no result for %q", purl)
	}
	if res.ReleaseInfo.Endpoint == "" {
		t.Fatalf("ReleaseInfo was dropped, want it attached (Stable/Dev empty but MaxSemverVersion populated)")
	}
	if res.ReleaseInfo.StableVersion.VersionKey.Version != "" {
		t.Fatalf("StableVersion=%s, want empty", res.ReleaseInfo.StableVersion.VersionKey.Version)
	}
	if res.ReleaseInfo.PreReleaseVersion.VersionKey.Version != "" {
		t.Fatalf("PreReleaseVersion=%s, want empty", res.ReleaseInfo.PreReleaseVersion.VersionKey.Version)
	}
	if res.ReleaseInfo.MaxSemverVersion.VersionKey.Version != "3.0.0" {
		t.Fatalf("MaxSemverVersion=%s, want 3.0.0", res.ReleaseInfo.MaxSemverVersion.VersionKey.Version)
	}
}

// TestBuildCompleteResult_StableAndDevEmpty_MaxPopulated_ReleaseInfoAttached
// is buildCompleteResult's counterpart to the buildFinalResults regression
// guard above, covering the repository-info-present code path.
func TestBuildCompleteResult_StableAndDevEmpty_MaxPopulated_ReleaseInfoAttached(t *testing.T) {
	c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "http://localhost"})

	const purl = "pkg:pypi/example@2.0.0"
	releaseInfo := releaseInfoStableAndDevEmptyMaxPopulated()
	packageResp := &PackageResponse{Version: Version{PURL: purl, VersionKey: VersionKey{System: "pypi", Name: "example", Version: "2.0.0"}}}

	res := c.buildCompleteResult(purl, packageResp, nil, releaseInfo)

	if res.ReleaseInfo.Endpoint == "" {
		t.Fatalf("ReleaseInfo was dropped, want it attached (Stable/Dev empty but MaxSemverVersion populated)")
	}
	if res.ReleaseInfo.StableVersion.VersionKey.Version != "" {
		t.Fatalf("StableVersion=%s, want empty", res.ReleaseInfo.StableVersion.VersionKey.Version)
	}
	if res.ReleaseInfo.PreReleaseVersion.VersionKey.Version != "" {
		t.Fatalf("PreReleaseVersion=%s, want empty", res.ReleaseInfo.PreReleaseVersion.VersionKey.Version)
	}
	if res.ReleaseInfo.MaxSemverVersion.VersionKey.Version != "3.0.0" {
		t.Fatalf("MaxSemverVersion=%s, want 3.0.0", res.ReleaseInfo.MaxSemverVersion.VersionKey.Version)
	}
}

// TestBuildCompleteResult_AllVersionSlotsEmpty_ReleaseInfoNotAttached is the
// contrasting case: when none of the four version slots is populated,
// ReleaseInfo must NOT be attached — HasAnyVersion's false path still gates
// correctly.
func TestBuildCompleteResult_AllVersionSlotsEmpty_ReleaseInfoNotAttached(t *testing.T) {
	c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "http://localhost"})

	const purl = "pkg:pypi/example@2.0.0"
	packageResp := &PackageResponse{Version: Version{PURL: purl, VersionKey: VersionKey{System: "pypi", Name: "example", Version: "2.0.0"}}}

	res := c.buildCompleteResult(purl, packageResp, nil, ReleaseInfo{Endpoint: "https://api.deps.dev/v3alpha/systems/pypi/packages/example"})

	if res.ReleaseInfo.Endpoint != "" {
		t.Fatalf("ReleaseInfo.Endpoint = %q, want empty (ReleaseInfo not attached: all version slots empty)", res.ReleaseInfo.Endpoint)
	}
	if res.ReleaseInfo.StableVersion.VersionKey.Version != "" || res.ReleaseInfo.PreReleaseVersion.VersionKey.Version != "" ||
		res.ReleaseInfo.MaxSemverVersion.VersionKey.Version != "" || res.ReleaseInfo.RequestedVersion.VersionKey.Version != "" {
		t.Fatalf("ReleaseInfo = %+v, want zero value (all version slots empty)", res.ReleaseInfo)
	}
}
