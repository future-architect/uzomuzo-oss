package osv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// attyPage is the shape OSV.dev actually returns for the crate atty: one
// GHSA-provenance record, one RustSec "unsound" record, and the RustSec
// "unmaintained" record this feature exists to see. Captured 2026-09-07.
const attyPage = `{"vulns":[
 {"id":"GHSA-g98v-hv3f-hcfr","summary":"atty potential unaligned read","published":"2023-06-30T20:21:59Z",
  "references":[{"type":"WEB","url":"https://github.com/softprops/atty/issues/50"},{"type":"ADVISORY","url":"https://github.com/advisories/GHSA-g98v-hv3f-hcfr"}],
  "affected":[{"package":{"name":"atty","ecosystem":"crates.io"},
   "ranges":[{"type":"SEMVER","events":[{"introduced":"0"},{"last_affected":"0.2.14"}]}],
   "database_specific":{"source":"https://github.com/github/advisory-database/..."}}]},
 {"id":"RUSTSEC-2021-0145","summary":"Potential unaligned read","published":"2021-07-04T12:00:00Z",
  "references":[{"type":"ADVISORY","url":"https://rustsec.org/advisories/RUSTSEC-2021-0145.html"}],
  "affected":[{"package":{"name":"atty","ecosystem":"crates.io"},
   "ranges":[{"type":"SEMVER","events":[{"introduced":"0.0.0-0"}]}],
   "database_specific":{"informational":"unsound"}}]},
 {"id":"RUSTSEC-2024-0375","summary":"atty is unmaintained","published":"2024-09-25T12:00:00Z",
  "references":[{"type":"PACKAGE","url":"https://crates.io/crates/atty"},{"type":"ADVISORY","url":"https://rustsec.org/advisories/RUSTSEC-2024-0375.html"}],
  "affected":[{"package":{"name":"atty","ecosystem":"crates.io"},
   "ranges":[{"type":"SEMVER","events":[{"introduced":"0.0.0-0"}]}],
   "database_specific":{"informational":"unmaintained"}}]}
]}`

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c := NewClient()
	c.SetBaseURL(baseURL)
	c.SetCacheTTL(0)
	return c
}

func recordByID(recs []domain.AdvisoryRecord, id string) *domain.AdvisoryRecord {
	for i := range recs {
		if recs[i].ID == id {
			return &recs[i]
		}
	}
	return nil
}

