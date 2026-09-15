package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/osv"
)

// unmaintainedPage is an OSV response whose single advisory satisfies every
// admission rule, aged well past the cooldown.
func unmaintainedPage(name string) string {
	published := time.Now().Add(-100 * 24 * time.Hour).UTC().Format(time.RFC3339)
	return fmt.Sprintf(`{"vulns":[{"id":"RUSTSEC-2024-0375","summary":"%s is unmaintained","published":%q,
	 "references":[{"type":"ADVISORY","url":"https://rustsec.org/advisories/RUSTSEC-2024-0375.html"}],
	 "affected":[{"package":{"name":%q,"ecosystem":"crates.io"},
	  "ranges":[{"type":"SEMVER","events":[{"introduced":"0.0.0-0"}]}],
	  "database_specific":{"informational":"unmaintained"}}]}]}`, name, published, name)
}

// newAdvisoryDBService wires an IntegrationService whose OSV client points at
// srv and records every package name it was asked about.
func newAdvisoryDBService(t *testing.T, srvURL string) *IntegrationService {
	t.Helper()
	c := osv.NewClient()
	c.SetBaseURL(srvURL)
	c.SetCacheTTL(0)
	return &IntegrationService{osvClient: c}
}

// osvRecorder answers every query with body and records the names requested.
type osvRecorder struct {
	mu    sync.Mutex
	names []string
	calls atomic.Int32
}

func (rec *osvRecorder) handler(body func(name string) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec.calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Package struct {
				Name      string `json:"name"`
				Ecosystem string `json:"ecosystem"`
			} `json:"package"`
		}
		_ = json.Unmarshal(raw, &req)
		rec.mu.Lock()
		rec.names = append(rec.names, req.Package.Ecosystem+"/"+req.Package.Name)
		rec.mu.Unlock()
		_, _ = fmt.Fprint(w, body(req.Package.Name))
	}
}

func (rec *osvRecorder) requested() []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]string{}, rec.names...)
}

func TestEnrichAdvisoryDBState_FlagsCargoPackage(t *testing.T) {
	t.Parallel()
	rec := &osvRecorder{}
	srv := httptest.NewServer(rec.handler(unmaintainedPage))
	defer srv.Close()

	a := analysisFor("pkg:cargo/atty@0.2.14", "cargo")
	analyses := map[string]*domain.Analysis{"pkg:cargo/atty@0.2.14": a}
	newAdvisoryDBService(t, srv.URL).enrichAdvisoryDBState(context.Background(), analyses)

	if a.AdvisoryDBState == nil {
		t.Fatalf("AdvisoryDBState is nil — a successful lookup must always write it")
	}
	if !a.AdvisoryDBState.Unmaintained {
		t.Errorf("Unmaintained: got false, want true")
	}
	if a.AdvisoryDBState.AdvisoryID != "RUSTSEC-2024-0375" {
		t.Errorf("AdvisoryID: got %q", a.AdvisoryDBState.AdvisoryID)
	}
	// The ecosystem sent must be OSV's identifier, not the PURL type "cargo".
	if got := rec.requested(); len(got) != 1 || got[0] != "crates.io/atty" {
		t.Errorf("requested: got %v, want [crates.io/atty]", got)
	}
	// The fact must never reach EOLStatus — that is the field the downstream
	// catalog treats as authoritative enough to overrule a human. See ADR-0025.
	if a.EOL.State != "" {
		t.Errorf("EOLStatus.State was set to %q by this path", a.EOL.State)
	}
}

func TestEnrichAdvisoryDBState_CleanPackageIsRecordedNotSkipped(t *testing.T) {
	t.Parallel()
	rec := &osvRecorder{}
	srv := httptest.NewServer(rec.handler(func(string) string { return `{"vulns":[]}` }))
	defer srv.Close()

	a := analysisFor("pkg:cargo/serde@1.0.210", "cargo")
	newAdvisoryDBService(t, srv.URL).enrichAdvisoryDBState(context.Background(),
		map[string]*domain.Analysis{"pkg:cargo/serde@1.0.210": a})

	// "asked, nothing flagged" must be distinguishable from "not asked".
	if a.AdvisoryDBState == nil {
		t.Fatalf("AdvisoryDBState is nil — a clean answer is still an answer")
	}
	if a.AdvisoryDBState.Unmaintained {
		t.Errorf("Unmaintained: got true, want false")
	}
}

func TestEnrichAdvisoryDBState_NonCargoIssuesNoRequest(t *testing.T) {
	t.Parallel()
	rec := &osvRecorder{}
	srv := httptest.NewServer(rec.handler(unmaintainedPage))
	defer srv.Close()

	analyses := map[string]*domain.Analysis{
		"npm":    analysisFor("pkg:npm/express@4.21.2", "npm"),
		"pypi":   analysisFor("pkg:pypi/requests@2.32.3", "pypi"),
		"maven":  analysisFor("pkg:maven/org.slf4j/slf4j-api@2.0.16", "maven"),
		"golang": analysisFor("pkg:golang/github.com/gin-gonic/gin@v1.10.0", "golang"),
		"gem":    analysisFor("pkg:gem/rails@7.1.0", "gem"),
	}
	newAdvisoryDBService(t, srv.URL).enrichAdvisoryDBState(context.Background(), analyses)

	if got := rec.calls.Load(); got != 0 {
		t.Errorf("OSV requests: got %d, want 0 — only cargo is asked (requested %v)", got, rec.requested())
	}
	for k, a := range analyses {
		if a.AdvisoryDBState != nil {
			t.Errorf("%s: AdvisoryDBState was populated for a non-cargo package", k)
		}
	}
}

