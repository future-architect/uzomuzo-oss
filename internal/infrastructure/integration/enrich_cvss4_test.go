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
	dd := &withdrawnGitHubDepsDev{stubDepsDevClient{dependenciesResults: map[string]*depsdev.DependenciesResponse{"pkg:pypi/withdrawn-test@1.0.0": graph}, advisoryDetails: map[string]*depsdev.AdvisoryDetail{id: {CVSS3Score: 0}}, transitiveAdvisoryResults: map[string][]depsdev.AdvisoryKey{"dep@1.0.0": {{ID: id}}}}}
	svc := NewIntegrationService(github.NewClient(cfg), dd)
	svc.osvClient = oc
	results, err := svc.AnalyzeFromGitHubURLs(context.Background(), []string{githubURL})
	if err != nil {
		t.Fatal(err)
	}
	a := results[githubURL]
	if a == nil || a.ReleaseInfo == nil || a.ReleaseInfo.StableVersion == nil || len(a.ReleaseInfo.StableVersion.Advisories) != 1 {
		t.Fatalf("analysis=%+v", a)
	}
	adv := a.ReleaseInfo.StableVersion.Advisories[0]
	if !adv.CVSS4Known || adv.CVSS4Score != 6.6 || adv.Severity != "MEDIUM" || calls.Load() != 1 {
		t.Fatalf("advisory=%+v OSV calls=%d", adv, calls.Load())
	}
}

func TestCVSS4FallbackKeepsOSVFactPriority(t *testing.T) {
	details := map[string]*depsdev.AdvisoryDetail{}
	missing := map[string]struct{}{"GHSA-000-missing": {}}
	for i := range 8 {
		details[fmt.Sprintf("GHSA-%03d-high", i)] = &depsdev.AdvisoryDetail{CVSS3Score: 9.0}
	}
	details["GHSA-999-v4"] = &depsdev.AdvisoryDetail{CVSS3Score: 0}
	oc := osv.NewClient()
	oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
		id := strings.TrimPrefix(r.URL.Path, "/v1/vulns/")
		if id == "GHSA-999-v4" {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"` + id + `"}`)), Header: make(http.Header), Request: r}, nil
	})})
	svc := &IntegrationService{osvClient: oc, osvFactsBudget: 100 * time.Millisecond}
	facts := svc.fetchDependencyAdvisoryOSVFacts(context.Background(), details, missing)
	for id := range missing {
		if facts[id].lookup != domain.AdvisoryRecordFound {
			t.Errorf("missing priority %s=%s", id, facts[id].lookup)
		}
	}
	for id, d := range details {
		if d.CVSS3Score == 0 {
			continue
		}
		if facts[id].lookup != domain.AdvisoryRecordFound {
			t.Errorf("high priority %s=%s", id, facts[id].lookup)
		}
	}
	if facts["GHSA-999-v4"].lookup != domain.AdvisoryRecordLookupFailed {
		t.Errorf("v4=%s", facts["GHSA-999-v4"].lookup)
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