func TestQueryPackage_DecodesRealAttyShape(t *testing.T) {
	t.Parallel()
	var capturedPath, capturedBody, capturedContentType atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedPath.Store(r.URL.Path)
		capturedBody.Store(string(body))
		capturedContentType.Store(r.Header.Get("Content-Type"))
		_, _ = fmt.Fprint(w, attyPage)
	}))
	defer srv.Close()

	recs, err := newTestClient(t, srv.URL).QueryPackage(context.Background(), "crates.io", "atty")
	if err != nil {
		t.Fatalf("QueryPackage failed: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3", len(recs))
	}

	if got := capturedPath.Load(); got != "/v1/query" {
		t.Errorf("request path: got %q, want /v1/query", got)
	}
	if got := capturedContentType.Load(); got != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", got)
	}
	// The ecosystem sent must be OSV's identifier, not the PURL type "cargo".
	var sent queryRequest
	if err := json.Unmarshal([]byte(capturedBody.Load().(string)), &sent); err != nil {
		t.Fatalf("request body did not decode: %v", err)
	}
	if sent.Package.Ecosystem != "crates.io" || sent.Package.Name != "atty" {
		t.Errorf("request body: got %+v, want {atty crates.io}", sent.Package)
	}
	if sent.PageToken != "" {
		t.Errorf("first page must send no page token, got %q", sent.PageToken)
	}

	um := recordByID(recs, "RUSTSEC-2024-0375")
	if um == nil {
		t.Fatalf("RUSTSEC-2024-0375 missing from %d records", len(recs))
	}
	if um.Summary != "atty is unmaintained" {
		t.Errorf("Summary: got %q", um.Summary)
	}
	// The ADVISORY-typed reference wins over the PACKAGE-typed one that precedes it.
	if want := "https://rustsec.org/advisories/RUSTSEC-2024-0375.html"; um.Reference != want {
		t.Errorf("Reference: got %q, want %q", um.Reference, want)
	}
	if want := time.Date(2024, 9, 25, 12, 0, 0, 0, time.UTC); !um.Published.Equal(want) {
		t.Errorf("Published: got %v, want %v", um.Published, want)
	}
	if um.Withdrawn {
		t.Error("Withdrawn: got true, want false (no withdrawn field in the response)")
	}
	if len(um.Affected) != 1 {
		t.Fatalf("Affected: got %d entries, want 1", len(um.Affected))
	}
	af := um.Affected[0]
	if af.Name != "atty" || af.Ecosystem != "crates.io" {
		t.Errorf("affected package: got %s/%s", af.Ecosystem, af.Name)
	}
	if af.Informational != "unmaintained" {
		t.Errorf("Informational: got %q, want unmaintained", af.Informational)
	}
	if len(af.Ranges) != 1 || af.Ranges[0].Type != "SEMVER" {
		t.Fatalf("Ranges: got %+v", af.Ranges)
	}
	if len(af.Ranges[0].Events) != 1 || af.Ranges[0].Events[0].Introduced != "0.0.0-0" {
		t.Errorf("Events: got %+v", af.Ranges[0].Events)
	}

	// The sibling records must survive decoding too: the domain, not the client,
	// decides that "unsound" and a non-RUSTSEC id do not qualify.
	if un := recordByID(recs, "RUSTSEC-2021-0145"); un == nil || un.Affected[0].Informational != "unsound" {
		t.Errorf("unsound record not preserved: %+v", un)
	}
	ghsa := recordByID(recs, "GHSA-g98v-hv3f-hcfr")
	if ghsa == nil {
		t.Fatalf("GHSA record missing")
	}
	if len(ghsa.Affected[0].Ranges[0].Events) != 2 || ghsa.Affected[0].Ranges[0].Events[1].LastAffected != "0.2.14" {
		t.Errorf("last_affected not decoded: %+v", ghsa.Affected[0].Ranges[0].Events)
	}
}

func TestQueryPackage_WithdrawnIsATimestampNotAFlag(t *testing.T) {
	t.Parallel()
	const page = `{"vulns":[{"id":"RUSTSEC-2025-0007","published":"2025-02-01T12:00:00Z",
	 "withdrawn":"2025-02-03T12:00:00Z",
	 "affected":[{"package":{"name":"ring","ecosystem":"crates.io"},
	  "ranges":[{"type":"SEMVER","events":[{"introduced":"0.0.0-0"}]}],
	  "database_specific":{"informational":"unmaintained"}}]}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, page)
	}))
	defer srv.Close()

	recs, err := newTestClient(t, srv.URL).QueryPackage(context.Background(), "crates.io", "ring")
	if err != nil {
		t.Fatalf("QueryPackage failed: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if !recs[0].Withdrawn {
		t.Error("Withdrawn: got false, want true — the field is an RFC3339 timestamp, and its presence is the retraction")
	}
}

func TestQueryPackage_UnparseableDatesLeavePublishedZero(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		published string
	}{
		{"absent", ""},
		{"not_rfc3339", "25 September 2024"},
		{"date_only", "2024-09-25"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			page := fmt.Sprintf(`{"vulns":[{"id":"RUSTSEC-2024-0375","published":%q}]}`, tc.published)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, page)
			}))
			defer srv.Close()

			recs, err := newTestClient(t, srv.URL).QueryPackage(context.Background(), "crates.io", "atty")
			if err != nil {
				t.Fatalf("QueryPackage failed: %v", err)
			}
			if len(recs) != 1 {
				t.Fatalf("got %d records, want 1", len(recs))
			}
			if !recs[0].Published.IsZero() {
				t.Errorf("Published: got %v, want zero", recs[0].Published)
			}
		})
	}
}

func TestQueryPackage_DropsVulnsWithoutIdentity(t *testing.T) {
	t.Parallel()
	const page = `{"vulns":[
	 {"id":"","summary":"no id"},
	 {"id":"RUSTSEC-2024-0375","affected":[
	   {"package":{"name":"","ecosystem":"crates.io"},"database_specific":{"informational":"unmaintained"}},
	   {"package":{"name":"atty","ecosystem":"crates.io"},"database_specific":{"informational":"unmaintained"}}]}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, page)
	}))
	defer srv.Close()

	recs, err := newTestClient(t, srv.URL).QueryPackage(context.Background(), "crates.io", "atty")
	if err != nil {
		t.Fatalf("QueryPackage failed: %v", err)
	}
	if len(recs) != 1 || recs[0].ID != "RUSTSEC-2024-0375" {
		t.Fatalf("got %+v, want only RUSTSEC-2024-0375", recs)
	}
	// An affected entry naming no package cannot be matched against the queried
	// one, so it must not enter the set the domain quantifies over.
	if len(recs[0].Affected) != 1 || recs[0].Affected[0].Name != "atty" {
		t.Errorf("Affected: got %+v, want only the atty entry", recs[0].Affected)
	}
}

