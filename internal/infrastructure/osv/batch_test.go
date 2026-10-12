package osv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// batchServer answers /v1/querybatch with batchBody(queries) and /v1/query with
// the atty page, and records which packages reached /v1/query.
type batchServer struct {
	mu        sync.Mutex
	batches   []int
	queried   []string
	batchBody func(n int) (int, string)
}

func (b *batchServer) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Queries []queryRequest `json:"queries"`
			queryRequest
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		switch r.URL.Path {
		case "/v1/querybatch":
			b.mu.Lock()
			b.batches = append(b.batches, len(body.Queries))
			b.mu.Unlock()
			// Not under the lock: a test may hold the answer back.
			status, out := b.batchBody(len(body.Queries))
			w.WriteHeader(status)
			_, _ = fmt.Fprint(w, out)
		case "/v1/query":
			b.mu.Lock()
			b.queried = append(b.queried, body.Package.Name)
			b.mu.Unlock()
			_, _ = fmt.Fprint(w, attyPage)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
}

func newBatchClient(t *testing.T, b *batchServer) *Client {
	t.Helper()
	srv := httptest.NewServer(b.handler(t))
	t.Cleanup(srv.Close)
	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(time.Minute)
	return c
}

// queryAll runs QueryPackage for each name and returns the record count per name.
func queryAll(t *testing.T, c *Client, names ...string) map[string]int {
	t.Helper()
	got := map[string]int{}
	for _, n := range names {
		recs, err := c.QueryPackage(context.Background(), "crates.io", n)
		if err != nil {
			t.Fatalf("QueryPackage(%s): %v", n, err)
		}
		got[n] = len(recs)
	}
	return got
}

func refs(names ...string) []PackageRef {
	out := make([]PackageRef, len(names))
	for i, n := range names {
		out[i] = PackageRef{Ecosystem: "crates.io", Name: n}
	}
	return out
}

// A package the batch reports clean is served from the cache; a package with
// an advisory, or one whose answer has a further page, still gets its own
// query and its full records.
func TestPrimeNoAdvisories_OnlyPackagesWithAdvisoriesAreQueried(t *testing.T) {
	t.Parallel()
	b := &batchServer{batchBody: func(int) (int, string) {
		return http.StatusOK, `{"results":[{},{"vulns":[{"id":"RUSTSEC-2024-0375"}]},{"next_page_token":"x"},{"vulns":[]}]}`
	}}
	c := newBatchClient(t, b)
	c.PrimeNoAdvisories(context.Background(), refs("serde", "atty", "paged", "rand"))

	got := queryAll(t, c, "serde", "atty", "paged", "rand")
	if strings.Join(b.queried, ",") != "atty,paged" {
		t.Errorf("queried %v, want [atty paged]", b.queried)
	}
	if got["serde"] != 0 || got["rand"] != 0 || got["atty"] != 3 {
		t.Errorf("records = %v, want serde 0, rand 0, atty 3", got)
	}
}

// Nothing is cached as clean unless OSV.dev said so for that package: a failed
// batch, a result count that cannot be matched to the queries, or a result that
// is not a clean object leaves those packages to their own query.
func TestPrimeNoAdvisories_UnclearAnswersFallBackToQuery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body func(int) (int, string)
		want string
	}{
		{"http error", func(int) (int, string) { return http.StatusBadRequest, `{}` }, "a,b"},
		{"fewer results than queries", func(int) (int, string) { return http.StatusOK, `{"results":[{}]}` }, "a,b"},
		{"not an object", func(int) (int, string) { return http.StatusOK, `null` }, "a,b"},
		{"results missing", func(int) (int, string) { return http.StatusOK, `{}` }, "a,b"},
		{"explicit null vulns", func(int) (int, string) { return http.StatusOK, `{"results":[{"vulns":null},{}]}` }, "a"},
		{"result not an object", func(int) (int, string) { return http.StatusOK, `{"results":[null,{}]}` }, "a"},
		{"upper-case null vulns", func(int) (int, string) { return http.StatusOK, `{"results":[{"VULNS":null},{}]}` }, "a"},
		{"repeated vulns key", func(int) (int, string) { return http.StatusOK, `{"results":[{"vulns":null,"vulns":[]},{}]}` }, "a"},
		{"mixed-case advisory list", func(int) (int, string) { return http.StatusOK, `{"results":[{"Vulns":[{"id":"MAL-1"}]},{}]}` }, "a"},
		{"mixed-case next page", func(int) (int, string) { return http.StatusOK, `{"results":[{"Next_Page_Token":"p"},{}]}` }, "a"},
		{"long-s vulns key", func(int) (int, string) { return http.StatusOK, `{"results":[{"vulnſ":[{"id":"MAL-1"}]},{}]}` }, "a"},
		{"repeated results key", func(int) (int, string) {
			return http.StatusOK, `{"results":[{"vulns":[{"id":"MAL-1"}]},{}],"results":[{},{}]}`
		}, "a,b"},
		{"long-s results key", func(int) (int, string) { return http.StatusOK, `{"results":[{},{}],"reſults":[{},{}]}` }, "a,b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := &batchServer{batchBody: tt.body}
			c := newBatchClient(t, b)
			c.PrimeNoAdvisories(context.Background(), refs("a", "b"))
			queryAll(t, c, "a", "b")
			if got := strings.Join(b.queried, ","); got != tt.want {
				t.Errorf("queried %q, want %q", got, tt.want)
			}
		})
	}
}

