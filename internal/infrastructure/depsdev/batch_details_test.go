package depsdev

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

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
