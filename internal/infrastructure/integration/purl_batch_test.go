package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
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
	mu                        sync.Mutex
	dependentCalls            []string
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

func (s *stubDepsDevClient) FetchDependentCountBatch(_ context.Context, purls []string) map[string]*depsdev.DependentsResponse {
	s.mu.Lock()
	s.dependentCalls = append(s.dependentCalls, purls...)
	s.mu.Unlock()
	if s.dependentResults == nil {
		return make(map[string]*depsdev.DependentsResponse)
	}
	return s.dependentResults
}

func TestEnrichDependentCounts_VersionedObservations(t *testing.T) {
	client := &stubDepsDevClient{dependentResults: map[string]*depsdev.DependentsResponse{
		"pkg:npm/express@4.18.2": {DependentCount: 7, DirectDependentCount: 5},
		"pkg:npm/express@5.0.0":  {DependentCount: 0, DirectDependentCount: 0},
	}}
	inputs := []string{"pkg:npm/express@4.18.2", "pkg:npm/express@5.0.0", "pkg:npm/express"}
	analyses := map[string]*domain.Analysis{
		inputs[0]: {EffectivePURL: inputs[0], Package: &domain.Package{Ecosystem: "npm", Version: "4.18.2"}},
		inputs[1]: {EffectivePURL: inputs[1], Package: &domain.Package{Ecosystem: "npm", Version: "5.0.0"}},
		inputs[2]: {EffectivePURL: inputs[0], Package: &domain.Package{Ecosystem: "npm"}},
	}
	(&IntegrationService{depsdevClient: client}).enrichDependentCounts(context.Background(), inputs, analyses)
	if !slices.Equal(client.dependentCalls, []string{inputs[0], inputs[1]}) {
		t.Fatalf("queried %v, want each exact effective PURL once", client.dependentCalls)
	}
	for _, input := range []string{inputs[0], inputs[2]} {
		a := analyses[input]
		if a.DependentCount != 7 || a.Dependents == nil || a.Dependents.Version != "4.18.2" || a.Dependents.Source != domain.DependentsSourceDepsDev || a.Dependents.Direct == nil || *a.Dependents.Direct != 5 || a.Dependents.Total == nil || *a.Dependents.Total != 7 {
			t.Errorf("%s: count=%d observation=%+v", input, a.DependentCount, a.Dependents)
		}
	}
	a := analyses[inputs[1]]
	if a.DependentCount != 0 || a.Dependents == nil || a.Dependents.Total == nil || *a.Dependents.Total != 0 {
		t.Errorf("measured zero: count=%d observation=%+v", a.DependentCount, a.Dependents)
	}
}

func TestEnrichDependentCounts_SelectionAndUnknown(t *testing.T) {
	client := &stubDepsDevClient{dependentResults: map[string]*depsdev.DependentsResponse{
		"pkg:cargo/serde@1.0.210": {DependentCount: 42, DirectDependentCount: 30},
	}}
	inputs := []string{"pkg:cargo/serde", "pkg:npm/express@4.18.2", "pkg:pypi/missing", "pkg:golang/github.com/pkg/errors@v0.9.1"}
	analyses := map[string]*domain.Analysis{
		inputs[0]: {EffectivePURL: inputs[0], Package: &domain.Package{Ecosystem: "cargo"}, ReleaseInfo: &domain.ReleaseInfo{StableVersion: &domain.VersionDetail{Version: "1.0.210"}}},
		inputs[1]: {EffectivePURL: inputs[1], Package: &domain.Package{Ecosystem: "npm", Version: "4.18.2"}},
		inputs[2]: {EffectivePURL: inputs[2], Package: &domain.Package{Ecosystem: "pypi"}, ReleaseInfo: &domain.ReleaseInfo{MaxSemverVersion: &domain.VersionDetail{Version: "2.0.0"}}},
		inputs[3]: {EffectivePURL: inputs[3], Package: &domain.Package{Ecosystem: "golang", Version: "v0.9.1"}},
	}
	(&IntegrationService{depsdevClient: client}).enrichDependentCounts(context.Background(), inputs, analyses)
	if !slices.Equal(client.dependentCalls, []string{"pkg:cargo/serde@1.0.210", inputs[1], "pkg:pypi/missing@2.0.0"}) {
		t.Fatalf("queried %v", client.dependentCalls)
	}
	if a := analyses[inputs[0]]; a.DependentCount != 42 || a.Dependents == nil || a.Dependents.Version != "1.0.210" {
		t.Fatalf("stable selection: %+v", a)
	}
	for _, input := range inputs[1:] {
		if a := analyses[input]; a.DependentCount != 0 || a.Dependents != nil {
			t.Errorf("%s should be unknown: %+v", input, a)
		}
	}
}

