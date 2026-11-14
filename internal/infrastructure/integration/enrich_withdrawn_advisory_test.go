package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/osv"
)

type withdrawnRoundTrip func(*http.Request) (*http.Response, error)

func (f withdrawnRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWithdrawnAdvisoryDecision(t *testing.T) {
	const id = "GHSA-withdrawn-test"
	for _, tc := range []struct {
		name      string
		missing   bool
		osvStatus int
		osvBody   string
		noOSV     bool
		wantKeep  bool
		wantCalls int32
	}{
		{"confirmed", true, 200, `{"id":"GHSA-withdrawn-test","withdrawn":"2026-10-22T19:21:04Z"}`, false, false, 1},
		{"active", true, 200, `{"id":"GHSA-withdrawn-test"}`, false, true, 1},
		{"osv missing", true, 404, ``, false, true, 1},
		{"osv error", true, 400, ``, false, true, 1},
		{"mismatched ID", true, 200, `{"id":"GHSA-other"}`, false, true, 1},
		{"no osv client", true, 0, ``, true, true, 0},
		{"depsdev transient failure", false, 200, `{"id":"GHSA-withdrawn-test","withdrawn":"2026-10-22T19:21:04Z"}`, false, true, 0},
		{"detail success", false, 200, `{"id":"GHSA-withdrawn-test","withdrawn":"2026-10-22T19:21:04Z"}`, false, true, 0},
	} {
		for _, path := range []string{"direct", "dependency"} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32
				var oc *osv.Client
				if !tc.noOSV {
					oc = osv.NewClient()
					oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
						if got := r.URL.Path; got != "/v1/vulns/"+id {
							t.Errorf("OSV path = %q", got)
						}
						calls.Add(1)
						return &http.Response{StatusCode: tc.osvStatus, Body: io.NopCloser(strings.NewReader(tc.osvBody)), Header: make(http.Header), Request: r}, nil
					})})
				}
				missing := map[string]struct{}{}
				if tc.missing {
					missing[id] = struct{}{}
				}
				dd := &stubDepsDevClient{advisoryMissing: missing, transitiveAdvisoryResults: map[string][]depsdev.AdvisoryKey{"dep@1.0.0": {{ID: id}}}}
				if tc.name == "detail success" {
					dd.advisoryDetails = map[string]*depsdev.AdvisoryDetail{id: {CVSS3Score: 2.0, Title: "Known low"}}
				}
				svc := &IntegrationService{depsdevClient: dd, osvClient: oc}
				vd := &domain.VersionDetail{Version: "1.0.0"}
				a := &domain.Analysis{ReleaseInfo: &domain.ReleaseInfo{StableVersion: vd}}
				if path == "direct" {
					vd.Advisories = []domain.Advisory{{ID: id}}
					svc.enrichAdvisorySeverity(context.Background(), map[string]*domain.Analysis{"p": a}, true)
				} else {
					graph := &depsdev.DependenciesResponse{Nodes: []depsdev.DependencyNode{
						{VersionKey: depsdev.DependencyVersionKey{Name: "self", Version: "1.0.0"}, Relation: "SELF"},
						{VersionKey: depsdev.DependencyVersionKey{Name: "dep", Version: "1.0.0"}, Relation: "DIRECT"},
					}}
					svc.enrichTransitiveAdvisories(context.Background(), []string{"p"}, map[string]*domain.Analysis{"p": a}, map[string]*depsdev.DependenciesResponse{"p": graph}, nil)
				}
				if got := len(vd.Advisories); (got == 1) != tc.wantKeep {
					t.Errorf("advisories = %v, wantKeep %v", vd.Advisories, tc.wantKeep)
				}
				if got := calls.Load(); got != tc.wantCalls {
					t.Errorf("OSV calls = %d, want %d", got, tc.wantCalls)
				}
				if tc.wantKeep && tc.name != "detail success" && len(vd.Advisories) == 1 {
					adv := vd.Advisories[0]
					if adv.RecordLookup != "" || !adv.Published.IsZero() || adv.ReviewedSeverity != "" || adv.Severity != "" {
						t.Errorf("retained unknown-severity advisory has facts: %+v", adv)
					}
				}
				if tc.name == "detail success" && len(vd.Advisories) == 1 && vd.Advisories[0].Severity != "LOW" {
					t.Errorf("detail severity = %q, want LOW", vd.Advisories[0].Severity)
				}
			})
		}
	}
}

