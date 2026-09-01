package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
)

// entryPathDepsDevClient records which PURL each deps.dev call received and serves
// a stable version so AnalyzeFromGitHubURL takes its Step 3 synthesize branch.
type entryPathDepsDevClient struct {
	stubDepsDevClient
	stableVersion string

	releasesFor []string
	detailsFor  []string
}

func (c *entryPathDepsDevClient) GetLatestReleasesForPURLs(_ context.Context, purls []string) (map[string]*depsdev.ReleaseInfo, error) {
	c.releasesFor = append(c.releasesFor, purls...)
	out := make(map[string]*depsdev.ReleaseInfo, len(purls))
	for _, p := range purls {
		ri := &depsdev.ReleaseInfo{}
		ri.StableVersion.VersionKey.Version = c.stableVersion
		out[p] = ri
	}
	return out, nil
}

func (c *entryPathDepsDevClient) GetDetailsForPURLs(_ context.Context, purls []string) (map[string]*depsdev.BatchResult, error) {
	c.detailsFor = append(c.detailsFor, purls...)
	out := make(map[string]*depsdev.BatchResult, len(purls))
	for _, p := range purls {
		out[p] = &depsdev.BatchResult{PURL: p, Package: &depsdev.Package{}}
	}
	return out, nil
}

// TestAnalyzeFromGitHubURL_EntryPathIdentity drives the real entry point rather
// than the helper, so it fails if AnalyzeFromGitHubURL ever hands the synthesized
// versioned PURL to fetchAndValidateGitHubAnalysis as *both* arguments — the exact
// wiring mistake the helper-level test cannot see.
//
// The GitHub GraphQL endpoint is redirected to an httptest server, so the test
// stays hermetic. See ADR-0021.
func TestAnalyzeFromGitHubURL_EntryPathIdentity(t *testing.T) {
	t.Parallel()

	const (
		githubURL     = "https://github.com/pydantic/pydantic-extra-types"
		basePURL      = "pkg:pypi/pydantic-extra-types"
		stableVersion = "2.11.2"
		versionedPURL = basePURL + "@" + stableVersion
	)

	// One manifest declaring a PIP dependency is enough for githubURLToPURL to
	// derive the pypi base PURL from the repository name.
	const graphQLBody = `{"data":{"repository":{` +
		`"isArchived":false,"isDisabled":false,"isFork":false,` +
		`"stargazerCount":330,"forkCount":10,"description":"Extra Pydantic types.",` +
		`"dependencyGraphManifests":{"nodes":[{"filename":"requirements.txt",` +
		`"dependencies":{"nodes":[{"packageManager":"PIP","packageName":"pydantic"}]}}]}` +
		`},"rateLimit":{"cost":1,"remaining":4999,"resetAt":""}}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(graphQLBody))
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.GitHub.Token = "test-token" // non-empty so the GraphQL path is taken
	cfg.GitHub.BaseURL = srv.URL
	cfg.GitHub.Timeout = 10 * time.Second

	dd := &entryPathDepsDevClient{stableVersion: stableVersion}
	s := NewIntegrationService(github.NewClient(cfg), dd)

	got, err := s.AnalyzeFromGitHubURL(context.Background(), githubURL)
	if err != nil {
		t.Fatalf("AnalyzeFromGitHubURL failed: %v", err)
	}
	if got == nil {
		t.Fatal("AnalyzeFromGitHubURL returned nil analysis")
	}

	// Guard: if deps.dev was never asked for the versioned coordinate, Step 3 did
	// not synthesize a version and the assertions below would pass vacuously.
	var sawVersioned bool
	for _, p := range dd.detailsFor {
		if p == versionedPURL {
			sawVersioned = true
		}
	}
	if !sawVersioned {
		t.Fatalf("Step 3 did not synthesize %s; details requested for %v", versionedPURL, dd.detailsFor)
	}

	if got.OriginalPURL != basePURL {
		t.Errorf("OriginalPURL = %q, want %q (the caller pinned no version)", got.OriginalPURL, basePURL)
	}
	if got.EffectivePURL != versionedPURL {
		t.Errorf("EffectivePURL = %q, want %q", got.EffectivePURL, versionedPURL)
	}
	if got.Package == nil {
		t.Fatal("Package is nil")
	}
	if got.Package.PURL != versionedPURL {
		t.Errorf("Package.PURL = %q, want %q", got.Package.PURL, versionedPURL)
	}
	if got.DisplayPURL() != basePURL {
		t.Errorf("DisplayPURL() = %q, want %q", got.DisplayPURL(), basePURL)
	}
	if got.CanonicalKey != basePURL {
		t.Errorf("CanonicalKey = %q, want %q", got.CanonicalKey, basePURL)
	}
}
