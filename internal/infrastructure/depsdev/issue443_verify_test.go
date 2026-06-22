package depsdev

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// TestBuildBasicResultNilPackageResp covers buildBasicResult being called with a
// nil *PackageResponse. Before the guard, dereferencing packageResp.Version would
// panic; the guard now leaves Package nil, mirroring the sibling buildCompleteResult.
// The nil input is defensive: the only current caller (buildFinalResults over
// purlsWithoutRepo) always has a packageInfoMap entry, but the guard protects any
// future caller that passes a nil packageResp.
func TestBuildBasicResultNilPackageResp(t *testing.T) {
	c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "http://localhost"})

	res := c.buildBasicResult("pkg:npm/left-pad@1.3.0", nil)
	if res == nil {
		t.Fatal("buildBasicResult returned nil")
	}
	if res.PURL != "pkg:npm/left-pad@1.3.0" {
		t.Errorf("PURL = %q, want pkg:npm/left-pad@1.3.0", res.PURL)
	}
	if res.Package != nil {
		t.Errorf("Package = %+v, want nil for missing package info", res.Package)
	}
}

// TestBuildBasicResultPopulatesPackage confirms the non-nil path is unchanged:
// a present *PackageResponse still yields a populated Package.
func TestBuildBasicResultPopulatesPackage(t *testing.T) {
	c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "http://localhost"})

	pkg := &PackageResponse{Version: Version{
		PURL:       "pkg:npm/left-pad@1.3.0",
		VersionKey: VersionKey{System: "npm", Name: "left-pad", Version: "1.3.0"},
	}}
	res := c.buildBasicResult("pkg:npm/left-pad@1.3.0", pkg)
	if res.Package == nil {
		t.Fatal("Package = nil, want populated")
	}
	if res.Package.PURL != "pkg:npm/left-pad@1.3.0" {
		t.Errorf("Package.PURL = %q, want pkg:npm/left-pad@1.3.0", res.Package.PURL)
	}
	if len(res.Package.Versions) != 1 {
		t.Errorf("len(Versions) = %d, want 1", len(res.Package.Versions))
	}
}

// TestBuildFinalResultsNoRepoNoPackageInfo exercises buildFinalResults' integrated
// assembly for a PURL in purlsWithoutRepo whose packageInfoMap entry is missing, so
// buildBasicResult receives a nil packageResp. The state is constructed directly (it
// is not produced by resolveRepoURLsBatch, which only emits purlsWithoutRepo entries
// that have a packageInfoMap key) to lock in the guard behavior end-to-end.
func TestBuildFinalResultsNoRepoNoPackageInfo(t *testing.T) {
	c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "http://localhost"})

	purl := "pkg:golang/example.com/missing@v0.0.0"
	results := c.buildFinalResults(
		[]string{purl},
		map[string]*PackageResponse{}, // packageInfoMap miss -> nil packageResp
		[]string{purl},                // purlsWithoutRepo
		map[string][]string{},
		map[string]*Project{},
		map[string]ReleaseInfo{},
	)

	res, ok := results[purl]
	if !ok {
		t.Fatalf("no result for %q", purl)
	}
	if res.PURL != purl {
		t.Errorf("PURL = %q, want %q", res.PURL, purl)
	}
	if res.Package != nil {
		t.Errorf("Package = %+v, want nil", res.Package)
	}
	// The PURL is handled by the purlsWithoutRepo branch, not the "mark not
	// found" branch, so Error must stay nil — pins which branch produced it.
	if res.Error != nil {
		t.Errorf("Error = %q, want nil", *res.Error)
	}
}
