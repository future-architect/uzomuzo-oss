package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/osv"
)

func TestAnalyzeFromGitHubURLsRetainsCVSS4Fallback(t *testing.T) {
	const githubURL = "https://github.com/example/withdrawn-test"
	const id = "GHSA-vcc3-ghjq-m6fr"
	const v3ID = "GHSA-v3-medium"
	const vector = "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:N/VI:N/VA:H/SC:N/SI:N/SA:N/E:U/S:N/AU:Y/R:U/V:D/RE:M/U:Amber"
	const graphQLBody = `{"data":{"repository":{"isArchived":false,"isDisabled":false,"isFork":false,"stargazerCount":1,"forkCount":1,"dependencyGraphManifests":{"nodes":[{"filename":"requirements.txt","dependencies":{"nodes":[{"packageManager":"PIP","packageName":"demo"}]}}]}},"rateLimit":{"cost":1,"remaining":4999,"resetAt":""}}}`
	var decoded github.GraphQLResponse
	if err := json.Unmarshal([]byte(graphQLBody), &decoded); err != nil || decoded.Data.RateLimit.Remaining != 4999 {
		t.Fatalf("invalid GitHub GraphQL fixture: remaining=%d err=%v", decoded.Data.RateLimit.Remaining, err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(graphQLBody)) }))
	defer srv.Close()
	cfg := &config.Config{}
	cfg.GitHub.Token, cfg.GitHub.BaseURL, cfg.GitHub.Timeout = "test-token", srv.URL, 10*time.Second
	var calls atomic.Int32
	oc := osv.NewClient()
	oc.SetCacheTTL(0)
	oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1/vulns/"+id {
			calls.Add(1)
		}
		body := `{"id":"` + id + `","severity":[{"type":"CVSS_V4","score":"` + vector + `"}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})})
	graph := &depsdev.DependenciesResponse{Nodes: []depsdev.DependencyNode{{VersionKey: depsdev.DependencyVersionKey{Name: "self", Version: "1.0.0"}, Relation: "SELF"}, {VersionKey: depsdev.DependencyVersionKey{Name: "dep", Version: "1.0.0"}, Relation: "DIRECT"}}}
	dd := &withdrawnGitHubDepsDev{stubDepsDevClient{dependenciesResults: map[string]*depsdev.DependenciesResponse{"pkg:pypi/withdrawn-test@1.0.0": graph}, advisoryDetails: map[string]*depsdev.AdvisoryDetail{id: {CVSS3Score: 0}, v3ID: {CVSS3Score: 5.0}}, transitiveAdvisoryResults: map[string][]depsdev.AdvisoryKey{"dep@1.0.0": {{ID: v3ID}, {ID: id}}}}}
	svc := NewIntegrationService(github.NewClient(cfg), dd)
	svc.osvClient = oc
	results, err := svc.AnalyzeFromGitHubURLs(context.Background(), []string{githubURL})
	if err != nil {
		t.Fatal(err)
	}
	a := results[githubURL]
	if a == nil || a.ReleaseInfo == nil || a.ReleaseInfo.StableVersion == nil || len(a.ReleaseInfo.StableVersion.Advisories) != 2 {
		t.Fatalf("analysis=%+v", a)
	}
	adv := a.ReleaseInfo.StableVersion.Advisories[0]
	if adv.ID != id || !adv.CVSS4Known || adv.CVSS4Score != 6.6 || adv.Severity != "MEDIUM" || a.ReleaseInfo.StableVersion.Advisories[1].ID != v3ID || calls.Load() != 1 {
		t.Fatalf("advisory=%+v OSV calls=%d", adv, calls.Load())
	}
}

func TestCVSS4FallbackKeepsOSVFactPriority(t *testing.T) {
	details := map[string]*depsdev.AdvisoryDetail{}
	missing := map[string]struct{}{"GHSA-zzz-missing": {}}
	for i := range 8 {
		details[fmt.Sprintf("GHSA-yyy-high-%d", i)] = &depsdev.AdvisoryDetail{CVSS3Score: 9.0}
		details[fmt.Sprintf("GHSA-aaa-v4-%d", i)] = &depsdev.AdvisoryDetail{CVSS3Score: 0}
	}
	oc := osv.NewClient()
	oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
		id := strings.TrimPrefix(r.URL.Path, "/v1/vulns/")
		if strings.Contains(id, "-v4-") {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"` + id + `"}`)), Header: make(http.Header), Request: r}, nil
	})})
	svc := &IntegrationService{osvClient: oc, osvFactsBudget: 200 * time.Millisecond}
	facts := svc.fetchDependencyAdvisoryOSVFacts(context.Background(), details, missing)
	for id := range missing {
		if facts[id].lookup != domain.AdvisoryRecordFound {
			t.Errorf("missing priority %s=%s", id, facts[id].lookup)
		}
	}
	for id, d := range details {
		want := domain.AdvisoryRecordFound
		if d.CVSS3Score == 0 {
			want = domain.AdvisoryRecordLookupFailed
		}
		if facts[id].lookup != want {
			t.Errorf("%s lookup=%s want %s", id, facts[id].lookup, want)
		}
	}
}

