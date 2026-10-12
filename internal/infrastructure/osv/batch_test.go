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
		b.mu.Lock()
		defer b.mu.Unlock()
		switch r.URL.Path {
		case "/v1/querybatch":
			b.batches = append(b.batches, len(body.Queries))
			status, out := b.batchBody(len(body.Queries))
			w.WriteHeader(status)
			_, _ = fmt.Fprint(w, out)
		case "/v1/query":
			b.queried = append(b.queried, body.Package.Name)
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
