package osv

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

type byIDRoundTripper func(*http.Request) (*http.Response, error)

func (f byIDRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGetVulnCVSS4(t *testing.T) {
	const full = "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:H/SI:H/SA:H/E:U"
	const none = "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:N/VI:N/VA:N/SC:N/SI:N/SA:N"
	const vcc3 = "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:N/VI:N/VA:H/SC:N/SI:N/SA:N/E:U/S:N/AU:Y/R:U/V:D/RE:M/U:Amber"
	const ph9p = "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:N/VA:N/SC:N/SI:N/SA:N/E:P"
	for _, tc := range []struct {
		name, id, severity, reviewed, band string
		score                              float64
		known                              bool
	}{
		{"GHSA-vcc3 published OSV", "GHSA-vcc3-ghjq-m6fr", `[{"type":"CVSS_V4","score":"` + vcc3 + `"}]`, "MODERATE", "MEDIUM", 6.6, true},
		{"GHSA-ph9p published OSV", "GHSA-ph9p-34f9-6g65", `[{"type":"CVSS_V4","score":"` + ph9p + `"}]`, "HIGH", "HIGH", 7.7, true},
		{"published threat", "GHSA-cvss4-test", `[{"type":"CVSS_V4","score":"` + full + `"}]`, "", "CRITICAL", 9.1, true},
		{"none", "GHSA-cvss4-test", `[{"type":"CVSS_V4","score":"` + none + `"}]`, "", "NONE", 0, true},
		{"malformed top level", "GHSA-cvss4-test", `{"score":"bad"}`, "", "", 0, false},
		{"invalid vector", "GHSA-cvss4-test", `[{"type":"CVSS_V4","score":"CVSS:4.0/INVALID"}]`, "", "", 0, false},
		{"duplicate v4 vectors", "GHSA-cvss4-test", `[{"type":"CVSS_V4","score":"` + full + `"},{"type":"CVSS_V4","score":"` + none + `"}]`, "", "", 0, false},
		{"duplicate metric", "GHSA-cvss4-test", `[{"type":"CVSS_V4","score":"` + none + `/AV:N"}]`, "", "", 0, false},
		{"v3 only", "GHSA-cvss4-test", `[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N"}]`, "", "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient()
			c.SetHTTPClient(&http.Client{Transport: byIDRoundTripper(func(r *http.Request) (*http.Response, error) {
				body := `{"id":"` + tc.id + `","database_specific":{"severity":"` + tc.reviewed + `"},"severity":` + tc.severity + `}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})})
			rec, found, err := c.GetVuln(context.Background(), tc.id)
			if err != nil || !found || rec.CVSS4Known != tc.known || rec.CVSS4Score != tc.score || rec.Severity != tc.reviewed || domain.SeverityFromScore(rec.CVSS4Score, rec.CVSS4Known) != tc.band {
				t.Fatalf("GetVuln: rec=%+v found=%v err=%v; want score=%v known=%v band=%s reviewed=%s", rec, found, err, tc.score, tc.known, tc.band, tc.reviewed)
			}
		})
	}
}

func TestGetVulnAmbiguousSeverityKey(t *testing.T) {
	const id = "GHSA-ambiguous-severity"
	const zero = "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:N/VI:N/VA:N/SC:N/SI:N/SA:N"
	const high = "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:H/SI:H/SA:H"
	for _, tc := range []struct{ name, field string }{
		{"duplicate key", `"severity":[],"severity":[{"type":"CVSS_V4","score":"` + zero + `"}]`},
		{"variant spelling", `"Severity":[{"type":"CVSS_V4","score":"` + zero + `"}]`},
		{"duplicate score in entry", `"severity":[{"type":"CVSS_V4","score":"` + high + `","score":"` + zero + `"}]`},
		{"variant score in entry", `"severity":[{"type":"CVSS_V4","score":"` + high + `","Score":"` + zero + `"}]`},
		{"variant type in entry", `"severity":[{"type":"CVSS_V3","Type":"CVSS_V4","score":"` + zero + `"}]`},
		{"long s in entry key", `"severity":[{"type":"CVSS_V4","score":"` + high + `","ſcore":"` + zero + `"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient()
			var calls atomic.Int32
			c.SetHTTPClient(&http.Client{Transport: byIDRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				body := `{"id":"` + id + `",` + tc.field + `}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})})
			rec, found, err := c.GetVuln(context.Background(), id)
			if err != nil || !found || rec.CVSS4Known || rec.CVSS4Score != 0 || calls.Load() != 1 {
				t.Fatalf("record=%+v found=%v err=%v calls=%d", rec, found, err, calls.Load())
			}
		})
	}
}

func TestMalformedTopLevelSeverityDoesNotBreakPackageResponses(t *testing.T) {
	const malicious = `{"id":"MAL-2026-0001","summary":"Malicious package","severity":{"unexpected":"shape"},"affected":[{"package":{"name":"evil","ecosystem":"npm"},"versions":["1.0.0"]}]}`
	var query queryResponse
	if err := json.Unmarshal([]byte(`{"vulns":[`+malicious+`]}`), &query); err != nil {
		t.Fatal(err)
	}
	if len(query.Vulns) != 1 {
		t.Fatalf("query records=%d", len(query.Vulns))
	}
	if rec, ok := toRecord(&query.Vulns[0]); !ok || rec.ID != "MAL-2026-0001" {
		t.Fatalf("query record=%+v ok=%v", rec, ok)
	}
	batch, err := decodeBatchResults([]byte(`{"results":[{"vulns":[` + malicious + `]}]}`))
	if err != nil || len(batch) != 1 || batch[0].clean {
		t.Fatalf("batch=%v err=%v", batch, err)
	}
}

func TestClient_GetVulnRejectsInvalidWithdrawalEvidence(t *testing.T) {
	const id = "GHSA-invalid-evidence"
	for _, tc := range []struct {
		name string
		body string
	}{
		{"invalid timestamp", `{"id":"GHSA-invalid-evidence","withdrawn":"not-a-timestamp"}`},
		{"duplicate withdrawn", `{"id":"GHSA-invalid-evidence","withdrawn":"2026-10-22T19:21:04Z","withdrawn":"2026-10-23T00:00:00Z"}`},
		{"case variant withdrawn", `{"id":"GHSA-invalid-evidence","Withdrawn":"2026-10-22T19:21:04Z"}`},
		{"duplicate id", `{"id":"GHSA-other","id":"GHSA-invalid-evidence","withdrawn":"2026-10-22T19:21:04Z"}`},
		{"case variant id", `{"ID":"GHSA-invalid-evidence","withdrawn":"2026-10-22T19:21:04Z"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			c := NewClient()
			c.SetHTTPClient(&http.Client{Transport: byIDRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/v1/vulns/"+id {
					t.Errorf("path=%q", r.URL.Path)
				}
				calls.Add(1)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header), Request: r}, nil
			})})
			for range 2 {
				if rec, found, err := c.GetVuln(context.Background(), id); err == nil || found || rec.Withdrawn {
					t.Errorf("GetVuln: rec=%+v found=%v err=%v, want uncached failure", rec, found, err)
				}
			}
			if got := calls.Load(); got != 2 {
				t.Errorf("calls=%d, want 2 because failures are not cached", got)
			}
		})
	}
}

func TestClient_GetVulnAcceptsNullOrEmptyWithdrawal(t *testing.T) {
	const id = "GHSA-unwithdrawn"
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"null", "null"},
		{"empty", `""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			c := NewClient()
			c.SetHTTPClient(&http.Client{Transport: byIDRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				body := `{"id":"GHSA-unwithdrawn","published":"2018-04-26T17:21:53Z","withdrawn":` + tc.value + `,"database_specific":{"severity":"HIGH"}}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})})
			for range 2 {
				rec, found, err := c.GetVuln(context.Background(), id)
				if err != nil || !found || rec.Withdrawn || rec.Published.IsZero() || rec.Severity != "HIGH" {
					t.Errorf("GetVuln: rec=%+v found=%v err=%v", rec, found, err)
				}
			}
			if got := calls.Load(); got != 1 {
				t.Errorf("OSV calls=%d, want 1 cached valid answer", got)
			}
		})
	}
}