func TestWithdrawnAdvisorySharedDirectAndDependency(t *testing.T) {
	const id = "GHSA-shared"
	var calls atomic.Int32
	oc := osv.NewClient()
	oc.SetCacheTTL(0)
	oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"GHSA-shared","withdrawn":"2026-10-22T00:00:00Z"}`)), Header: make(http.Header), Request: r}, nil
	})})
	dd := &stubDepsDevClient{advisoryMissing: map[string]struct{}{id: {}}, transitiveAdvisoryResults: map[string][]depsdev.AdvisoryKey{"dep@1.0.0": {{ID: id}}}}
	svc := &IntegrationService{depsdevClient: dd, osvClient: oc}
	stable := &domain.VersionDetail{Version: "1.0.0", Advisories: []domain.Advisory{{ID: id}}}
	max := &domain.VersionDetail{Version: "1.0.0", Advisories: []domain.Advisory{{ID: id}}}
	a := &domain.Analysis{ReleaseInfo: &domain.ReleaseInfo{StableVersion: stable, MaxSemverVersion: max}}
	confirmed := svc.enrichAdvisorySeverity(context.Background(), map[string]*domain.Analysis{"p": a}, true)
	graph := &depsdev.DependenciesResponse{Nodes: []depsdev.DependencyNode{{VersionKey: depsdev.DependencyVersionKey{Name: "self", Version: "1.0.0"}, Relation: "SELF"}}}
	svc.enrichTransitiveAdvisories(context.Background(), []string{"p"}, map[string]*domain.Analysis{"p": a}, map[string]*depsdev.DependenciesResponse{"p": graph}, confirmed)
	if len(stable.Advisories) != 0 || len(max.Advisories) != 0 || len(confirmed) != 1 {
		t.Errorf("shared advisory survived: stable=%v max=%v confirmed=%v", stable.Advisories, max.Advisories, confirmed)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("OSV calls = %d, want 1", got)
	}
}

func TestWithdrawnAdvisorySelectedFallback(t *testing.T) {
	for _, tc := range []struct {
		name    string
		withPre bool
		wantPre int
		wantReq int
	}{
		{"prerelease selected", true, 0, 1},
		{"requested selected", false, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id = "GHSA-fallback"
			oc := osv.NewClient()
			oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"GHSA-fallback","withdrawn":"2026-10-22T00:00:00Z"}`)), Header: make(http.Header), Request: r}, nil
			})})
			pre := &domain.VersionDetail{Advisories: []domain.Advisory{{ID: id}}}
			req := &domain.VersionDetail{Advisories: []domain.Advisory{{ID: id}}}
			ri := &domain.ReleaseInfo{RequestedVersion: req}
			if tc.withPre {
				ri.PreReleaseVersion = pre
			}
			svc := &IntegrationService{depsdevClient: &stubDepsDevClient{advisoryMissing: map[string]struct{}{id: {}}}, osvClient: oc}
			svc.enrichAdvisorySeverity(context.Background(), map[string]*domain.Analysis{"p": {ReleaseInfo: ri}}, true)
			if len(pre.Advisories) != tc.wantPre || len(req.Advisories) != tc.wantReq {
				t.Errorf("fallback scope: prerelease=%v requested=%v", pre.Advisories, req.Advisories)
			}
		})
	}
}