func TestQueryPackage_DropsVulnsWithMalformedID(t *testing.T) {
	t.Parallel()
	// The ID is printed verbatim in the lifecycle reason and a CLI signal, so
	// an ID carrying escape, bidi, whitespace or other punctuation is rejected
	// rather than repaired: a repaired ID would name an advisory that does not
	// exist.
	const page = `{"vulns":[
	 {"id":"RUSTSEC-2024-0375\u001b[31m"},
	 {"id":"RUSTSEC-2024\u202e-0376"},
	 {"id":"RUSTSEC-2024-0377\nforged line"},
	 {"id":"RUSTSEC/../2024-0378"},
	 {"id":"  RUSTSEC-2024-0379  "}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, page)
	}))
	defer srv.Close()

	recs, err := newTestClient(t, srv.URL).QueryPackage(context.Background(), "crates.io", "atty")
	if err != nil {
		t.Fatalf("QueryPackage failed: %v", err)
	}
	if len(recs) != 1 || recs[0].ID != "RUSTSEC-2024-0379" {
		t.Fatalf("got %+v, want only RUSTSEC-2024-0379 (surrounding whitespace trimmed)", recs)
	}
}

func TestQueryPackage_DecodesAliases(t *testing.T) {
	t.Parallel()
	const page = `{"vulns":[{"id":"RUSTSEC-2022-0054",
	 "aliases":["GHSA-rc23-xxgq-x27g"," CVE-2022-0001 ","GHSA-bad\u001b[31m",""]}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, page)
	}))
	defer srv.Close()

	recs, err := newTestClient(t, srv.URL).QueryPackage(context.Background(), "crates.io", "wee_alloc")
	if err != nil {
		t.Fatalf("QueryPackage failed: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	// Malformed aliases are dropped for the same reason malformed IDs are;
	// dropping an alias only costs an exclusion, never a false match.
	if want := []string{"GHSA-rc23-xxgq-x27g", "CVE-2022-0001"}; !slices.Equal(recs[0].Aliases, want) {
		t.Errorf("Aliases: got %v, want %v", recs[0].Aliases, want)
	}
}

func TestQueryPackage_UnrecognizedEventsAreNotSilentlyDropped(t *testing.T) {
	t.Parallel()
	// A future event key, a known key sharing an object with an unknown one,
	// and an empty object must each reach the domain as an event with no
	// recognized field, so the package-wide check rejects the range instead
	// of reading it as open-ended.
	const page = `{"vulns":[{"id":"RUSTSEC-2024-0375","affected":[{"package":{"name":"atty","ecosystem":"crates.io"},
	 "ranges":[{"type":"SEMVER","events":[
	   {"introduced":"0"},
	   {"future_bound":"1.0.0"},
	   {"introduced":"0","future_bound":"1.0.0"},
	   {}]}]}]}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, page)
	}))
	defer srv.Close()

	recs, err := newTestClient(t, srv.URL).QueryPackage(context.Background(), "crates.io", "atty")
	if err != nil {
		t.Fatalf("QueryPackage failed: %v", err)
	}
	if len(recs) != 1 || len(recs[0].Affected) != 1 || len(recs[0].Affected[0].Ranges) != 1 {
		t.Fatalf("unexpected record shape: %+v", recs)
	}
	want := []domain.AdvisoryRangeEvent{{Introduced: "0"}, {}, {}, {}}
	if got := recs[0].Affected[0].Ranges[0].Events; !slices.Equal(got, want) {
		t.Errorf("Events: got %+v, want %+v", got, want)
	}
}

func TestQueryPackage_SanitizesSummary(t *testing.T) {
	t.Parallel()
	// \u001b is the ANSI escape introducer and \u200b a zero-width space; both
	// reach the terminal verbatim if the summary is printed unfiltered.
	const page = `{"vulns":[{"id":"RUSTSEC-2024-0375","summary":"  atty\u001b[31m is\n\n  unmaintained\u200b  "}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, page)
	}))
	defer srv.Close()

	recs, err := newTestClient(t, srv.URL).QueryPackage(context.Background(), "crates.io", "atty")
	if err != nil {
		t.Fatalf("QueryPackage failed: %v", err)
	}
	if want := "atty[31m is unmaintained"; recs[0].Summary != want {
		t.Errorf("Summary: got %q, want %q", recs[0].Summary, want)
	}
}