func TestClient_GetVuln(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/v1/vulns/GHSA-jp4x-w63m-7wgm":
			_, _ = w.Write([]byte(`{"id":"GHSA-jp4x-w63m-7wgm","published":"2018-04-26T17:21:53Z","summary":"hoek prototype pollution","database_specific":{"severity":"moderate"}}`))
		case "/v1/vulns/GHSA-hp3w-g68c-fv3c":
			_, _ = w.Write([]byte(`{"id":"GHSA-hp3w-g68c-fv3c","published":"2026-09-24T00:00:00Z","withdrawn":"2026-10-22T19:21:04Z"}`))
		case "/v1/vulns/GHSA-other-answer":
			_, _ = w.Write([]byte(`{"id":"GHSA-not-what-was-asked","published":"2020-01-01T00:00:00Z"}`))
		case "/v1/vulns/GHSA-odd-severity":
			_, _ = w.Write([]byte(`{"id":"GHSA-odd-severity","published":"2020-01-01T00:00:00Z","database_specific":{"severity":{"level":"high"}}}`))
		case "/v1/vulns/GHSA-null-severity":
			_, _ = w.Write([]byte(`{"id":"GHSA-null-severity","published":"2020-01-01T00:00:00Z","database_specific":{"severity":null}}`))
		case "/v1/vulns/GHSA-server-error":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv.URL)
	c.SetCacheTTL(time.Minute)
	ctx := context.Background()

	rec, found, err := c.GetVuln(ctx, "GHSA-jp4x-w63m-7wgm")
	if err != nil || !found {
		t.Fatalf("GetVuln: found=%v err=%v", found, err)
	}
	if want := time.Date(2018, 4, 26, 17, 21, 53, 0, time.UTC); !rec.Published.Equal(want) {
		t.Errorf("Published: got %v, want %v", rec.Published, want)
	}
	if rec.Withdrawn {
		t.Errorf("Withdrawn: got true, want false")
	}
	if rec.Severity != "MODERATE" {
		t.Errorf("Severity: got %q, want MODERATE", rec.Severity)
	}

	rec, found, err = c.GetVuln(ctx, "GHSA-hp3w-g68c-fv3c")
	if err != nil || !found || !rec.Withdrawn {
		t.Fatalf("withdrawn record: found=%v withdrawn=%v err=%v", found, rec.Withdrawn, err)
	}

	if _, found, err = c.GetVuln(ctx, "GHSA-xxxx-xxxx-xxxx"); err != nil || found {
		t.Fatalf("404: found=%v err=%v, want not found and no error", found, err)
	}
	// A severity that is not a string is an unrecognized rating: not a failed
	// lookup, and not "no rating" either.
	if rec, found, err = c.GetVuln(ctx, "GHSA-odd-severity"); err != nil || !found || rec.Severity != unrecognizedRating {
		t.Fatalf("non-string severity: found=%v severity=%q err=%v, want found, %s, no error", found, rec.Severity, err, unrecognizedRating)
	}
	if rec, found, err = c.GetVuln(ctx, "GHSA-null-severity"); err != nil || !found || rec.Severity != unrecognizedRating {
		t.Fatalf("null severity: found=%v severity=%q err=%v, want found, %s, no error", found, rec.Severity, err, unrecognizedRating)
	}
	// An omitted severity is no rating (hp3w above has no database_specific).
	if rec, _, _ = c.GetVuln(ctx, "GHSA-hp3w-g68c-fv3c"); rec.Severity != "" {
		t.Errorf("omitted severity: got %q, want empty", rec.Severity)
	}
	if _, _, err = c.GetVuln(ctx, "GHSA-other-answer"); err == nil {
		t.Errorf("an answer naming another ID must be an error")
	}

	// Cached: the second lookup of a found and a 404 ID sends no request.
	before := calls.Load()
	_, _, _ = c.GetVuln(ctx, "GHSA-jp4x-w63m-7wgm")
	_, _, _ = c.GetVuln(ctx, "GHSA-xxxx-xxxx-xxxx")
	if got := calls.Load(); got != before {
		t.Errorf("cached lookups sent %d requests, want 0", got-before)
	}
	// SetCacheTTL drops the cached answers, so a later withdrawal or a record
	// published after a 404 is seen.
	c.SetCacheTTL(time.Minute)
	before = calls.Load()
	_, _, _ = c.GetVuln(ctx, "GHSA-jp4x-w63m-7wgm")
	_, _, _ = c.GetVuln(ctx, "GHSA-xxxx-xxxx-xxxx")
	if got := calls.Load() - before; got != 2 {
		t.Errorf("after SetCacheTTL: %d requests, want 2", got)
	}
	// A disabled cache asks every time.
	c.SetCacheTTL(0)
	before = calls.Load()
	_, _, _ = c.GetVuln(ctx, "GHSA-jp4x-w63m-7wgm")
	_, _, _ = c.GetVuln(ctx, "GHSA-jp4x-w63m-7wgm")
	if got := calls.Load() - before; got != 2 {
		t.Errorf("with caching off: %d requests, want 2", got)
	}
	// Not cached, even with caching on: an error is retried on the next call.
	c.SetCacheTTL(time.Minute)
	_, _, err = c.GetVuln(ctx, "GHSA-server-error")
	if err == nil {
		t.Fatalf("500: want error")
	}
	n := calls.Load()
	_, _, _ = c.GetVuln(ctx, "GHSA-server-error")
	if calls.Load() == n {
		t.Errorf("an error answer must not be cached")
	}
}

// TestClient_GetVuln_ResetDuringRequest checks that an answer to a request
// sent before SetCacheTTL is not stored after it: the reset must not be undone
// by a slow response, for a found record and for a 404 alike.
func TestClient_GetVuln_ResetDuringRequest(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, id, body string }{
		{"found", "GHSA-slow-found", `{"id":"GHSA-slow-found","published":"2018-04-26T17:21:53Z"}`},
		{"not found", "GHSA-slow-missing", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			arrived := make(chan struct{}, 4)
			release := make(chan struct{})
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					arrived <- struct{}{}
					<-release
				}
				if tc.body == "" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			c := newTestClient(t, srv.URL)
			c.SetCacheTTL(time.Minute)

			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _, _ = c.GetVuln(context.Background(), tc.id)
			}()
			<-arrived
			c.SetCacheTTL(time.Minute)
			close(release)
			<-done

			_, _, _ = c.GetVuln(context.Background(), tc.id)
			if got := calls.Load(); got != 2 {
				t.Errorf("requests: got %d, want 2 (the answer to the request sent before the reset must not be cached)", got)
			}
		})
	}
}