func TestWithdrawnAdvisoryLifecycle(t *testing.T) {
	const id = "GHSA-lifecycle"
	for _, path := range []string{"direct", "dependency"} {
		for _, withdrawn := range []bool{false, true} {
			name := path + "/active"
			if withdrawn {
				name = path + "/withdrawn"
			}
			t.Run(name, func(t *testing.T) {
				body := `{"id":"GHSA-lifecycle"}`
				if withdrawn {
					body = `{"id":"GHSA-lifecycle","withdrawn":"2026-10-22T00:00:00Z"}`
				}
				oc := osv.NewClient()
				oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
				})})
				old := time.Now().AddDate(-4, 0, 0)
				vd := &domain.VersionDetail{Version: "1.0.0", PublishedAt: old}
				a := &domain.Analysis{RepoState: &domain.RepoState{DaysSinceLastCommit: 1500, LatestHumanCommit: &old, CommitStats: &domain.CommitStats{}}, ReleaseInfo: &domain.ReleaseInfo{StableVersion: vd}}
				dd := &stubDepsDevClient{advisoryMissing: map[string]struct{}{id: {}}, transitiveAdvisoryResults: map[string][]depsdev.AdvisoryKey{"dep@1.0.0": {{ID: id}}}}
				svc := &IntegrationService{depsdevClient: dd, osvClient: oc}
				if path == "direct" {
					vd.Advisories = []domain.Advisory{{ID: id}}
					svc.enrichAdvisorySeverity(context.Background(), map[string]*domain.Analysis{"p": a}, true)
				} else {
					graph := &depsdev.DependenciesResponse{Nodes: []depsdev.DependencyNode{{VersionKey: depsdev.DependencyVersionKey{Name: "self", Version: "1.0.0"}, Relation: "SELF"}}}
					svc.enrichTransitiveAdvisories(context.Background(), []string{"p"}, map[string]*domain.Analysis{"p": a}, map[string]*depsdev.DependenciesResponse{"p": graph}, nil)
				}
				res, err := domain.NewLifecycleAssessorService().Assess(context.Background(), domain.AssessmentInput{Analysis: a, Scores: map[string]*domain.ScoreEntity{"Maintained": domain.NewScoreEntity("Maintained", 0, 10, ""), "Vulnerabilities": domain.NewScoreEntity("Vulnerabilities", 5, 10, "")}, EOL: domain.EOLStatus{State: domain.EOLNotEOL}})
				if err != nil {
					t.Fatal(err)
				}
				if withdrawn {
					if len(vd.Advisories) != 0 || res.Label != string(domain.LabelLegacySafe) {
						t.Errorf("withdrawn: advisories=%v label=%s", vd.Advisories, res.Label)
					}
				} else if len(vd.Advisories) != 1 || res.Label == string(domain.LabelLegacySafe) || res.Label == string(domain.LabelActive) {
					t.Errorf("active: advisories=%v label=%s", vd.Advisories, res.Label)
				}
			})
		}
	}
}