func TestQueryPackage_CachesNegativeResult(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = fmt.Fprint(w, `{"vulns":[]}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(time.Minute)

	for i := 0; i < 2; i++ {
		recs, err := c.QueryPackage(context.Background(), "crates.io", "serde")
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
		if len(recs) != 0 {
			t.Fatalf("call %d: got %d records, want 0", i, len(recs))
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("HTTP calls: got %d, want 1 (a clean package must be cached, not re-asked)", got)
	}
}

func TestQueryPackage_CacheKeyIsCaseSensitive(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = fmt.Fprint(w, `{"vulns":[]}`)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(time.Minute)

	// OSV matches crates.io names case-sensitively, so case variants are
	// different packages: sharing a cache entry between them would answer one
	// name with another name's advisories.
	for _, name := range []string{"Atty", "atty", "ATTY", "atty"} {
		if _, err := c.QueryPackage(context.Background(), "crates.io", name); err != nil {
			t.Fatalf("QueryPackage(%q) failed: %v", name, err)
		}
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("HTTP calls: got %d, want 3 (one per distinct spelling, the repeat served from cache)", got)
	}
}

func TestQueryPackage_EmptyArgumentsIssueNoRequest(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = fmt.Fprint(w, `{"vulns":[]}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	for _, tc := range []struct{ eco, name string }{{"", "atty"}, {"crates.io", ""}, {"  ", "  "}} {
		recs, err := c.QueryPackage(context.Background(), tc.eco, tc.name)
		if err != nil || recs != nil {
			t.Errorf("QueryPackage(%q,%q) = (%v,%v), want (nil,nil)", tc.eco, tc.name, recs, err)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("HTTP calls: got %d, want 0", got)
	}
}

func TestQueryPackage_Pagination(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req queryRequest
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		switch req.PageToken {
		case "":
			_, _ = fmt.Fprint(w, `{"vulns":[{"id":"RUSTSEC-2024-0375"}],"next_page_token":"p2"}`)
		case "p2":
			// The lexicographically smaller advisory arrives last, so a caller
			// that stopped at page one would pick the wrong evidence.
			_, _ = fmt.Fprint(w, `{"vulns":[{"id":"RUSTSEC-2021-0001"}]}`)
		default:
			t.Errorf("unexpected page token %q", req.PageToken)
		}
	}))
	defer srv.Close()

	recs, err := newTestClient(t, srv.URL).QueryPackage(context.Background(), "crates.io", "atty")
	if err != nil {
		t.Fatalf("QueryPackage failed: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2 (both pages)", len(recs))
	}
	if recordByID(recs, "RUSTSEC-2021-0001") == nil {
		t.Error("the second page's record is missing")
	}
}