func TestDirectCVSS4FallbackKeepsWithdrawalPriority(t *testing.T) {
	const missingID = "GHSA-zzz-missing"
	var missingCalls atomic.Int32
	details := map[string]*depsdev.AdvisoryDetail{}
	vd := &domain.VersionDetail{Advisories: []domain.Advisory{{ID: missingID}}}
	for i := range 8 {
		id := fmt.Sprintf("GHSA-aaa-v4-%d", i)
		details[id] = &depsdev.AdvisoryDetail{CVSS3Score: 0}
		vd.Advisories = append(vd.Advisories, domain.Advisory{ID: id})
	}
	oc := osv.NewClient()
	oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
		id := strings.TrimPrefix(r.URL.Path, "/v1/vulns/")
		if id != missingID {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		missingCalls.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"` + id + `"}`)), Header: make(http.Header), Request: r}, nil
	})})
	svc := &IntegrationService{depsdevClient: &stubDepsDevClient{advisoryDetails: details, advisoryMissing: map[string]struct{}{missingID: {}}}, osvClient: oc, osvFactsBudget: 200 * time.Millisecond}
	svc.enrichAdvisorySeverity(context.Background(), map[string]*domain.Analysis{"p": {ReleaseInfo: &domain.ReleaseInfo{StableVersion: vd}}}, true)
	if missingCalls.Load() != 1 {
		t.Fatalf("withdrawal requests=%d", missingCalls.Load())
	}
	if len(vd.Advisories) != 9 {
		t.Fatalf("advisories=%d", len(vd.Advisories))
	}
	for _, adv := range vd.Advisories {
		if adv.ID != missingID && !adv.CVSS4LookupFailed {
			t.Errorf("v4 lookup was not marked failed: %+v", adv)
		}
	}
}

func TestEnrichCVSS4Fallback(t *testing.T) {
	const vcc3 = "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:N/VI:N/VA:H/SC:N/SI:N/SA:N/E:U/S:N/AU:Y/R:U/V:D/RE:M/U:Amber"
	const ph9p = "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:N/VA:N/SC:N/SI:N/SA:N/E:P"
	const synthetic = "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:N/VI:N/VA:N/SC:H/SI:H/SA:H"
	for _, tc := range []struct {
		name, id, vector, reviewed, band string
		score                            float64
		transitive                       bool
	}{
		{"direct GHSA-vcc3", "GHSA-vcc3-ghjq-m6fr", vcc3, "MODERATE", "MEDIUM", 6.6, false},
		{"dependency GHSA-ph9p", "GHSA-ph9p-34f9-6g65", ph9p, "HIGH", "HIGH", 7.7, true},
		{"non GHSA", "RUSTSEC-2026-9999", synthetic, "HIGH", "HIGH", 7.9, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oc := osv.NewClient()
			oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
				body := `{"id":"` + tc.id + `","published":"2020-01-01T00:00:00Z","database_specific":{"severity":"` + tc.reviewed + `"},"severity":[{"type":"CVSS_V4","score":"` + tc.vector + `"}]}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})})
			dd := &stubDepsDevClient{advisoryDetails: map[string]*depsdev.AdvisoryDetail{tc.id: {CVSS3Score: 0}}, transitiveAdvisoryResults: map[string][]depsdev.AdvisoryKey{"dep@1.0.0": {{ID: tc.id}}}}
			svc := &IntegrationService{depsdevClient: dd, osvClient: oc}
			vd := &domain.VersionDetail{Version: "1.0.0"}
			a := &domain.Analysis{ReleaseInfo: &domain.ReleaseInfo{StableVersion: vd}}
			if tc.transitive {
				graph := &depsdev.DependenciesResponse{Nodes: []depsdev.DependencyNode{{VersionKey: depsdev.DependencyVersionKey{Name: "self", Version: "1.0.0"}, Relation: "SELF"}, {VersionKey: depsdev.DependencyVersionKey{Name: "dep", Version: "1.0.0"}, Relation: "DIRECT"}}}
				svc.enrichTransitiveAdvisories(context.Background(), []string{"p"}, map[string]*domain.Analysis{"p": a}, map[string]*depsdev.DependenciesResponse{"p": graph}, nil)
			} else {
				vd.Advisories = []domain.Advisory{{ID: tc.id}}
				svc.enrichAdvisorySeverity(context.Background(), map[string]*domain.Analysis{"p": a}, true)
			}
			if len(vd.Advisories) != 1 {
				t.Fatalf("advisories=%v", vd.Advisories)
			}
			adv := vd.Advisories[0]
			if !adv.CVSS4Known || adv.CVSS4Score != tc.score || adv.Severity != tc.band {
				t.Fatalf("v4 advisory=%+v", adv)
			}
			if tc.transitive && (adv.RecordLookup != domain.AdvisoryRecordFound || !adv.Published.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) || adv.ReviewedSeverity != tc.reviewed) {
				t.Fatalf("age facts=%+v", adv)
			}
		})
	}
}