func TestFetchAdvisoryOSVFacts_DeduplicatesIDs(t *testing.T) {
	var calls atomic.Int32
	oc := osv.NewClient()
	oc.SetCacheTTL(0)
	oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"GHSA-duplicate","withdrawn":"2026-10-22T00:00:00Z"}`)), Header: make(http.Header), Request: r}, nil
	})})
	svc := &IntegrationService{osvClient: oc}
	facts := svc.fetchAdvisoryOSVFacts(context.Background(), []string{"GHSA-duplicate", "GHSA-duplicate"})
	if got := calls.Load(); got != 1 {
		t.Errorf("calls=%d, want 1", got)
	}
	if len(facts) != 1 || facts["GHSA-duplicate"].lookup != domain.AdvisoryRecordWithdrawn {
		t.Errorf("facts=%v", facts)
	}
}

type recordingAdvisoryDepsDev struct {
	stubDepsDevClient
	requested []string
}

func (c *recordingAdvisoryDepsDev) FetchAdvisoriesBatchWithStatus(_ context.Context, ids []string) (map[string]*depsdev.AdvisoryDetail, map[string]struct{}) {
	c.requested = append(c.requested, ids...)
	missing := make(map[string]struct{})
	for _, id := range ids {
		if _, ok := c.advisoryMissing[id]; ok {
			missing[id] = struct{}{}
		}
	}
	return nil, missing
}

func TestWithdrawnAdvisoryUnusedVersionScope(t *testing.T) {
	dd := &recordingAdvisoryDepsDev{stubDepsDevClient: stubDepsDevClient{advisoryMissing: map[string]struct{}{"GHSA-stable": {}, "GHSA-unused": {}}}}
	svc := &IntegrationService{depsdevClient: dd}
	a := &domain.Analysis{ReleaseInfo: &domain.ReleaseInfo{
		StableVersion:     &domain.VersionDetail{Advisories: []domain.Advisory{{ID: "GHSA-stable"}}},
		PreReleaseVersion: &domain.VersionDetail{Advisories: []domain.Advisory{{ID: "GHSA-unused"}}},
		RequestedVersion:  &domain.VersionDetail{Advisories: []domain.Advisory{{ID: "GHSA-unused"}}},
	}}
	svc.enrichAdvisorySeverity(context.Background(), map[string]*domain.Analysis{"p": a}, true)
	if len(dd.requested) != 1 || dd.requested[0] != "GHSA-stable" {
		t.Errorf("detail requests = %v, want only GHSA-stable", dd.requested)
	}
}

func TestWithdrawnAdvisoryRetainsSibling(t *testing.T) {
	for _, path := range []string{"direct", "dependency"} {
		t.Run(path, func(t *testing.T) {
			oc := osv.NewClient()
			oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"GHSA-gone","withdrawn":"2026-10-22T00:00:00Z"}`)), Header: make(http.Header), Request: r}, nil
			})})
			dd := &stubDepsDevClient{
				advisoryMissing:           map[string]struct{}{"GHSA-gone": {}},
				advisoryDetails:           map[string]*depsdev.AdvisoryDetail{"GHSA-low": {CVSS3Score: 2.0}},
				transitiveAdvisoryResults: map[string][]depsdev.AdvisoryKey{"dep@1.0.0": {{ID: "GHSA-gone"}, {ID: "GHSA-low"}}},
			}
			svc := &IntegrationService{depsdevClient: dd, osvClient: oc}
			vd := &domain.VersionDetail{Version: "1.0.0"}
			a := &domain.Analysis{ReleaseInfo: &domain.ReleaseInfo{StableVersion: vd}}
			if path == "direct" {
				vd.Advisories = []domain.Advisory{{ID: "GHSA-gone"}, {ID: "GHSA-low"}}
				svc.enrichAdvisorySeverity(context.Background(), map[string]*domain.Analysis{"p": a}, true)
			} else {
				graph := &depsdev.DependenciesResponse{Nodes: []depsdev.DependencyNode{{VersionKey: depsdev.DependencyVersionKey{Name: "self", Version: "1.0.0"}, Relation: "SELF"}}}
				svc.enrichTransitiveAdvisories(context.Background(), []string{"p"}, map[string]*domain.Analysis{"p": a}, map[string]*depsdev.DependenciesResponse{"p": graph}, nil)
			}
			if len(vd.Advisories) != 1 || vd.Advisories[0].ID != "GHSA-low" || vd.Advisories[0].Severity != "LOW" {
				t.Errorf("advisories=%v, want retained LOW sibling only", vd.Advisories)
			}
		})
	}
}

