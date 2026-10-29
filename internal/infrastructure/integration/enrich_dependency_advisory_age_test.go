package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/osv"
)

// TestDependencyAdvisoryAge_FetchEnrichDecide runs the OSV fact lookup, the
// enrichment and the lifecycle decision together, with OSV failing for one ID.
// A package with one failed and one aged advisory stays EOL-Effective; a
// package with only the failed one is Stalled with status unknown. See ADR-0032.
func TestDependencyAdvisoryAge_FetchEnrichDecide(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/v1/vulns/GHSA-aged":
			_, _ = w.Write([]byte(`{"id":"GHSA-aged","published":"2018-07-24T00:00:00Z","database_specific":{"severity":"HIGH"}}`))
		case "/v1/vulns/GHSA-moderate":
			_, _ = w.Write([]byte(`{"id":"GHSA-moderate","published":"2018-07-24T00:00:00Z","database_specific":{"severity":"MODERATE"}}`))
		case "/v1/vulns/GHSA-withdrawn":
			_, _ = w.Write([]byte(`{"id":"GHSA-withdrawn","published":"2018-07-24T00:00:00Z","withdrawn":"2026-10-22T00:00:00Z"}`))
		case "/v1/vulns/GHSA-failing":
			// 400 is not retried, so the test does not wait out the backoff;
			// any non-200, non-404 answer is a failed lookup.
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	osvClient := osv.NewClient()
	osvClient.SetBaseURL(srv.URL)

	node := func(name, rel string) depsdev.DependencyNode {
		return depsdev.DependencyNode{VersionKey: depsdev.DependencyVersionKey{System: "NPM", Name: name, Version: "1.0.0"}, Relation: rel}
	}
	graph := func(self string, deps ...string) *depsdev.DependenciesResponse {
		g := &depsdev.DependenciesResponse{Nodes: []depsdev.DependencyNode{node(self, "SELF")}}
		for _, d := range deps {
			g.Nodes = append(g.Nodes, node(d, "DIRECT"))
		}
		return g
	}
	pkgs := map[string]*depsdev.DependenciesResponse{
		"pkg:npm/failed-and-aged@1.0.0": graph("failed-and-aged", "fails", "old"),
		"pkg:npm/failed-only@1.0.0":     graph("failed-only", "fails"),
		"pkg:npm/withdrawn-only@1.0.0":  graph("withdrawn-only", "gone", "low"),
		"pkg:npm/moderate-only@1.0.0":   graph("moderate-only", "mod"),
	}
	dormant := time.Now().AddDate(-4, 0, 0)
	purls := make([]string, 0, len(pkgs))
	analyses := make(map[string]*domain.Analysis, len(pkgs))
	for p := range pkgs {
		purls = append(purls, p)
		analyses[p] = &domain.Analysis{
			EffectivePURL: p,
			Package:       &domain.Package{Ecosystem: "npm", Version: "1.0.0"},
			RepoState:     &domain.RepoState{DaysSinceLastCommit: 1500, LatestHumanCommit: &dormant, CommitStats: &domain.CommitStats{}},
			ReleaseInfo:   &domain.ReleaseInfo{StableVersion: &domain.VersionDetail{Version: "1.0.0", PublishedAt: dormant}},
		}
	}
	sort.Strings(purls)
	svc := &IntegrationService{
		osvClient: osvClient,
		depsdevClient: graphAwareDepsDev{&stubDepsDevClient{
			transitiveAdvisoryResults: map[string][]depsdev.AdvisoryKey{
				"fails@1.0.0": {{ID: "GHSA-failing"}},
				"old@1.0.0":   {{ID: "GHSA-aged"}},
				"gone@1.0.0":  {{ID: "GHSA-withdrawn"}},
				"low@1.0.0":   {{ID: "GHSA-low"}},
				"mod@1.0.0":   {{ID: "GHSA-moderate"}},
			},
			advisoryDetails: map[string]*depsdev.AdvisoryDetail{
				"GHSA-failing":   {CVSS3Score: 9.8},
				"GHSA-aged":      {CVSS3Score: 7.5},
				"GHSA-withdrawn": {CVSS3Score: 9.8},
				"GHSA-low":       {CVSS3Score: 5.3},
				"GHSA-moderate":  {CVSS3Score: 7.5},
			},
		}},
	}

	svc.enrichTransitiveAdvisories(context.Background(), purls, analyses, pkgs)

	mu.Lock()
	sort.Strings(asked)
	got := asked
	mu.Unlock()
	want := []string{"/v1/vulns/GHSA-aged", "/v1/vulns/GHSA-failing", "/v1/vulns/GHSA-moderate", "/v1/vulns/GHSA-withdrawn"}
	if len(got) != len(want) {
		t.Fatalf("OSV lookups: got %v, want %v (GHSA-low is below the threshold and must not be asked)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("OSV lookups: got %v, want %v", got, want)
		}
	}

	assessor := domain.NewLifecycleAssessorService()
	maintained0 := map[string]*domain.ScoreEntity{"Maintained": domain.NewScoreEntity("Maintained", 0, 10, "")}
	tests := []struct {
		purl       string
		wantLabel  domain.MaintenanceStatus
		wantStatus domain.DependencyAdvisoryAgeStatus
		wantID     string
	}{
		{"pkg:npm/failed-and-aged@1.0.0", domain.LabelEOLEffective, domain.DependencyAdvisoryAged, "GHSA-aged"},
		{"pkg:npm/failed-only@1.0.0", domain.LabelStalled, domain.DependencyAdvisoryUnknown, "GHSA-failing"},
		{"pkg:npm/withdrawn-only@1.0.0", domain.LabelStalled, domain.DependencyAdvisoryWithdrawn, "GHSA-withdrawn"},
		{"pkg:npm/moderate-only@1.0.0", domain.LabelStalled, domain.DependencyAdvisoryRatedBelowHigh, "GHSA-moderate"},
	}
	for _, tt := range tests {
		t.Run(tt.purl, func(t *testing.T) {
			res, err := assessor.Assess(context.Background(), domain.AssessmentInput{Analysis: analyses[tt.purl], Scores: maintained0, EOL: domain.EOLStatus{State: domain.EOLNotEOL}})
			if err != nil {
				t.Fatalf("Assess: %v", err)
			}
			if domain.MaintenanceStatus(res.Label) != tt.wantLabel {
				t.Fatalf("Label: got %q, want %q (reason %q)", res.Label, tt.wantLabel, res.Reason)
			}
			if res.DependencyAdvisoryAge == nil {
				t.Fatalf("DependencyAdvisoryAge: got nil, want %q", tt.wantStatus)
			}
			if d := res.DependencyAdvisoryAge; d.Status != tt.wantStatus || d.AdvisoryID != tt.wantID {
				t.Errorf("DependencyAdvisoryAge: got %q %q, want %q %q", d.Status, d.AdvisoryID, tt.wantStatus, tt.wantID)
			}
		})
	}
}