func TestCVSS4EffectiveScoreEnrichmentOrder(t *testing.T) {
	const v3ID, v4ID = "GHSA-v3-medium", "GHSA-v4-high"
	vd := &domain.VersionDetail{Advisories: []domain.Advisory{{ID: v3ID}, {ID: v4ID}}}
	details := map[string]*depsdev.AdvisoryDetail{v3ID: {CVSS3Score: 6.5}, v4ID: {CVSS3Score: 0}}
	facts := map[string]osvFact{v4ID: {rec: domain.AdvisoryRecord{CVSS4Score: 7.7, CVSS4Known: true}, lookup: domain.AdvisoryRecordFound}}
	enrichVersionAdvisories(vd, details, facts)
	if vd.Advisories[0].ID != v4ID || vd.Advisories[1].ID != v3ID {
		t.Fatalf("direct order=%v", vd.Advisories)
	}

	transitive := &domain.VersionDetail{Advisories: []domain.Advisory{{ID: v3ID, CVSS3Score: 6.5}}}
	appendTransitiveAdvisories(transitive, []domain.Advisory{{ID: v4ID, CVSS4Score: 7.7, CVSS4Known: true, Relation: domain.AdvisoryRelationTransitive}})
	if transitive.Advisories[0].ID != v4ID || transitive.Advisories[1].ID != v3ID {
		t.Fatalf("dependency order=%v", transitive.Advisories)
	}

	final := &domain.VersionDetail{Advisories: []domain.Advisory{{ID: v3ID, CVSS3Score: 6.5}, {ID: v4ID, CVSS4Score: 7.7, CVSS4Known: true}}}
	svc := &IntegrationService{depsdevClient: &stubDepsDevClient{advisoryDetails: details}}
	svc.enrichAdvisorySeverity(context.Background(), map[string]*domain.Analysis{"p": {ReleaseInfo: &domain.ReleaseInfo{StableVersion: final}}}, false)
	if final.Advisories[0].ID != v4ID || final.Advisories[1].ID != v3ID || !final.Advisories[0].CVSS4Known {
		t.Fatalf("GitHub URL final-pass order=%v", final.Advisories)
	}
}

