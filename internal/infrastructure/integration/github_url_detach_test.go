package integration

import (
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// TestDetachPackageIdentity pins that no package-scoped fact survives the
// fallback to a GitHub-only analysis. RegistryState is the one that matters most
// here: a stale "all releases yanked" would label a repository Review Needed on
// the strength of a package the caller never asked about.
func TestDetachPackageIdentity(t *testing.T) {
	t.Parallel()
	const githubURL = "https://github.com/owner/repo"
	a := &domain.Analysis{
		OriginalPURL:  "pkg:pypi/ghost",
		EffectivePURL: "pkg:pypi/ghost",
		Package:       &domain.Package{PURL: "pkg:pypi/ghost", Ecosystem: "pypi"},
		ReleaseInfo:   &domain.ReleaseInfo{StableVersion: &domain.VersionDetail{Version: "1.0.0"}},
		RegistryState: &domain.RegistryState{
			AllReleasesYanked: true,
			Registry:          domain.RegistryPyPI,
			Reason:            "Unmaintained",
		},
		RepoState: &domain.RepoState{},
		RepoURL:   githubURL,
	}

	detachPackageIdentity(a, githubURL)

	if a.Package != nil {
		t.Errorf("Package = %+v, want nil", a.Package)
	}
	if a.ReleaseInfo != nil {
		t.Errorf("ReleaseInfo = %+v, want nil", a.ReleaseInfo)
	}
	if a.RegistryState != nil {
		t.Errorf("RegistryState = %+v, want nil", a.RegistryState)
	}
	if a.AllReleasesYanked() {
		t.Error("AllReleasesYanked() = true, want false")
	}
	if a.OriginalPURL != githubURL || a.EffectivePURL != githubURL {
		t.Errorf("PURLs = %q / %q, want both %q", a.OriginalPURL, a.EffectivePURL, githubURL)
	}
	if a.RepoState == nil {
		t.Error("RepoState was dropped; the repository facts must survive")
	}
}