// OSV.dev accepts at most 1,000 queries per batch; duplicates and packages
// already in the cache are not sent.
func TestPrimeNoAdvisories_SplitsAtTheBatchLimitAndSkipsKnownPackages(t *testing.T) {
	t.Parallel()
	b := &batchServer{batchBody: func(n int) (int, string) {
		return http.StatusOK, `{"results":[` + strings.TrimSuffix(strings.Repeat(`{},`, n), ",") + `]}`
	}}
	c := newBatchClient(t, b)
	queryAll(t, c, "cached")

	var names []string
	for i := 0; i < 1001; i++ {
		names = append(names, fmt.Sprintf("p%d", i))
	}
	pkgs := append(refs(names...), refs("p0", "cached", " ")...)
	c.PrimeNoAdvisories(context.Background(), pkgs)

	if fmt.Sprint(b.batches) != "[1000 1]" {
		t.Errorf("batch sizes %v, want [1000 1]", b.batches)
	}
}

// A batch answer that arrives after a concurrent QueryPackage stored records
// for the same package must not hide them: the records win.
func TestPrimeNoAdvisories_LateBatchAnswerDoesNotHideRecords(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	b := &batchServer{batchBody: func(int) (int, string) {
		<-release
		return http.StatusOK, `{"results":[{}]}`
	}}
	c := newBatchClient(t, b)

	done := make(chan struct{})
	go func() {
		c.PrimeNoAdvisories(context.Background(), refs("atty"))
		close(done)
	}()
	// Wait until the batch is in flight, then store atty's records.
	deadline := time.After(5 * time.Second)
	for {
		b.mu.Lock()
		n := len(b.batches)
		b.mu.Unlock()
		if n == 1 {
			break
		}
		select {
		case <-deadline:
			close(release)
			t.Fatal("batch request was never sent")
		case <-time.After(time.Millisecond):
		}
	}
	if got := queryAll(t, c, "atty")["atty"]; got != 3 {
		t.Fatalf("records before the batch answered: %d, want 3", got)
	}
	close(release)
	<-done
	if got := queryAll(t, c, "atty")["atty"]; got != 3 {
		t.Errorf("records after a late clean batch answer: %d, want 3", got)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if got := strings.Join(b.queried, ","); got != "atty" {
		t.Errorf("queried %q, want one query for atty served from the cache afterwards", got)
	}
}

// Records cached during a batch and then expired before its late clean answer
// must not leave that answer serving the package as clean: the answer is as
// old as its request, so it expires no later than the records did.
func TestPrimeNoAdvisories_LateAnswerAfterRecordsExpired(t *testing.T) {
	t.Parallel()
	const ttl = 150 * time.Millisecond
	release := make(chan struct{})
	b := &batchServer{batchBody: func(int) (int, string) {
		<-release
		return http.StatusOK, `{"results":[{}]}`
	}}
	c := newBatchClient(t, b)
	c.SetCacheTTL(ttl)

	done := make(chan struct{})
	go func() {
		c.PrimeNoAdvisories(context.Background(), refs("atty"))
		close(done)
	}()
	deadline := time.After(5 * time.Second)
	for {
		b.mu.Lock()
		n := len(b.batches)
		b.mu.Unlock()
		if n == 1 {
			break
		}
		select {
		case <-deadline:
			close(release)
			t.Fatal("batch request was never sent")
		case <-time.After(time.Millisecond):
		}
	}
	if got := queryAll(t, c, "atty")["atty"]; got != 3 {
		t.Fatalf("records before the batch answered: %d, want 3", got)
	}
	time.Sleep(2 * ttl) // the records expire while the batch is still out
	close(release)
	<-done
	if got := queryAll(t, c, "atty")["atty"]; got != 3 {
		t.Errorf("records after they expired and a late clean answer arrived: %d, want 3", got)
	}
}

// Overlapping requests for one package answer out of order. Each case takes
// tickets, then stores answers in the listed order; the cache must end up
// serving what the case names. An answer naming an advisory must survive any
// empty answer, and a batch mark must survive until a query answers.
func TestStoreAnswer_AdvisoryAnswersWin(t *testing.T) {
	t.Parallel()
	type answer struct {
		send      int // index into the sent tickets
		advisory  bool
		batch     bool
		needQuery bool
	}
	tests := []struct {
		name    string
		sent    int
		stores  []answer
		wantHit bool // QueryPackage would serve from cache
		wantMAL bool // and the served answer names MAL-1
	}{
		{"older empty query after newer advisory query", 2,
			[]answer{{send: 1, advisory: true}, {send: 0}}, true, true},
		{"older empty batch after newer batch named an advisory", 2,
			[]answer{{send: 1, batch: true, needQuery: true}, {send: 0, batch: true}}, false, false},
		{"query answer settles a batch mark whatever the order", 2,
			[]answer{{send: 1, batch: true, needQuery: true}, {send: 0}}, true, false},
		{"query records after a newer-numbered batch no-advisory", 2,
			[]answer{{send: 1, batch: true}, {send: 0, advisory: true}}, true, true},
		{"newer batch empty does not replace query records", 2,
			[]answer{{send: 0, advisory: true}, {send: 1, batch: true}}, true, true},
		{"newer empty batch after older batch named an advisory", 2,
			[]answer{{send: 0, batch: true, needQuery: true}, {send: 1, batch: true}}, false, false},
		{"older advisory mark after newer empty batch", 2,
			[]answer{{send: 1, batch: true}, {send: 0, batch: true, needQuery: true}}, false, false},
		{"newer query replaces the needs-query mark", 2,
			[]answer{{send: 0, batch: true, needQuery: true}, {send: 1, advisory: true}}, true, true},
		{"empty query never replaces advisory records", 2,
			[]answer{{send: 0, advisory: true}, {send: 1}}, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewClient()
			const key = "npm/chalk"
			asked := time.Now()
			tickets := make([]uint64, tt.sent)
			for i := range tickets {
				tickets[i], _ = c.nextTicket()
			}
			for _, s := range tt.stores {
				ans := cachedAnswer{ticket: tickets[s.send], needsQuery: s.needQuery, recs: []domain.AdvisoryRecord{}}
				if s.advisory {
					ans.recs = []domain.AdvisoryRecord{{ID: "MAL-1"}}
				}
				c.storeAnswer(key, ans, asked, s.batch)
			}
			got, ok := c.cache.Get(key)
			hit := ok && !got.needsQuery
			if hit != tt.wantHit {
				t.Fatalf("served from cache = %v (%+v), want %v", hit, got, tt.wantHit)
			}
			if hit && (len(got.recs) == 1 && got.recs[0].ID == "MAL-1") != tt.wantMAL {
				t.Errorf("served %+v, want MAL-1 = %v", got.recs, tt.wantMAL)
			}
		})
	}
}

