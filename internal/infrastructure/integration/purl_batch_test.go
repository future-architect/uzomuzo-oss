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

// stubDepsDevClient implements depsdev.Client for testing enrichDependentCounts.
type stubDepsDevClient struct {
	dependentResults map[string]*depsdev.DependentsResponse
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

func TestEnrichDependentCounts_Phase1(t *testing.T) {
	tests := []struct {
		name           string
		purls          []string
		analyses       map[string]*domain.Analysis
		stubResults    map[string]*depsdev.DependentsResponse
		wantCounts     map[string]int
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