// An incomplete walk must be an error, never a cached negative: a page cap hit
// or a stuck token would otherwise turn "we did not finish looking" into "this
// package has no advisories".
func TestQueryPackage_IncompleteWalkErrorsAndCachesNothing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		handler http.HandlerFunc
		wantSub string
	}{
		{
			name: "page_cap_exhausted",
			handler: func(w http.ResponseWriter, r *http.Request) {
				var req queryRequest
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &req)
				// A fresh token every time: the walk never terminates on its own.
				_, _ = fmt.Fprintf(w, `{"vulns":[],"next_page_token":"p%s0"}`, req.PageToken)
			},
			wantSub: "exceeded",
		},
		{
			name: "repeated_page_token",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, `{"vulns":[],"next_page_token":"stuck"}`)
			},
			wantSub: "repeated a page token",
		},
		{
			name: "later_page_transport_failure",
			handler: func(w http.ResponseWriter, r *http.Request) {
				var req queryRequest
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &req)
				if req.PageToken == "" {
					_, _ = fmt.Fprint(w, `{"vulns":[{"id":"RUSTSEC-2024-0375"}],"next_page_token":"p2"}`)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantSub: "request failed after all retries",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				tc.handler(w, r)
			}))
			defer srv.Close()

			c := NewClient()
			c.SetBaseURL(srv.URL)
			c.SetCacheTTL(time.Minute)

			recs, err := c.QueryPackage(context.Background(), "crates.io", "atty")
			if err == nil {
				t.Fatalf("expected an error, got %d records", len(recs))
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not mention %q", err, tc.wantSub)
			}
			if recs != nil {
				t.Errorf("records must be nil on error, got %+v", recs)
			}
			before := calls.Load()
			if _, err := c.QueryPackage(context.Background(), "crates.io", "atty"); err == nil {
				t.Fatal("second call unexpectedly succeeded — a failed walk was cached")
			}
			if calls.Load() == before {
				t.Error("second call issued no request — a failed walk must not be cached")
			}
		})
	}
}

func TestQueryPackage_TransportFailuresAreUnknownNotNegative(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		serve   http.HandlerFunc
		cancel  bool
		wantSub string
	}{
		{
			name:  "server_error_after_retries",
			serve: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
			// The shared client retries 5xx and surfaces its own exhaustion error,
			// so the status never reaches this package's own status check.
			wantSub: "request failed after all retries",
		},
		{
			name:    "bad_gateway",
			serve:   func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) },
			wantSub: "request failed after all retries",
		},
		{
			name:    "not_found_is_an_error_not_an_empty_answer",
			serve:   func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) },
			wantSub: "http status 404",
		},
		{
			name:    "malformed_json_200",
			serve:   func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, `{"vulns":[`) },
			wantSub: "decode failed",
		},
		{
			name:    "html_error_page_200",
			serve:   func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, `<html>nope</html>`) },
			wantSub: "not a JSON object",
		},
		{
			// `null` unmarshals into a zero queryResponse without error, which
			// would otherwise be cached as "this package has no advisories".
			name:    "json_null_200",
			serve:   func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, `null`) },
			wantSub: "not a JSON object",
		},
		{
			name:    "json_array_200",
			serve:   func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, `[]`) },
			wantSub: "not a JSON object",
		},
		{
			name: "oversized_body",
			serve: func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprint(w, `{"vulns":[{"id":"X","summary":"`)
				chunk := strings.Repeat("a", 1<<16)
				for written := 0; written <= maxJSONResponseSize; written += len(chunk) {
					_, _ = io.WriteString(w, chunk)
				}
				_, _ = fmt.Fprint(w, `"}]}`)
			},
			wantSub: "exceeded",
		},
		{
			name:    "context_cancelled",
			serve:   func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, `{"vulns":[]}`) },
			cancel:  true,
			wantSub: "context canceled",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tc.serve)
			defer srv.Close()

			c := NewClient()
			c.SetBaseURL(srv.URL)
			c.SetCacheTTL(time.Minute)

			ctx, cancel := context.WithCancel(context.Background())
			if tc.cancel {
				cancel()
			} else {
				defer cancel()
			}

			recs, err := c.QueryPackage(ctx, "crates.io", "atty")
			if err == nil {
				t.Fatalf("expected an error, got %d records", len(recs))
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not mention %q", err, tc.wantSub)
			}
			if recs != nil {
				t.Errorf("records must be nil on error, got %+v", recs)
			}
			// Nothing may be cached: the next caller must ask again rather than
			// inherit a failure as "no advisories".
			if _, ok := c.cache.Get("crates.io/atty"); ok {
				t.Error("a failed lookup was written to the cache")
			}
		})
	}
}