// Advisories found while caching is off are not stored, so turning caching
// back on must not revive an older no-advisory answer.
func TestPrimeNoAdvisories_ClearedWhenTTLChanges(t *testing.T) {
	t.Parallel()
	b := &batchServer{batchBody: func(int) (int, string) {
		return http.StatusOK, `{"results":[{}]}`
	}}
	c := newBatchClient(t, b)
	c.PrimeNoAdvisories(context.Background(), refs("atty"))
	c.SetCacheTTL(0)
	c.SetCacheTTL(time.Minute)
	if got := queryAll(t, c, "atty")["atty"]; got != 3 {
		t.Errorf("records after re-enabling the cache: %d, want 3 from a fresh query", got)
	}
}

// A batch slower than the /v1/query timeout but within batchTimeout still
// answers: the batch must not inherit the shorter per-query timeout.
func TestPrimeNoAdvisories_OutlivesQueryTimeout(t *testing.T) {
	b := &batchServer{batchBody: func(int) (int, string) {
		time.Sleep(200 * time.Millisecond)
		return http.StatusOK, `{"results":[{},{}]}`
	}}
	srv := httptest.NewServer(b.handler(t))
	t.Cleanup(srv.Close)
	c := newClient(50 * time.Millisecond)
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(time.Minute)

	c.PrimeNoAdvisories(context.Background(), refs("a", "b"))
	queryAll(t, c, "a", "b")
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.queried) != 0 {
		t.Errorf("queried %v after a slow but successful batch, want none", b.queried)
	}
}

// A slow or failing batch is sent once, not retried, and the packages are
// then queried one by one as before.
func TestPrimeNoAdvisories_FailureIsNotRetried(t *testing.T) {
	saved := batchTimeout
	t.Cleanup(func() { batchTimeout = saved })
	tests := []struct {
		name    string
		timeout time.Duration
		body    func(int) (int, string)
	}{
		{"server error", saved, func(int) (int, string) { return http.StatusInternalServerError, `{}` }},
		{"slower than the timeout", 50 * time.Millisecond, func(int) (int, string) {
			time.Sleep(200 * time.Millisecond)
			return http.StatusOK, `{"results":[{},{}]}`
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batchTimeout = tt.timeout
			b := &batchServer{batchBody: tt.body}
			c := newBatchClient(t, b)
			start := time.Now()
			c.PrimeNoAdvisories(context.Background(), refs("a", "b"))
			if d := time.Since(start); d > time.Second {
				t.Errorf("prime took %v, want the batch abandoned after one attempt", d)
			}
			queryAll(t, c, "a", "b")
			b.mu.Lock()
			defer b.mu.Unlock()
			if len(b.batches) != 1 {
				t.Errorf("batch requests = %d, want 1 (no retry)", len(b.batches))
			}
			if got := strings.Join(b.queried, ","); got != "a,b" {
				t.Errorf("queried %q, want a,b", got)
			}
		})
	}
}
