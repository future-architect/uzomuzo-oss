package integration

import (
	"context"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
)

// identityDepsDevClient serves one BatchResult so AnalyzeFromPURLs reaches its
// success path without network access. It embeds stubDepsDevClient (purl_batch_test.go)
// for the methods this test does not exercise.
type identityDepsDevClient struct {
	stubDepsDevClient
	details map[string]*depsdev.BatchResult
}

func (c *identityDepsDevClient) GetDetailsForPURLs(_ context.Context, _ []string) (map[string]*depsdev.BatchResult, error) {
	return c.details, nil
}

// TestFetchAndValidateGitHubAnalysis_IdentitySplit pins the PURL identity contract
// on the GitHub URL entry path: a version uzomuzo selected on the caller's behalf
// lands in EffectivePURL / Package.PURL, while OriginalPURL keeps the unversioned
// base the caller's GitHub URL maps to.
//
// Version-specific rules (the yank rules) read OriginalPURL, so leaking the
// synthesized version into it makes them treat uzomuzo's own choice as a caller
// pin. See ADR-0021 and the identity model in domain/analysis/aggregates.go.
func TestFetchAndValidateGitHubAnalysis_IdentitySplit(t *testing.T) {
	const (
		githubURL     = "https://github.com/pydantic/pydantic-extra-types"
		basePURL      = "pkg:pypi/pydantic-extra-types"
		versionedPURL = "pkg:pypi/pydantic-extra-types@2.11.2"
	)

	tests := []struct {
		name string
		// analyzePURL is the coordinate Step 4 hands down: the synthesized
		// versioned PURL, or basePURL when deps.dev offered no stable version.
		analyzePURL       string
		wantOriginalPURL  string
		wantEffectivePURL string
		wantPackagePURL   string
	}{
		{
			name:              "synthesized version stays out of OriginalPURL",
			analyzePURL:       versionedPURL,
			wantOriginalPURL:  basePURL,
			wantEffectivePURL: versionedPURL,
			wantPackagePURL:   versionedPURL,
		},
		{
			name:              "no stable version: every identity is the base PURL",
			analyzePURL:       basePURL,
			wantOriginalPURL:  basePURL,
			wantEffectivePURL: basePURL,
			wantPackagePURL:   basePURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dd := &identityDepsDevClient{
				details: map[string]*depsdev.BatchResult{
					tt.analyzePURL: {
						PURL:    tt.analyzePURL,
						Package: &depsdev.Package{},
						// The package must link back to the input repository, or
						// the round-trip check discards it (see #539).
						RepoURL: githubURL,
					},
				},
			}
			// Empty-token client: FetchRepositoryStates fills defaults without
			// touching the network.
			s := NewIntegrationService(github.NewClient(&config.Config{}), dd)

			got, err := s.fetchAndValidateGitHubAnalysis(context.Background(), tt.analyzePURL, basePURL, githubURL)
			if err != nil {
				t.Fatalf("fetchAndValidateGitHubAnalysis failed: %v", err)
			}
			if got == nil {
				t.Fatal("fetchAndValidateGitHubAnalysis returned nil analysis")
			}

			if got.OriginalPURL != tt.wantOriginalPURL {
				t.Errorf("OriginalPURL = %q, want %q", got.OriginalPURL, tt.wantOriginalPURL)
			}
			if got.EffectivePURL != tt.wantEffectivePURL {
				t.Errorf("EffectivePURL = %q, want %q", got.EffectivePURL, tt.wantEffectivePURL)
			}
			if got.Package == nil {
				t.Fatal("Package is nil")
			}
			if got.Package.PURL != tt.wantPackagePURL {
				t.Errorf("Package.PURL = %q, want %q", got.Package.PURL, tt.wantPackagePURL)
			}
			// CanonicalKey is versionless, so it is the same either way — assert it
			// so a future change to OriginalPURL cannot silently move the dedup key.
			if got.CanonicalKey != basePURL {
				t.Errorf("CanonicalKey = %q, want %q", got.CanonicalKey, basePURL)
			}
		})
	}
}

// TestFetchAndValidateGitHubAnalysis_NoRepoLinkFallsBackToGitHubOnly pins #539:
// github.com/grafana/grafana resolved to pkg:npm/grafana, an unrelated package with
// no repository link, and the repository was labeled from that package's 2018
// release. A package that does not link back to the input repository must be
// discarded in favor of the GitHub-only analysis.
func TestFetchAndValidateGitHubAnalysis_NoRepoLinkFallsBackToGitHubOnly(t *testing.T) {
	t.Parallel()

	const (
		githubURL     = "https://github.com/grafana/grafana"
		basePURL      = "pkg:npm/grafana"
		versionedPURL = "pkg:npm/grafana@1.0.1"
	)

	dd := &identityDepsDevClient{
		details: map[string]*depsdev.BatchResult{
			versionedPURL: {
				PURL:    versionedPURL,
				Package: &depsdev.Package{},
			},
		},
	}
	s := NewIntegrationService(github.NewClient(&config.Config{}), dd)

	got, err := s.fetchAndValidateGitHubAnalysis(context.Background(), versionedPURL, basePURL, githubURL)
	if err != nil {
		t.Fatalf("fetchAndValidateGitHubAnalysis failed: %v", err)
	}
	if got.Package != nil {
		t.Errorf("Package = %+v, want nil (unrelated package must be discarded)", got.Package)
	}
	if got.OriginalPURL != githubURL {
		t.Errorf("OriginalPURL = %q, want %q", got.OriginalPURL, githubURL)
	}
	if got.RepoURL != githubURL {
		t.Errorf("RepoURL = %q, want %q", got.RepoURL, githubURL)
	}
	want := domain.UnmatchedPackage{PURL: versionedPURL}
	if got.UnmatchedPackage == nil || *got.UnmatchedPackage != want {
		t.Errorf("UnmatchedPackage = %+v, want %+v", got.UnmatchedPackage, want)
	}
}