func TestFetchAdvisoryOSVFacts_BudgetForMissingDetails(t *testing.T) {
	var mu sync.Mutex
	open, maxOpen := 0, 0
	oc := osv.NewClient()
	oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		open++
		if open > maxOpen {
			maxOpen = open
		}
		mu.Unlock()
		<-r.Context().Done()
		mu.Lock()
		open--
		mu.Unlock()
		return nil, r.Context().Err()
	})})
	svc := &IntegrationService{osvClient: oc, osvFactsBudget: 50 * time.Millisecond}
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = fmt.Sprintf("GHSA-missing-%02d", i)
	}
	start := time.Now()
	facts := svc.fetchAdvisoryOSVFacts(context.Background(), ids)
	if time.Since(start) > time.Second {
		t.Error("OSV budget did not bound lookup duration")
	}
	if len(facts) != len(ids) {
		t.Fatalf("facts=%d, want %d", len(facts), len(ids))
	}
	for id, f := range facts {
		if f.lookup != domain.AdvisoryRecordLookupFailed {
			t.Errorf("%s lookup=%q, want failed", id, f.lookup)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if maxOpen == 0 || maxOpen > osvFactWorkers {
		t.Errorf("max concurrent requests=%d", maxOpen)
	}
}

type withdrawnGitHubDepsDev struct{ stubDepsDevClient }

func (c *withdrawnGitHubDepsDev) GetLatestReleasesForPURLs(_ context.Context, purls []string) (map[string]*depsdev.ReleaseInfo, error) {
	ri := &depsdev.ReleaseInfo{}
	ri.StableVersion.VersionKey.Version = "1.0.0"
	return map[string]*depsdev.ReleaseInfo{purls[0]: ri}, nil
}

func (c *withdrawnGitHubDepsDev) GetDetailsForPURLs(_ context.Context, purls []string) (map[string]*depsdev.BatchResult, error) {
	v := depsdev.Version{VersionKey: depsdev.VersionKey{Version: "1.0.0"}}
	ri := depsdev.ReleaseInfo{StableVersion: v, MaxSemverVersion: v, RequestedVersion: v}
	return map[string]*depsdev.BatchResult{purls[0]: {PURL: purls[0], Package: &depsdev.Package{Versions: []depsdev.Version{v}}, RepoURL: "https://github.com/example/withdrawn-test", ReleaseInfo: ri}}, nil
}

func TestAnalyzeFromGitHubURLs_RetainsUnconfirmedDependencyOnce(t *testing.T) {
	const githubURL = "https://github.com/example/withdrawn-test"
	const graphQLBody = `{"data":{"repository":{"isArchived":false,"isDisabled":false,"isFork":false,"stargazerCount":1,"forkCount":1,"dependencyGraphManifests":{"nodes":[{"filename":"requirements.txt","dependencies":{"nodes":[{"packageManager":"PIP","packageName":"demo"}]}}]}},"rateLimit":{"cost":1,"remaining":4999,"resetAt":""}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(graphQLBody))
	}))
	defer srv.Close()
	cfg := &config.Config{}
	cfg.GitHub.Token = "test-token"
	cfg.GitHub.BaseURL = srv.URL
	cfg.GitHub.Timeout = 10 * time.Second
	var calls atomic.Int32
	oc := osv.NewClient()
	oc.SetCacheTTL(0)
	oc.SetHTTPClient(&http.Client{Transport: withdrawnRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1/vulns/GHSA-unknown" {
			calls.Add(1)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"GHSA-unknown"}`)), Header: make(http.Header), Request: r}, nil
	})})
	graph := &depsdev.DependenciesResponse{Nodes: []depsdev.DependencyNode{{VersionKey: depsdev.DependencyVersionKey{Name: "self", Version: "1.0.0"}, Relation: "SELF"}}}
	dd := &withdrawnGitHubDepsDev{stubDepsDevClient{dependenciesResults: map[string]*depsdev.DependenciesResponse{"pkg:pypi/withdrawn-test@1.0.0": graph}, advisoryMissing: map[string]struct{}{"GHSA-unknown": {}}, advisoryDetails: map[string]*depsdev.AdvisoryDetail{"GHSA-low": {Title: "Low", CVSS3Score: 2.0}}, transitiveAdvisoryResults: map[string][]depsdev.AdvisoryKey{"dep@1.0.0": {{ID: "GHSA-unknown"}, {ID: "GHSA-low"}}}}}
	svc := NewIntegrationService(github.NewClient(cfg), dd)
	svc.osvClient = oc
	results, err := svc.AnalyzeFromGitHubURLs(context.Background(), []string{githubURL})
	if err != nil {
		t.Fatal(err)
	}
	a := results[githubURL]
	if a == nil || a.ReleaseInfo == nil || a.ReleaseInfo.StableVersion == nil {
		t.Fatalf("missing PURL analysis: %+v", a)
	}
	seen := make(map[string]domain.Advisory)
	for _, adv := range a.ReleaseInfo.StableVersion.Advisories {
		seen[adv.ID] = adv
	}
	if len(seen) != 2 || seen["GHSA-unknown"].Relation != domain.AdvisoryRelationTransitive || seen["GHSA-low"].Severity != "LOW" {
		t.Errorf("stable advisories = %v", seen)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("OSV calls = %d, want 1", got)
	}
}