func TestEnrichCVSS4UnknownAndFailure(t *testing.T) {
	const id = "GHSA-v4-fallback"
	const vector = "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:N/VI:N/VA:N/SC:H/SI:H/SA:H"
	for _, tc := range []struct {
		name   string
		status int
		body   string
		block  bool
		failed bool
	}{
		{"OSV 404", 404, "", false, false},
		{"no vector", 200, `{"id":"` + id + `"}`, false, false},
		{"malformed vector", 200, `{"id":"` + id + `","severity":{"bad":"shape"}}`, false, false},
		{"withdrawn", 200, `{"id":"` + id + `","withdrawn":"2026-01-01T00:00:00Z","severity":[{"type":"CVSS_V4","score":"` + vector + `"}]}`, false, false},
		{"OSV error", 400, "", false, true},
		{"budget", 0, "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oc := osv.NewClient()
			oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
				if tc.block {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header), Request: r}, nil
			})})
			svc := &IntegrationService{depsdevClient: &stubDepsDevClient{advisoryDetails: map[string]*depsdev.AdvisoryDetail{id: {CVSS3Score: 0}}}, osvClient: oc, osvFactsBudget: 25 * time.Millisecond}
			vd := &domain.VersionDetail{Advisories: []domain.Advisory{{ID: id}}}
			svc.enrichAdvisorySeverity(context.Background(), map[string]*domain.Analysis{"p": {ReleaseInfo: &domain.ReleaseInfo{StableVersion: vd}}}, true)
			adv := vd.Advisories[0]
			if adv.CVSS4Known || adv.Severity != "" || adv.CVSS4LookupFailed != tc.failed {
				t.Fatalf("advisory=%+v want failed=%v", adv, tc.failed)
			}
		})
	}
}

func TestTransitiveCVSS4LookupFailure(t *testing.T) {
	const id = "GHSA-v4-dependency"
	for _, tc := range []struct {
		name  string
		block bool
	}{{"OSV error", false}, {"budget exhausted", true}} {
		t.Run(tc.name, func(t *testing.T) {
			oc := osv.NewClient()
			oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
				if tc.block {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
			})})
			dd := &stubDepsDevClient{advisoryDetails: map[string]*depsdev.AdvisoryDetail{id: {CVSS3Score: 0}}, transitiveAdvisoryResults: map[string][]depsdev.AdvisoryKey{"dep@1.0.0": {{ID: id}}}}
			svc := &IntegrationService{depsdevClient: dd, osvClient: oc, osvFactsBudget: 25 * time.Millisecond}
			vd := &domain.VersionDetail{Version: "1.0.0"}
			a := &domain.Analysis{ReleaseInfo: &domain.ReleaseInfo{StableVersion: vd}}
			analyses := map[string]*domain.Analysis{"p": a}
			graph := &depsdev.DependenciesResponse{Nodes: []depsdev.DependencyNode{{VersionKey: depsdev.DependencyVersionKey{Name: "self", Version: "1.0.0"}, Relation: "SELF"}, {VersionKey: depsdev.DependencyVersionKey{Name: "dep", Version: "1.0.0"}, Relation: "DIRECT"}}}
			svc.enrichTransitiveAdvisories(context.Background(), []string{"p"}, analyses, map[string]*depsdev.DependenciesResponse{"p": graph}, nil)
			if len(vd.Advisories) != 1 {
				t.Fatalf("advisories=%v", vd.Advisories)
			}
			adv := vd.Advisories[0]
			if !adv.CVSS4LookupFailed || adv.CVSS4Known || adv.Severity != "" || adv.RecordLookup != "" || domain.CountCVSS4LookupFailures(analyses) != 1 {
				t.Fatalf("advisory=%+v failed analyses=%d", adv, domain.CountCVSS4LookupFailures(analyses))
			}
		})
	}
}

func TestCVSS4FallbackExcludesPrereleaseOnlyIDs(t *testing.T) {
	const id = "GHSA-prerelease-only"
	for _, tc := range []struct {
		name      string
		missing   bool
		wantCalls int32
	}{
		{"zero v3 detail", false, 0},
		{"missing detail withdrawal check", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			oc := osv.NewClient()
			oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"` + id + `"}`)), Header: make(http.Header), Request: r}, nil
			})})
			dd := &stubDepsDevClient{advisoryDetails: map[string]*depsdev.AdvisoryDetail{id: {CVSS3Score: 0}}}
			if tc.missing {
				dd.advisoryDetails = nil
				dd.advisoryMissing = map[string]struct{}{id: {}}
			}
			vd := &domain.VersionDetail{Advisories: []domain.Advisory{{ID: id}}}
			svc := &IntegrationService{depsdevClient: dd, osvClient: oc}
			svc.enrichAdvisorySeverity(context.Background(), map[string]*domain.Analysis{"p": {ReleaseInfo: &domain.ReleaseInfo{PreReleaseVersion: vd}}}, true)
			if got := calls.Load(); got != tc.wantCalls {
				t.Fatalf("OSV calls=%d want %d", got, tc.wantCalls)
			}
			if len(vd.Advisories) != 1 {
				t.Fatalf("advisories=%v", vd.Advisories)
			}
		})
	}
}
