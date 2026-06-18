package depsdev

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// TestBuildBasicResultNilPackageResp covers the case where buildBasicResult is
// called with a nil *PackageResponse, which happens in buildFinalResults when
// packageInfoMap[purl] misses (the key is absent for PURLs whose package info
// could not be fetched). Before the nil guard, dereferencing packageResp.Version
// panicked. The result should now carry no Package, matching buildCompleteResult.
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

// TestBuildFinalResultsNoRepoNoPackageInfo exercises the integrated path that
// triggered the panic: a PURL in purlsWithoutRepo with no entry in packageInfoMap.
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
	if res.Package != nil {
		t.Errorf("Package = %+v, want nil", res.Package)
	}
}