func TestEnrichDependentCounts_HTTPOutcomes(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.URL.Path)
		mu.Unlock()
		switch {
		case strings.Contains(r.URL.Path, "/zero/"):
			_, _ = w.Write([]byte(`{"dependentCount":0,"directDependentCount":0,"indirectDependentCount":0}`))
		case strings.Contains(r.URL.Path, "/missing/"):
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	client := depsdev.NewDepsDevClient(&config.DepsDevConfig{BaseURL: srv.URL, Timeout: 5e9, MaxRetries: 0})
	inputs := []string{"pkg:npm/zero@1.0.0", "pkg:npm/missing@1.0.0", "pkg:npm/error@1.0.0"}
	analyses := map[string]*domain.Analysis{}
	for _, input := range inputs {
		analyses[input] = &domain.Analysis{EffectivePURL: input, Package: &domain.Package{Ecosystem: "npm", Version: "1.0.0"}}
	}
	(&IntegrationService{depsdevClient: client}).enrichDependentCounts(context.Background(), inputs, analyses)
	if a := analyses[inputs[0]]; a.Dependents == nil || a.Dependents.Direct == nil || *a.Dependents.Direct != 0 || a.Dependents.Total == nil || *a.Dependents.Total != 0 || a.DependentCount != 0 {
		t.Errorf("measured zero: %+v", a)
	}
	for _, input := range inputs[1:] {
		if a := analyses[input]; a.Dependents != nil || a.DependentCount != 0 {
			t.Errorf("%s: %+v", input, a)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 3 {
		t.Errorf("HTTP calls = %v, want one for each versioned input", calls)
	}
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
				"pkg:npm/express@4.18.2": {DependentCount: 5000},
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
				"pkg:cargo/serde@1.0.200": {DependentCount: 10000},
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
				"pkg:maven/org.slf4j/slf4j-api@2.0.16?type=jar": {DependentCount: 8000},
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
	if a.Dependents == nil || a.Dependents.Source != domain.DependentsSourceRubyGems || a.Dependents.Version != "" || a.Dependents.Direct == nil || *a.Dependents.Direct != 3 || a.Dependents.Total != nil {
		t.Errorf("RubyGems observation = %+v", a.Dependents)
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
	if a.Dependents == nil || a.Dependents.Source != domain.DependentsSourcePackagist || a.Dependents.Version != "" || a.Dependents.Direct == nil || *a.Dependents.Direct != 4200 || a.Dependents.Total != nil {
		t.Errorf("Packagist observation = %+v", a.Dependents)
	}
}

func TestEnrichDependentCounts_RegistryNotFoundDoesNotRetainDepsDevZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer srv.Close()
	rg := rubygems.NewClient()
	rg.SetBaseURL(srv.URL)
	pk := packagist.NewClient()
	pk.SetBaseURL(srv.URL)
	inputs := []string{"pkg:gem/missing@1.0.0", "pkg:composer/missing/pkg@1.0.0"}
	analyses := map[string]*domain.Analysis{
		inputs[0]: {EffectivePURL: inputs[0], Package: &domain.Package{Ecosystem: "gem", Version: "1.0.0"}},
		inputs[1]: {EffectivePURL: inputs[1], Package: &domain.Package{Ecosystem: "composer", Version: "1.0.0"}},
	}
	client := &stubDepsDevClient{dependentResults: map[string]*depsdev.DependentsResponse{
		inputs[0]: {DependentCount: 0}, inputs[1]: {DependentCount: 0},
	}}
	(&IntegrationService{depsdevClient: client, rubygemsClient: rg, packagistClient: pk}).enrichDependentCounts(context.Background(), inputs, analyses)
	for _, input := range inputs {
		a := analyses[input]
		if a.Dependents != nil || a.DependentCount != 0 {
			t.Errorf("%s: count=%d observation=%+v, want unknown", input, a.DependentCount, a.Dependents)
		}
	}
}

func TestEnrichDependentCounts_Phase2_SkipsWhenDepsDevPopulated(t *testing.T) {
	var registryCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		registryCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	rg := rubygems.NewClient()
	rg.SetBaseURL(srv.URL)
	svc := &IntegrationService{
		depsdevClient:  &stubDepsDevClient{dependentResults: map[string]*depsdev.DependentsResponse{"pkg:gem/rails@7.0.4": {DependentCount: 999, DirectDependentCount: 123}}},
		rubygemsClient: rg,
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
	if a.Dependents == nil {
		t.Fatal("deps.dev observation was discarded")
	}
	if a.Dependents.Source != domain.DependentsSourceDepsDev || a.Dependents.Version != "7.0.4" || a.Dependents.Direct == nil || *a.Dependents.Direct != 123 || a.Dependents.Total == nil || *a.Dependents.Total != 999 {
		t.Errorf("deps.dev observation = %+v", a.Dependents)
	}
	if registryCalls.Load() != 0 {
		t.Errorf("RubyGems calls = %d, want 0", registryCalls.Load())
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
			name:     "max semver preferred over prerelease when stable missing",
			analysis: &domain.Analysis{ReleaseInfo: &domain.ReleaseInfo{MaxSemverVersion: &domain.VersionDetail{Version: "4.5.0"}, PreReleaseVersion: &domain.VersionDetail{Version: "1.0.0-beta.1"}}},
			want:     "4.5.0",
		},
		{
			name:     "fallback to prerelease when no stable or max semver",
			analysis: &domain.Analysis{ReleaseInfo: &domain.ReleaseInfo{PreReleaseVersion: &domain.VersionDetail{Version: "1.0.0-beta.1"}}},
			want:     "1.0.0-beta.1",
		},
		{
			name:     "requested version not used (pinned-version semantics belong to resolvedVersion)",
			analysis: &domain.Analysis{ReleaseInfo: &domain.ReleaseInfo{RequestedVersion: &domain.VersionDetail{Version: "0.9.0"}}},
			want:     "",
		},
		{
			name:     "package version not used (pinned-version semantics belong to resolvedVersion)",
			analysis: &domain.Analysis{Package: &domain.Package{Version: "7.2.1"}},
			want:     "",
		},
		{
			name:     "nil analysis",
			analysis: nil,
			want:     "",
		},
		{
			name:     "nil release info",
			analysis: &domain.Analysis{},
			want:     "",
		},
		{
			name:     "empty stable version",
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
		wantGraphVersion       map[string]string
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
				"pkg:npm/express@4.21.2": {
					Nodes: []depsdev.DependencyNode{
						{Relation: "SELF", VersionKey: depsdev.DependencyVersionKey{Version: "4.21.2"}},
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
			wantGraphVersion:       map[string]string{"pkg:npm/express@4.21.2": "4.21.2"},
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
				"pkg:cargo/serde@1.0.200": {
					Nodes: []depsdev.DependencyNode{
						{Relation: "SELF", VersionKey: depsdev.DependencyVersionKey{Version: "1.0.200"}},
						{Relation: "DIRECT"},
					},
				},
			},
			wantDirect:             map[string]int{"pkg:cargo/serde": 1},
			wantTransitive:         map[string]int{"pkg:cargo/serde": 0},
			wantHasDependencyGraph: map[string]bool{"pkg:cargo/serde": true},
			wantGraphVersion:       map[string]string{"pkg:cargo/serde": "1.0.200"},
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
				"pkg:npm/beta-only@0.1.0-beta.1": {
					Nodes: []depsdev.DependencyNode{
						{Relation: "SELF", VersionKey: depsdev.DependencyVersionKey{Version: "0.1.0-beta.1"}},
						{Relation: "DIRECT"},
						{Relation: "INDIRECT"},
					},
				},
			},
			wantDirect:             map[string]int{"pkg:npm/beta-only": 1},
			wantTransitive:         map[string]int{"pkg:npm/beta-only": 1},
			wantHasDependencyGraph: map[string]bool{"pkg:npm/beta-only": true},
			wantGraphVersion:       map[string]string{"pkg:npm/beta-only": "0.1.0-beta.1"},
		},
		{
			name:  "versionless PURL falls back to max semver when stable missing",
			purls: []string{"pkg:pypi/django"},
			analyses: map[string]*domain.Analysis{
				"pkg:pypi/django": {
					EffectivePURL: "pkg:pypi/django",
					Package:       &domain.Package{Ecosystem: "pypi"},
					// Upstream release metadata missing stable but has MaxSemver —
					// the deps query should still succeed using the semver fallback.
					ReleaseInfo: &domain.ReleaseInfo{MaxSemverVersion: &domain.VersionDetail{Version: "5.1.0"}},
				},
			},
			stubResults: map[string]*depsdev.DependenciesResponse{
				"pkg:pypi/django@5.1.0": {
					Nodes: []depsdev.DependencyNode{
						{Relation: "SELF", VersionKey: depsdev.DependencyVersionKey{Version: "5.1.0"}},
						{Relation: "DIRECT"},
						{Relation: "DIRECT"},
					},
				},
			},
			wantDirect:             map[string]int{"pkg:pypi/django": 2},
			wantTransitive:         map[string]int{"pkg:pypi/django": 0},
			wantHasDependencyGraph: map[string]bool{"pkg:pypi/django": true},
			wantGraphVersion:       map[string]string{"pkg:pypi/django": "5.1.0"},
		},
		{
			name:  "nil response from deps.dev marks graph absent",
			purls: []string{"pkg:cargo/serde@1.0.210"},
			analyses: map[string]*domain.Analysis{
				"pkg:cargo/serde@1.0.210": {
					EffectivePURL: "pkg:cargo/serde@1.0.210",
					Package:       &domain.Package{Ecosystem: "cargo", Version: "1.0.210"},
				},
			},
			// Explicit nil entry exercises the resp == nil branch distinct from "key missing".
			stubResults: map[string]*depsdev.DependenciesResponse{
				"pkg:cargo/serde@1.0.210": nil,
			},
			wantDirect:             map[string]int{"pkg:cargo/serde@1.0.210": 0},
			wantTransitive:         map[string]int{"pkg:cargo/serde@1.0.210": 0},
			wantHasDependencyGraph: map[string]bool{"pkg:cargo/serde@1.0.210": false},
			wantGraphVersion:       map[string]string{"pkg:cargo/serde@1.0.210": ""},
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
				"pkg:npm/react@19.1.0": {Nodes: []depsdev.DependencyNode{{Relation: "SELF", VersionKey: depsdev.DependencyVersionKey{Version: "19.1.0"}}}},
			},
			wantDirect:             map[string]int{"pkg:npm/react@19.1.0": 0},
			wantTransitive:         map[string]int{"pkg:npm/react@19.1.0": 0},
			wantHasDependencyGraph: map[string]bool{"pkg:npm/react@19.1.0": true},
			wantGraphVersion:       map[string]string{"pkg:npm/react@19.1.0": "19.1.0"},
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
			wantGraphVersion:       map[string]string{"pkg:pypi/unknown@1.0.0": ""},
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
			// stub has data, but the unresolved versionless PURL is filtered out before lookup;
			// FetchDependenciesBatch may still be invoked with an empty slice.
			stubResults: map[string]*depsdev.DependenciesResponse{
				"pkg:pypi/unknown": {Nodes: []depsdev.DependencyNode{{Relation: "DIRECT"}}},
			},
			wantDirect:             map[string]int{"pkg:pypi/unknown": 0},
			wantTransitive:         map[string]int{"pkg:pypi/unknown": 0},
			wantHasDependencyGraph: map[string]bool{"pkg:pypi/unknown": false},
			wantGraphVersion:       map[string]string{"pkg:pypi/unknown": ""},
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
			wantGraphVersion:       map[string]string{"pkg:golang/github.com/gin-gonic/gin@v1.10.0": ""},
		},
		{
			name:  "fallback graph is labelled with the version it describes",
			purls: []string{"pkg:npm/react@19.2.5"},
			analyses: map[string]*domain.Analysis{
				"pkg:npm/react@19.2.5": {
					EffectivePURL: "pkg:npm/react@19.2.5",
					Package:       &domain.Package{Ecosystem: "npm", Version: "19.2.5"},
				},
			},
			stubResults: map[string]*depsdev.DependenciesResponse{
				"pkg:npm/react@19.2.5": {Nodes: []depsdev.DependencyNode{{Relation: "SELF", VersionKey: depsdev.DependencyVersionKey{Version: "19.1.0"}}}},
			},
			wantDirect:             map[string]int{"pkg:npm/react@19.2.5": 0},
			wantTransitive:         map[string]int{"pkg:npm/react@19.2.5": 0},
			wantHasDependencyGraph: map[string]bool{"pkg:npm/react@19.2.5": true},
			wantGraphVersion:       map[string]string{"pkg:npm/react@19.2.5": "19.1.0"},
		},
		{
			name:  "two versions of one package keep their own graphs",
			purls: []string{"pkg:npm/example@1.0.0", "pkg:npm/example@2.0.0"},
			analyses: map[string]*domain.Analysis{
				"pkg:npm/example@1.0.0": {EffectivePURL: "pkg:npm/example@1.0.0", Package: &domain.Package{Ecosystem: "npm", Version: "1.0.0"}},
				"pkg:npm/example@2.0.0": {EffectivePURL: "pkg:npm/example@2.0.0", Package: &domain.Package{Ecosystem: "npm", Version: "2.0.0"}},
			},
			stubResults: map[string]*depsdev.DependenciesResponse{
				"pkg:npm/example@1.0.0": {Nodes: []depsdev.DependencyNode{{Relation: "SELF", VersionKey: depsdev.DependencyVersionKey{Version: "1.0.0"}}, {Relation: "DIRECT"}}},
				"pkg:npm/example@2.0.0": {Nodes: []depsdev.DependencyNode{{Relation: "SELF", VersionKey: depsdev.DependencyVersionKey{Version: "2.0.0"}}, {Relation: "DIRECT"}, {Relation: "DIRECT"}, {Relation: "INDIRECT"}}},
			},
			wantDirect:             map[string]int{"pkg:npm/example@1.0.0": 1, "pkg:npm/example@2.0.0": 2},
			wantTransitive:         map[string]int{"pkg:npm/example@1.0.0": 0, "pkg:npm/example@2.0.0": 1},
			wantHasDependencyGraph: map[string]bool{"pkg:npm/example@1.0.0": true, "pkg:npm/example@2.0.0": true},
			wantGraphVersion:       map[string]string{"pkg:npm/example@1.0.0": "1.0.0", "pkg:npm/example@2.0.0": "2.0.0"},
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
			for purl, wantVersion := range tt.wantGraphVersion {
				a := tt.analyses[purl]
				if a == nil {
					t.Fatalf("analysis not found for %s", purl)
				}
				if a.DependencyGraphVersion != wantVersion {
					t.Errorf("DependencyGraphVersion for %s = %q, want %q", purl, a.DependencyGraphVersion, wantVersion)
				}
			}
		})
	}
}

