package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/packagist"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/rubygems"
)

func TestResolvedVersion(t *testing.T) {
	tests := []struct {
		name     string
		analysis *domain.Analysis
		want     string
	}{
		{
			name:     "package version set",
			analysis: &domain.Analysis{Package: &domain.Package{Version: "1.2.3"}},
			want:     "1.2.3",
		},
		{
			name: "fallback to stable version",
			analysis: &domain.Analysis{
				Package:     &domain.Package{},
				ReleaseInfo: &domain.ReleaseInfo{StableVersion: &domain.VersionDetail{Version: "2.0.0"}},
			},
			want: "2.0.0",
		},
		{
			name: "fallback to max semver version",
			analysis: &domain.Analysis{
				Package:     &domain.Package{},
				ReleaseInfo: &domain.ReleaseInfo{MaxSemverVersion: &domain.VersionDetail{Version: "3.0.0-rc1"}},
			},
			want: "3.0.0-rc1",
		},
		{
			name: "package version takes precedence over release info",
			analysis: &domain.Analysis{
				Package:     &domain.Package{Version: "1.0.0"},
				ReleaseInfo: &domain.ReleaseInfo{StableVersion: &domain.VersionDetail{Version: "2.0.0"}},
			},
			want: "1.0.0",
		},
		{
			name:     "nil package and nil release info",
			analysis: &domain.Analysis{},
			want:     "",
		},
		{
			name:     "whitespace-only package version",
			analysis: &domain.Analysis{Package: &domain.Package{Version: "  "}},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvedVersion(tt.analysis)
			if got != tt.want {
				t.Errorf("resolvedVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

// stubDepsDevClient implements depsdev.Client for testing enrichment functions.
type stubDepsDevClient struct {
	dependentResults          map[string]*depsdev.DependentsResponse
	dependenciesResults       map[string]*depsdev.DependenciesResponse
	transitiveAdvisoryResults map[string][]depsdev.AdvisoryKey
	advisoryDetails           map[string]*depsdev.AdvisoryDetail
}

func (s *stubDepsDevClient) GetDetailsForPURLs(_ context.Context, _ []string) (map[string]*depsdev.BatchResult, error) {
	return nil, nil
}

func (s *stubDepsDevClient) GetLatestReleasesForPURLs(_ context.Context, _ []string) (map[string]*depsdev.ReleaseInfo, error) {
	return nil, nil
}

func (s *stubDepsDevClient) GetPackageVersionLicenses(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (s *stubDepsDevClient) FetchDependentCountBatch(_ context.Context, _ []string) map[string]*depsdev.DependentsResponse {
	if s.dependentResults == nil {
		return make(map[string]*depsdev.DependentsResponse)
	}
	return s.dependentResults
}

func (s *stubDepsDevClient) FetchDependenciesBatch(_ context.Context, _ []string) map[string]*depsdev.DependenciesResponse {
	if s.dependenciesResults == nil {
		return make(map[string]*depsdev.DependenciesResponse)
	}
	return s.dependenciesResults
}

func (s *stubDepsDevClient) FetchAdvisoriesBatch(_ context.Context, _ []string) map[string]*depsdev.AdvisoryDetail {
	if s.advisoryDetails != nil {
		return s.advisoryDetails
	}
	return make(map[string]*depsdev.AdvisoryDetail)
}

func (s *stubDepsDevClient) FetchTransitiveAdvisoryKeys(_ context.Context, deps *depsdev.DependenciesResponse) (map[string][]depsdev.AdvisoryKey, error) {
	if s.transitiveAdvisoryResults == nil {
		return make(map[string][]depsdev.AdvisoryKey), nil
	}
	return s.transitiveAdvisoryResults, nil
}

func TestEnrichDependentCounts_Phase1(t *testing.T) {
	tests := []struct {
		name        string
		purls       []string
		analyses    map[string]*domain.Analysis
		stubResults map[string]*depsdev.DependentsResponse
		wantCounts  map[string]int
	}{
		{
			name:  "versioned PURL matches canonical key",
			purls: []string{"pkg:npm/express@4.18.2"},
			analyses: map[string]*domain.Analysis{
				"pkg:npm/express@4.18.2": {
					EffectivePURL: "pkg:npm/express@4.18.2",
					Package:       &domain.Package{Ecosystem: "npm", Version: "4.18.2"},
				},
			},
			stubResults: map[string]*depsdev.DependentsResponse{
				"pkg:npm/express": {DependentCount: 5000},
			},
			wantCounts: map[string]int{
				"pkg:npm/express@4.18.2": 5000,
			},
		},
		{
			name:  "versionless PURL resolved via release info",
			purls: []string{"pkg:cargo/serde"},
			analyses: map[string]*domain.Analysis{
				"pkg:cargo/serde": {
					EffectivePURL: "pkg:cargo/serde",
					Package:       &domain.Package{Ecosystem: "cargo"},
					ReleaseInfo:   &domain.ReleaseInfo{StableVersion: &domain.VersionDetail{Version: "1.0.200"}},
				},
			},
			stubResults: map[string]*depsdev.DependentsResponse{
				"pkg:cargo/serde": {DependentCount: 10000},
			},
			wantCounts: map[string]int{
				"pkg:cargo/serde": 10000,
			},
		},
		{
			name:  "no match in deps.dev results leaves count at zero",
			purls: []string{"pkg:pypi/unknown"},
			analyses: map[string]*domain.Analysis{
				"pkg:pypi/unknown": {
					EffectivePURL: "pkg:pypi/unknown",
					Package:       &domain.Package{Ecosystem: "pypi"},
				},
			},
			stubResults: map[string]*depsdev.DependentsResponse{},
			wantCounts: map[string]int{
				"pkg:pypi/unknown": 0,
			},
		},
		{
			name:  "PURL with qualifiers handled safely",
			purls: []string{"pkg:maven/org.slf4j/slf4j-api?type=jar"},
			analyses: map[string]*domain.Analysis{
				"pkg:maven/org.slf4j/slf4j-api?type=jar": {
					EffectivePURL: "pkg:maven/org.slf4j/slf4j-api?type=jar",
					Package:       &domain.Package{Ecosystem: "maven"},
					ReleaseInfo:   &domain.ReleaseInfo{StableVersion: &domain.VersionDetail{Version: "2.0.16"}},
				},
			},
			stubResults: map[string]*depsdev.DependentsResponse{
				// CanonicalKey preserves qualifiers, so the key includes ?type=jar
				"pkg:maven/org.slf4j/slf4j-api?type=jar": {DependentCount: 8000},
			},
			wantCounts: map[string]int{
				"pkg:maven/org.slf4j/slf4j-api?type=jar": 8000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &IntegrationService{
				depsdevClient: &stubDepsDevClient{dependentResults: tt.stubResults},
			}
			svc.enrichDependentCounts(context.Background(), tt.purls, tt.analyses)
			for purl, wantCount := range tt.wantCounts {
				a := tt.analyses[purl]
				if a == nil {
					t.Fatalf("analysis not found for %s", purl)
				}
				if a.DependentCount != wantCount {
					t.Errorf("DependentCount for %s = %d, want %d", purl, a.DependentCount, wantCount)
				}
			}
		})
	}
}

func TestEnrichDependentCounts_Phase2_RubyGems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/gems/rails/reverse_dependencies.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `["dep1","dep2","dep3"]`) // test helper
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	rgClient := rubygems.NewClient()
	rgClient.SetBaseURL(srv.URL)

	svc := &IntegrationService{
		depsdevClient:  &stubDepsDevClient{},
		rubygemsClient: rgClient,
	}
	purls := []string{"pkg:gem/rails@7.0.4"}
	analyses := map[string]*domain.Analysis{
		"pkg:gem/rails@7.0.4": {
			EffectivePURL: "pkg:gem/rails@7.0.4",
			Package:       &domain.Package{Ecosystem: "gem", Version: "7.0.4"},
		},
	}
	svc.enrichDependentCounts(context.Background(), purls, analyses)

	a := analyses["pkg:gem/rails@7.0.4"]
	if a.DependentCount != 3 {
		t.Errorf("DependentCount = %d, want 3", a.DependentCount)
	}
}

func TestEnrichDependentCounts_Phase2_Packagist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/packages/monolog/monolog.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"package":{"name":"monolog/monolog","dependents":4200,"versions":{}}}`) // test helper
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	pkgClient := packagist.NewClient()
	pkgClient.SetBaseURL(srv.URL)

	svc := &IntegrationService{
		depsdevClient:   &stubDepsDevClient{},
		packagistClient: pkgClient,
	}
	purls := []string{"pkg:composer/monolog/monolog@3.0"}
	analyses := map[string]*domain.Analysis{
		"pkg:composer/monolog/monolog@3.0": {
			EffectivePURL: "pkg:composer/monolog/monolog@3.0",
			Package:       &domain.Package{Ecosystem: "composer", Version: "3.0"},
		},
	}
	svc.enrichDependentCounts(context.Background(), purls, analyses)

	a := analyses["pkg:composer/monolog/monolog@3.0"]
	if a.DependentCount != 4200 {
		t.Errorf("DependentCount = %d, want 4200", a.DependentCount)
	}
}

func TestEnrichDependentCounts_Phase2_SkipsWhenDepsDevPopulated(t *testing.T) {
	// When Phase 1 already populated DependentCount, Phase 2 should skip.
	svc := &IntegrationService{
		depsdevClient:  &stubDepsDevClient{dependentResults: map[string]*depsdev.DependentsResponse{"pkg:gem/rails": {DependentCount: 999}}},
		rubygemsClient: rubygems.NewClient(), // should not be called
	}
	purls := []string{"pkg:gem/rails@7.0.4"}
	analyses := map[string]*domain.Analysis{
		"pkg:gem/rails@7.0.4": {
			EffectivePURL: "pkg:gem/rails@7.0.4",
			Package:       &domain.Package{Ecosystem: "gem", Version: "7.0.4"},
		},
	}
	svc.enrichDependentCounts(context.Background(), purls, analyses)

	a := analyses["pkg:gem/rails@7.0.4"]
	if a.DependentCount != 999 {
		t.Errorf("DependentCount = %d, want 999 (from Phase 1)", a.DependentCount)
	}
}

func TestLatestReleaseVersion(t *testing.T) {
	tests := []struct {
		name     string
		analysis *domain.Analysis
		want     string
	}{
		{
			name:     "stable version preferred",
			analysis: &domain.Analysis{ReleaseInfo: &domain.ReleaseInfo{StableVersion: &domain.VersionDetail{Version: "2.0.0"}, PreReleaseVersion: &domain.VersionDetail{Version: "3.0.0-rc1"}}},
			want:     "2.0.0",
		},
		{
			name:     "fallback to prerelease",
			analysis: &domain.Analysis{ReleaseInfo: &domain.ReleaseInfo{PreReleaseVersion: &domain.VersionDetail{Version: "1.0.0-beta.1"}}},
			want:     "1.0.0-beta.1",
		},
		{
			name:     "nil release info",
			analysis: &domain.Analysis{},
			want:     "",
		},
		{
			name:     "empty versions",
			analysis: &domain.Analysis{ReleaseInfo: &domain.ReleaseInfo{StableVersion: &domain.VersionDetail{Version: ""}}},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := latestReleaseVersion(tt.analysis)
			if got != tt.want {
				t.Errorf("latestReleaseVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnrichDependencyCounts(t *testing.T) {
	tests := []struct {
		name                   string
		purls                  []string
		analyses               map[string]*domain.Analysis
		stubResults            map[string]*depsdev.DependenciesResponse
		wantDirect             map[string]int
		wantTransitive         map[string]int
		wantHasDependencyGraph map[string]bool
	}{
		{
			name:  "versioned PURL populates counts",
			purls: []string{"pkg:npm/express@4.21.2"},
			analyses: map[string]*domain.Analysis{
				"pkg:npm/express@4.21.2": {
					EffectivePURL: "pkg:npm/express@4.21.2",
					Package:       &domain.Package{Ecosystem: "npm", Version: "4.21.2"},
				},
			},
			stubResults: map[string]*depsdev.DependenciesResponse{
				"pkg:npm/express": {
					Nodes: []depsdev.DependencyNode{
						{Relation: "SELF"},
						{Relation: "DIRECT"},
						{Relation: "DIRECT"},
						{Relation: "INDIRECT"},
						{Relation: "INDIRECT"},
						{Relation: "INDIRECT"},
					},
				},
			},
			wantDirect:             map[string]int{"pkg:npm/express@4.21.2": 2},
			wantTransitive:         map[string]int{"pkg:npm/express@4.21.2": 3},
			wantHasDependencyGraph: map[string]bool{"pkg:npm/express@4.21.2": true},
		},
		{
			name:  "versionless PURL resolved via stable release",
			purls: []string{"pkg:cargo/serde"},
			analyses: map[string]*domain.Analysis{
				"pkg:cargo/serde": {
					EffectivePURL: "pkg:cargo/serde",
					Package:       &domain.Package{Ecosystem: "cargo"},
					ReleaseInfo:   &domain.ReleaseInfo{StableVersion: &domain.VersionDetail{Version: "1.0.200"}},
				},
			},
			stubResults: map[string]*depsdev.DependenciesResponse{
				"pkg:cargo/serde": {
					Nodes: []depsdev.DependencyNode{
						{Relation: "SELF"},
						{Relation: "DIRECT"},
					},
				},
			},
			wantDirect:             map[string]int{"pkg:cargo/serde": 1},
			wantTransitive:         map[string]int{"pkg:cargo/serde": 0},
			wantHasDependencyGraph: map[string]bool{"pkg:cargo/serde": true},
		},
		{
			name:  "versionless PURL falls back to prerelease",
			purls: []string{"pkg:npm/beta-only"},
			analyses: map[string]*domain.Analysis{
				"pkg:npm/beta-only": {
					EffectivePURL: "pkg:npm/beta-only",
					Package:       &domain.Package{Ecosystem: "npm"},
					ReleaseInfo:   &domain.ReleaseInfo{PreReleaseVersion: &domain.VersionDetail{Version: "0.1.0-beta.1"}},
				},
			},
			stubResults: map[string]*depsdev.DependenciesResponse{
				"pkg:npm/beta-only": {
					Nodes: []depsdev.DependencyNode{
						{Relation: "SELF"},
						{Relation: "DIRECT"},
						{Relation: "INDIRECT"},
					},
				},
			},
			wantDirect:             map[string]int{"pkg:npm/beta-only": 1},
			wantTransitive:         map[string]int{"pkg:npm/beta-only": 1},
			wantHasDependencyGraph: map[string]bool{"pkg:npm/beta-only": true},
		},
		{
			name:  "leaf package with zero deps still marks graph as present",
			purls: []string{"pkg:npm/react@19.1.0"},
			analyses: map[string]*domain.Analysis{
				"pkg:npm/react@19.1.0": {
					EffectivePURL: "pkg:npm/react@19.1.0",
					Package:       &domain.Package{Ecosystem: "npm", Version: "19.1.0"},
				},
			},
			stubResults: map[string]*depsdev.DependenciesResponse{
				// Genuine leaf: deps.dev returned a response, but only the SELF node.
				"pkg:npm/react": {Nodes: []depsdev.DependencyNode{{Relation: "SELF"}}},
			},
			wantDirect:             map[string]int{"pkg:npm/react@19.1.0": 0},
			wantTransitive:         map[string]int{"pkg:npm/react@19.1.0": 0},
			wantHasDependencyGraph: map[string]bool{"pkg:npm/react@19.1.0": true},
		},
		{
			name:  "no match leaves counts at zero and graph absent",
			purls: []string{"pkg:pypi/unknown@1.0.0"},
			analyses: map[string]*domain.Analysis{
				"pkg:pypi/unknown@1.0.0": {
					EffectivePURL: "pkg:pypi/unknown@1.0.0",
					Package:       &domain.Package{Ecosystem: "pypi", Version: "1.0.0"},
				},
			},
			stubResults:            map[string]*depsdev.DependenciesResponse{},
			wantDirect:             map[string]int{"pkg:pypi/unknown@1.0.0": 0},
			wantTransitive:         map[string]int{"pkg:pypi/unknown@1.0.0": 0},
			wantHasDependencyGraph: map[string]bool{"pkg:pypi/unknown@1.0.0": false},
		},
		{
			name:  "versionless with no resolvable release marks graph absent",
			purls: []string{"pkg:pypi/unknown"},
			analyses: map[string]*domain.Analysis{
				"pkg:pypi/unknown": {
					EffectivePURL: "pkg:pypi/unknown",
					Package:       &domain.Package{Ecosystem: "pypi"},
					// no ReleaseInfo -> latestReleaseVersion returns ""
				},
			},
			// stub has data, but a versionless PURL must never reach the batch call.
			stubResults: map[string]*depsdev.DependenciesResponse{
				"pkg:pypi/unknown": {Nodes: []depsdev.DependencyNode{{Relation: "DIRECT"}}},
			},
			wantDirect:             map[string]int{"pkg:pypi/unknown": 0},
			wantTransitive:         map[string]int{"pkg:pypi/unknown": 0},
			wantHasDependencyGraph: map[string]bool{"pkg:pypi/unknown": false},
		},
		{
			name:  "unsupported ecosystem skipped without API call",
			purls: []string{"pkg:golang/github.com/gin-gonic/gin@v1.10.0"},
			analyses: map[string]*domain.Analysis{
				"pkg:golang/github.com/gin-gonic/gin@v1.10.0": {
					EffectivePURL: "pkg:golang/github.com/gin-gonic/gin@v1.10.0",
					Package:       &domain.Package{Ecosystem: "golang", Version: "v1.10.0"},
				},
			},
			// stub has data, but it should never be consulted for golang
			stubResults: map[string]*depsdev.DependenciesResponse{
				"pkg:golang/github.com/gin-gonic/gin": {
					Nodes: []depsdev.DependencyNode{{Relation: "DIRECT"}},
				},
			},
			wantDirect:             map[string]int{"pkg:golang/github.com/gin-gonic/gin@v1.10.0": 0},
			wantTransitive:         map[string]int{"pkg:golang/github.com/gin-gonic/gin@v1.10.0": 0},
			wantHasDependencyGraph: map[string]bool{"pkg:golang/github.com/gin-gonic/gin@v1.10.0": false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &IntegrationService{
				depsdevClient: &stubDepsDevClient{dependenciesResults: tt.stubResults},
			}
			svc.enrichDependencyCounts(context.Background(), tt.purls, tt.analyses)
			for purl, wantDirect := range tt.wantDirect {
				a := tt.analyses[purl]
				if a == nil {
					t.Fatalf("analysis not found for %s", purl)
				}
				if a.DirectDepsCount != wantDirect {
					t.Errorf("DirectDepsCount for %s = %d, want %d", purl, a.DirectDepsCount, wantDirect)
				}
			}
			for purl, wantTransitive := range tt.wantTransitive {
				a := tt.analyses[purl]
				if a == nil {
					t.Fatalf("analysis not found for %s", purl)
				}
				if a.TransitiveDepsCount != wantTransitive {
					t.Errorf("TransitiveDepsCount for %s = %d, want %d", purl, a.TransitiveDepsCount, wantTransitive)
				}
			}
			for purl, wantHas := range tt.wantHasDependencyGraph {
				a := tt.analyses[purl]
				if a == nil {
					t.Fatalf("analysis not found for %s", purl)
				}
				if a.HasDependencyGraph != wantHas {
					t.Errorf("HasDependencyGraph for %s = %v, want %v", purl, a.HasDependencyGraph, wantHas)
				}
			}
		})
	}
}