func TestEnrichAdvisoryDBState_OneRequestPerCrateAcrossVersions(t *testing.T) {
	t.Parallel()
	rec := &osvRecorder{}
	srv := httptest.NewServer(rec.handler(unmaintainedPage))
	defer srv.Close()

	// Three versions of one crate, plus a case-variant name: a batch must not
	// turn them into four lookups.
	analyses := map[string]*domain.Analysis{
		"v1":   analysisFor("pkg:cargo/atty@0.2.14", "cargo"),
		"v2":   analysisFor("pkg:cargo/atty@0.2.13", "cargo"),
		"v3":   analysisFor("pkg:cargo/atty", "cargo"),
		"case": analysisFor("pkg:cargo/Atty@0.2.11", "cargo"),
	}
	// Caching is disabled in this service, so the count measures dispatch-level
	// deduplication rather than the client's cache.
	newAdvisoryDBService(t, srv.URL).enrichAdvisoryDBState(context.Background(), analyses)

	if got := rec.calls.Load(); got != 1 {
		t.Errorf("OSV requests: got %d, want 1 (requested %v)", got, rec.requested())
	}
	for k, a := range analyses {
		if a.AdvisoryDBState == nil || !a.AdvisoryDBState.Unmaintained {
			t.Errorf("%s: every version of the crate must receive the fact, got %+v", k, a.AdvisoryDBState)
		}
	}
	// Each analysis must own its copy, so a later per-analysis edit cannot leak.
	if analyses["v1"].AdvisoryDBState == analyses["v2"].AdvisoryDBState {
		t.Fatal("two analyses share one AdvisoryDBState pointer")
	}
	// Distinct pointers are not enough: the slice inside must not share storage.
	analyses["v1"].AdvisoryDBState.MarkerIDs[0] = "MUTATED"
	if got := analyses["v2"].AdvisoryDBState.MarkerIDs[0]; got == "MUTATED" {
		t.Error("two analyses share one MarkerIDs backing array")
	}
}

func TestEnrichAdvisoryDBState_FetchFailureLeavesStateNil(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := analysisFor("pkg:cargo/atty@0.2.14", "cargo")
	newAdvisoryDBService(t, srv.URL).enrichAdvisoryDBState(context.Background(),
		map[string]*domain.Analysis{"a": a})

	// A failed lookup is unknown, not "not flagged".
	if a.AdvisoryDBState != nil {
		t.Errorf("AdvisoryDBState: got %+v, want nil after a failed lookup", a.AdvisoryDBState)
	}
}

func TestEnrichAdvisoryDBState_NoClientIsANoOp(t *testing.T) {
	t.Parallel()
	a := analysisFor("pkg:cargo/atty@0.2.14", "cargo")
	(&IntegrationService{}).enrichAdvisoryDBState(context.Background(),
		map[string]*domain.Analysis{"a": a})
	if a.AdvisoryDBState != nil {
		t.Errorf("AdvisoryDBState: got %+v, want nil when the client is unwired", a.AdvisoryDBState)
	}
}

func TestEnrichAdvisoryDBState_SkipsUnparseableAndNamespacedPURLs(t *testing.T) {
	t.Parallel()
	rec := &osvRecorder{}
	srv := httptest.NewServer(rec.handler(unmaintainedPage))
	defer srv.Close()

	analyses := map[string]*domain.Analysis{
		"garbage":    analysisFor("not-a-purl", "cargo"),
		"empty":      analysisFor("", "cargo"),
		"namespaced": analysisFor("pkg:cargo/somenamespace/atty@0.2.14", "cargo"),
		"nil_pkg":    {},
	}
	newAdvisoryDBService(t, srv.URL).enrichAdvisoryDBState(context.Background(), analyses)

	if got := rec.calls.Load(); got != 0 {
		t.Errorf("OSV requests: got %d, want 0 (requested %v)", got, rec.requested())
	}
}

func TestEnrichAdvisoryDBState_CancelledContextStopsDispatch(t *testing.T) {
	t.Parallel()
	rec := &osvRecorder{}
	srv := httptest.NewServer(rec.handler(unmaintainedPage))
	defer srv.Close()

	analyses := map[string]*domain.Analysis{}
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("pkg:cargo/crate%02d@1.0.0", i)
		analyses[key] = analysisFor(key, "cargo")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	newAdvisoryDBService(t, srv.URL).enrichAdvisoryDBState(ctx, analyses)

	// Dispatch stops on cancellation rather than parking a goroutine per package.
	if got := rec.calls.Load(); got > maxPackageFactWorkers {
		t.Errorf("OSV requests after cancellation: got %d, want at most %d", got, maxPackageFactWorkers)
	}
}