func TestSetRegistryDependents_CopiesCount(t *testing.T) {
	count := 42
	a := &domain.Analysis{}
	setRegistryDependents(a, domain.DependentsSourcePackagist, &count)
	count = 1
	if a.Dependents == nil || a.Dependents.Direct == nil {
		t.Fatal("Dependents.Direct is nil")
	}
	if *a.Dependents.Direct != 42 {
		t.Errorf("Direct = %d, want 42 (must not alias the caller's value)", *a.Dependents.Direct)
	}
	if a.DependentCount != 42 {
		t.Errorf("DependentCount = %d, want 42", a.DependentCount)
	}
}

func TestEnrichDependentCounts_Phase2_DuplicatePURLQueriedOnce(t *testing.T) {
	var registryCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		registryCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"package":{"name":"monolog/monolog","dependents":8529}}`))
	}))
	defer srv.Close()
	pk := packagist.NewClient()
	pk.SetBaseURL(srv.URL)
	svc := &IntegrationService{depsdevClient: &stubDepsDevClient{}, packagistClient: pk}
	const p = "pkg:composer/monolog/monolog@3.0.0"
	purls := []string{p, p}
	analyses := map[string]*domain.Analysis{
		p: {EffectivePURL: p, Package: &domain.Package{Ecosystem: "composer", Version: "3.0.0"}},
	}
	svc.enrichDependentCounts(context.Background(), purls, analyses)

	if got := registryCalls.Load(); got != 1 {
		t.Errorf("registry calls = %d, want 1", got)
	}
	a := analyses[p]
	if a.Dependents == nil || a.Dependents.Direct == nil {
		t.Fatal("Packagist observation missing")
	}
	if *a.Dependents.Direct != 8529 || a.DependentCount != 8529 {
		t.Errorf("Direct=%d DependentCount=%d, want 8529", *a.Dependents.Direct, a.DependentCount)
	}
}
