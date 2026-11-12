package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/hex"
)

// hexPlugBody is a hex.pm package response for "plug" whose latest stable
// release 1.5.0 is affected by one vulnerability listed under two aliased IDs,
// and not by a second one fixed in 1.4.0.
const hexPlugBody = `{"latest_stable_version":"1.5.0","security_advisories":[
	{"id":"EEF-CVE-2026-1","aliases":["CVE-2026-1","GHSA-aaaa-bbbb-cccc"],"summary":"eef","cvss_score":8.2,"affected":[">= 1.0.0 and < 2.0.0"]},
	{"id":"GHSA-aaaa-bbbb-cccc","aliases":["CVE-2026-1","EEF-CVE-2026-1"],"summary":"Header injection","cvss_score":8.2,"affected":[">= 1.0.0 and < 2.0.0"]},
	{"id":"GHSA-fixd-fixd-fixd","aliases":["CVE-2026-2"],"cvss_score":9.8,"affected":["< 1.4.0"]}]}`

// hexHandler answers GET /api/packages/plug with hexPlugBody and 404s the rest.
func hexHandler(calls *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		if r.URL.Path != "/api/packages/plug" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprintln(w, hexPlugBody)
	}
}

func newHexClient(srvURL string) *hex.Client {
	c := hex.NewClient()
	c.SetBaseURL(srvURL)
	return c
}

func TestEnrichHexAdvisories(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(hexHandler(nil))
	defer srv.Close()

	tests := []struct {
		name      string
		purl      string
		ecosystem string
		want      *domain.VersionDetail // nil: ReleaseInfo stays nil
	}{
		{
			name: "affected latest stable", purl: "pkg:hex/plug@1.2.0", ecosystem: "hex",
			want: &domain.VersionDetail{Version: "1.5.0", Advisories: []domain.Advisory{{
				ID: "GHSA-aaaa-bbbb-cccc", Source: "GHSA", URL: "https://github.com/advisories/GHSA-aaaa-bbbb-cccc",
				Title: "Header injection", CVSS3Score: 8.2, Severity: "HIGH",
			}}},
		},
		{name: "unknown package", purl: "pkg:hex/missing", ecosystem: "hex"},
		{name: "namespaced private package", purl: "pkg:hex/acme/plug", ecosystem: "hex"},
		{name: "other ecosystem", purl: "pkg:cargo/plug", ecosystem: "cargo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &IntegrationService{hexClient: newHexClient(srv.URL)}
			a := analysisFor(tt.purl, tt.ecosystem)
			s.enrichHexAdvisories(context.Background(), map[string]*domain.Analysis{"k": a})

			if tt.want == nil {
				if a.ReleaseInfo != nil {
					t.Fatalf("expected nil ReleaseInfo, got %+v", a.ReleaseInfo)
				}
				return
			}
			if a.ReleaseInfo == nil || a.ReleaseInfo.StableVersion == nil {
				t.Fatalf("expected StableVersion, got %+v", a.ReleaseInfo)
			}
			got := a.ReleaseInfo.StableVersion
			if got.Version != tt.want.Version || len(got.Advisories) != len(tt.want.Advisories) {
				t.Fatalf("StableVersion = %+v, want %+v", got, tt.want)
			}
			for i := range got.Advisories {
				if got.Advisories[i] != tt.want.Advisories[i] {
					t.Errorf("advisory[%d] = %+v, want %+v", i, got.Advisories[i], tt.want.Advisories[i])
				}
			}
		})
	}
}

// TestEnrichHexAdvisories_KeepsExistingStableVersion pins that a StableVersion
// already set by another source is not replaced.
func TestEnrichHexAdvisories_KeepsExistingStableVersion(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(hexHandler(nil))
	defer srv.Close()

	s := &IntegrationService{hexClient: newHexClient(srv.URL)}
	existing := &domain.VersionDetail{Version: "9.9.9"}
	a := analysisFor("pkg:hex/plug", "hex")
	a.ReleaseInfo = &domain.ReleaseInfo{StableVersion: existing}
	s.enrichHexAdvisories(context.Background(), map[string]*domain.Analysis{"k": a})

	if a.ReleaseInfo.StableVersion != existing || len(existing.Advisories) != 0 {
		t.Fatalf("existing StableVersion was modified: %+v", a.ReleaseInfo.StableVersion)
	}
}