// The query is this codebase's first POST through the retrying HTTP client.
// A retry that replayed an empty body would silently query a different package.
func TestQueryPackage_RetryReplaysTheRequestBody(t *testing.T) {
	t.Parallel()
	var bodies atomic.Value
	bodies.Store([]string{})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen := append(append([]string{}, bodies.Load().([]string)...), string(body))
		bodies.Store(seen)
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, `{"vulns":[]}`)
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv.URL).QueryPackage(context.Background(), "crates.io", "atty"); err != nil {
		t.Fatalf("QueryPackage failed: %v", err)
	}
	seen := bodies.Load().([]string)
	if len(seen) < 2 {
		t.Fatalf("expected at least 2 attempts, got %d", len(seen))
	}
	for i, b := range seen {
		var req queryRequest
		if err := json.Unmarshal([]byte(b), &req); err != nil {
			t.Fatalf("attempt %d body did not decode (%q): %v", i, b, err)
		}
		if req.Package.Name != "atty" || req.Package.Ecosystem != "crates.io" {
			t.Errorf("attempt %d carried %+v, want the full body", i, req.Package)
		}
	}
}

func TestAdvisoryReference(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		refs []wireReference
		want string
	}{
		{"advisory_type_wins", []wireReference{{Type: "PACKAGE", URL: "https://crates.io/crates/atty"}, {Type: "ADVISORY", URL: "https://rustsec.org/a.html"}}, "https://rustsec.org/a.html"},
		{"advisory_type_case_insensitive", []wireReference{{Type: "advisory", URL: "https://rustsec.org/a.html"}}, "https://rustsec.org/a.html"},
		{"falls_back_to_first_url", []wireReference{{Type: "WEB", URL: "https://example.com/w"}, {Type: "REPORT", URL: "https://example.com/r"}}, "https://example.com/w"},
		{"skips_empty_urls", []wireReference{{Type: "WEB", URL: "  "}, {Type: "REPORT", URL: "https://example.com/r"}}, "https://example.com/r"},
		{"none", nil, ""},
		// The URL is evidence a reader is expected to open, so anything that is
		// not a plain http(s) URL is dropped rather than passed through.
		{"rejects_javascript_scheme", []wireReference{{Type: "ADVISORY", URL: "javascript:alert(1)"}}, ""},
		{"rejects_data_scheme", []wireReference{{Type: "WEB", URL: "data:text/html,x"}}, ""},
		{"rejects_control_characters", []wireReference{{Type: "ADVISORY", URL: "https://rustsec.org/a\u001b[31m"}}, ""},
		{"falls_back_past_a_rejected_url", []wireReference{{Type: "ADVISORY", URL: "javascript:alert(1)"}, {Type: "WEB", URL: "https://rustsec.org/a.html"}}, "https://rustsec.org/a.html"},
		{"keeps_http", []wireReference{{Type: "ADVISORY", URL: "http://rustsec.org/a.html"}}, "http://rustsec.org/a.html"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := advisoryReference(tc.refs); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSetBaseURL_TrimsTrailingSlash(t *testing.T) {
	t.Parallel()
	c := NewClient()
	c.SetBaseURL("https://example.test/")
	if got := c.resolvedBaseURL(); got != "https://example.test" {
		t.Errorf("got %q, want https://example.test", got)
	}
	c.SetBaseURL("")
	if got := c.resolvedBaseURL(); got != "https://api.osv.dev" {
		t.Errorf("empty base URL must fall back to the default, got %q", got)
	}
}

func TestSetHTTPClient_IgnoresNil(t *testing.T) {
	t.Parallel()
	c := NewClient()
	before := c.http
	c.SetHTTPClient(nil)
	if c.http != before {
		t.Error("SetHTTPClient(nil) must not replace the configured client")
	}
}