// TestFetchDependencyAdvisoryOSVFacts_Cancelled checks that IDs never sent
// because the context ended are recorded as failed lookups, not dropped:
// otherwise the WARN line would undercount exactly when OSV is slow.
func TestFetchDependencyAdvisoryOSVFacts_Cancelled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	osvClient := osv.NewClient()
	osvClient.SetBaseURL(srv.URL)
	svc := &IntegrationService{osvClient: osvClient}
	details := map[string]*depsdev.AdvisoryDetail{}
	for _, id := range []string{"GHSA-a", "GHSA-b", "GHSA-c"} {
		details[id] = &depsdev.AdvisoryDetail{CVSS3Score: 9.8}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	facts := svc.fetchDependencyAdvisoryOSVFacts(ctx, details)

	if len(facts) != len(details) {
		t.Fatalf("facts: got %d entries, want %d", len(facts), len(details))
	}
	for id, f := range facts {
		if f.lookup != domain.AdvisoryRecordLookupFailed && f.lookup != domain.AdvisoryRecordNotFound {
			t.Errorf("%s: got %q, want lookup_failed (or not_found if it was sent before the cancel was seen)", id, f.lookup)
		}
	}
	failed := 0
	for _, f := range facts {
		if f.lookup == domain.AdvisoryRecordLookupFailed {
			failed++
		}
	}
	if failed == 0 {
		t.Errorf("no ID was recorded as failed after cancellation: %+v", facts)
	}
}

// graphAwareDepsDev returns only the advisory keys of the nodes in the graph
// asked about; stubDepsDevClient returns every key for every graph.
type graphAwareDepsDev struct{ *stubDepsDevClient }

func (g graphAwareDepsDev) FetchTransitiveAdvisoryKeys(_ context.Context, deps *depsdev.DependenciesResponse) (map[string][]depsdev.AdvisoryKey, error) {
	out := make(map[string][]depsdev.AdvisoryKey)
	for _, n := range deps.Nodes {
		key := n.VersionKey.Name + "@" + n.VersionKey.Version
		if keys, ok := g.transitiveAdvisoryResults[key]; ok {
			out[key] = keys
		}
	}
	return out, nil
}

// TestFetchDependencyAdvisoryOSVFacts_Budget runs more IDs than workers
// against an OSV that never answers: no more than osvFactWorkers requests are
// open at once, the budget ends the wait, and every ID, sent or not, is a
// failed lookup.
func TestFetchDependencyAdvisoryOSVFacts_Budget(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	open, maxOpen := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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
	}))
	t.Cleanup(srv.Close)
	osvClient := osv.NewClient()
	osvClient.SetBaseURL(srv.URL)
	svc := &IntegrationService{osvClient: osvClient, osvFactsBudget: 300 * time.Millisecond}
	details := map[string]*depsdev.AdvisoryDetail{}
	for i := range 20 {
		details[fmt.Sprintf("GHSA-%02d", i)] = &depsdev.AdvisoryDetail{CVSS3Score: 9.8}
	}

	start := time.Now()
	facts := svc.fetchDependencyAdvisoryOSVFacts(context.Background(), details)
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("elapsed %v: the budget did not stop the lookups", elapsed)
	}
	if len(facts) != len(details) {
		t.Fatalf("facts: got %d, want %d", len(facts), len(details))
	}
	for id, f := range facts {
		if f.lookup != domain.AdvisoryRecordLookupFailed {
			t.Errorf("%s: got %q, want lookup_failed", id, f.lookup)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if maxOpen > osvFactWorkers || maxOpen == 0 {
		t.Errorf("concurrent requests: got %d, want 1..%d", maxOpen, osvFactWorkers)
	}
}