// TestEnrichHexAdvisories_DeduplicatesByName pins one request per package name
// (Hex names are case-insensitive) and an advisory slice owned by each analysis,
// because later enrichments mutate advisories in place.
func TestEnrichHexAdvisories_DeduplicatesByName(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(hexHandler(&calls))
	defer srv.Close()

	s := &IntegrationService{hexClient: newHexClient(srv.URL)}
	analyses := map[string]*domain.Analysis{
		"a": analysisFor("pkg:hex/plug", "hex"),
		"b": analysisFor("pkg:hex/plug@1.0.0", "hex"),
		"c": analysisFor("pkg:hex/Plug", "hex"),
	}
	s.enrichHexAdvisories(context.Background(), analyses)

	for k, a := range analyses {
		if a.ReleaseInfo == nil || a.ReleaseInfo.StableVersion == nil || len(a.ReleaseInfo.StableVersion.Advisories) != 1 {
			t.Fatalf("%s: expected one advisory, got %+v", k, a.ReleaseInfo)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("http calls: got %d, want 1", got)
	}
	analyses["a"].ReleaseInfo.StableVersion.Advisories[0].Relation = domain.AdvisoryRelationDirect
	if analyses["b"].ReleaseInfo.StableVersion.Advisories[0].Relation != "" {
		t.Error("expected each analysis to own its advisory slice")
	}
}

func TestEnrichHexAdvisories_FetchFailureLeavesReleaseInfoNil(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	s := &IntegrationService{hexClient: newHexClient(srv.URL)}
	a := analysisFor("pkg:hex/plug", "hex")
	s.enrichHexAdvisories(context.Background(), map[string]*domain.Analysis{"k": a})

	if a.ReleaseInfo != nil {
		t.Fatalf("expected nil ReleaseInfo after a failed fetch, got %+v", a.ReleaseInfo)
	}
}

func TestEnrichHexAdvisories_UnwiredClient(t *testing.T) {
	t.Parallel()
	s := &IntegrationService{}
	a := analysisFor("pkg:hex/plug", "hex")
	s.enrichHexAdvisories(context.Background(), map[string]*domain.Analysis{"k": a})
	if a.ReleaseInfo != nil {
		t.Fatalf("expected nil ReleaseInfo without a client, got %+v", a.ReleaseInfo)
	}
}

// TestAnalyzeFromPURLs_PopulatesHexAdvisories drives the production path so that
// deleting the enrichHexAdvisories call in purl_batch.go fails a test. deps.dev
// returns a zero CVSS3 score for the GHSA ID, as it does for Hex advisories, so
// running the enrichment before enrichAdvisorySeverity also fails it.
func TestAnalyzeFromPURLs_PopulatesHexAdvisories(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(hexHandler(nil))
	defer srv.Close()

	dd := &stubDepsDevClient{advisoryDetails: map[string]*depsdev.AdvisoryDetail{
		"GHSA-aaaa-bbbb-cccc": {Title: "Header injection", CVSS3Score: 0},
	}}
	// A tokenless GitHub client short-circuits the repository-state fetch without
	// any network call; a nil one would panic inside enhanceAnalysesWithGitHubBatch.
	svc := NewIntegrationService(github.NewClient(&config.Config{}), dd, WithHexClient(newHexClient(srv.URL)))

	analyses, err := svc.AnalyzeFromPURLs(context.Background(), []string{"pkg:hex/plug@1.2.0"})
	if err != nil {
		t.Fatalf("AnalyzeFromPURLs failed: %v", err)
	}
	a := analyses["pkg:hex/plug@1.2.0"]
	if a == nil || a.ReleaseInfo == nil {
		t.Fatalf("expected ReleaseInfo through the production path, got %+v", a)
	}
	vd := a.ReleaseInfo.LatestVersionDetail()
	if vd == nil || len(vd.Advisories) != 1 {
		t.Fatalf("expected one advisory on the latest version, got %+v", vd)
	}
	if adv := vd.Advisories[0]; adv.CVSS3Score != 8.2 || adv.Severity != "HIGH" {
		t.Fatalf("hex.pm score lost: %+v", adv)
	}
}
