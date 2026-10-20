package rdap

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// TestLookupFailedIsNotPersisted checks recovery on a new client.
func TestLookupFailedIsNotPersisted(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var status atomic.Int32
	status.Store(http.StatusServiceUnavailable)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if status.Load() == http.StatusOK {
			_, _ = w.Write([]byte(`{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}]}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	cacheDir := t.TempDir()
	client := NewClient()
	client.SetBaseURL("net", srv.URL)
	client.SetCacheDir(cacheDir)
	client.SetRetryDelay(0)
	client.SetRequestInterval(0)
	first, err := client.Lookup(context.Background(), "atlantis-software.net")
	if err == nil || first.Status != analysis.DomainLookupFailed || calls.Load() != 2 {
		t.Fatalf("first=%+v err=%v calls=%d", first, err, calls.Load())
	}
	if _, err := os.Stat(client.cachePath("atlantis-software.net")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("failed lookup cache entry exists or cannot be checked: %v", err)
	}
	status.Store(http.StatusOK)
	client = NewClient()
	client.SetBaseURL("net", srv.URL)
	client.SetCacheDir(cacheDir)
	client.SetRequestInterval(0)
	second, err := client.Lookup(context.Background(), "atlantis-software.net")
	if err != nil || second.Status != analysis.DomainFound || calls.Load() != 3 {
		t.Fatalf("second=%+v err=%v calls=%d, want found after a new request", second, err, calls.Load())
	}
}

// TestLookupDiskCacheEvidenceAndLegacyEntry checks evidence and old cache entries.
func TestLookupDiskCacheEvidenceAndLegacyEntry(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}]}`))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	c := NewClient()
	c.SetBaseURL("net", srv.URL)
	c.SetCacheDir(dir)
	c.SetRequestInterval(0)
	before := time.Now().UTC().Add(-time.Second)
	if rec, err := c.Lookup(context.Background(), "atlantis-software.net"); err != nil || rec.Status != analysis.DomainFound {
		t.Fatalf("record=%+v err=%v", rec, err)
	}
	data, err := os.ReadFile(c.cachePath("atlantis-software.net"))
	if err != nil {
		t.Fatal(err)
	}
	var entry struct {
		RequestURL string `json:"request_url"`
		FetchedAt  string `json:"fetched_at"`
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	if want := srv.URL + "/domain/atlantis-software.net"; entry.RequestURL != want {
		t.Errorf("request_url=%q, want %q", entry.RequestURL, want)
	}
	fetched, err := time.Parse(time.RFC3339, entry.FetchedAt)
	if err != nil || fetched.Before(before) || fetched.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("fetched_at=%q err=%v", entry.FetchedAt, err)
	}
	other := NewClient()
	other.SetBaseURL("net", srv.URL)
	other.SetCacheDir(dir)
	rec, err := other.Lookup(context.Background(), "atlantis-software.net")
	if err != nil || rec.Status != analysis.DomainFound || calls.Load() != 1 {
		t.Errorf("cached record=%+v err=%v calls=%d", rec, err, calls.Load())
	}
	legacy := []byte(`{"Status":0,"Registered":"2020-01-01T00:00:00Z"}`)
	if err := os.WriteFile(c.cachePath("legacy.net"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	rec, err = other.Lookup(context.Background(), "legacy.net")
	if err != nil || rec.Status != analysis.DomainFound || rec.Registered.Year() != 2026 || calls.Load() != 2 {
		t.Errorf("legacy record=%+v err=%v calls=%d, want refetch", rec, err, calls.Load())
	}
}

// TestLookup checks RDAP response handling and retries.
func TestLookup(t *testing.T) {
	t.Parallel()
	registered := time.Date(2026, 5, 7, 11, 49, 33, 0, time.UTC)
	tests := []struct {
		name           string
		status         int
		body           string
		want           analysis.DomainLookupStatus
		wantRegistered time.Time
		calls          int32
	}{
		{"found", 200, `{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}]}`, analysis.DomainFound, registered, 1},
		{"two registrations", 200, `{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"},{"eventAction":"registration","eventDate":"2020-01-01T00:00:00Z"}]}`, analysis.DomainLookupFailed, time.Time{}, 1},
		{"invalid date", 200, `{"events":[{"eventAction":"registration","eventDate":"yesterday"}]}`, analysis.DomainLookupFailed, time.Time{}, 1},
		{"undated pending", 200, `{"status":["pending delete"]}`, analysis.DomainFound, time.Time{}, 1},
		{"undated plain", 200, `{"status":["active"]}`, analysis.DomainLookupFailed, time.Time{}, 1},
		{"malformed body", 200, `{`, analysis.DomainLookupFailed, time.Time{}, 1},
		{"not found", 404, ``, analysis.DomainNotFound, time.Time{}, 1},
		{"request timeout", 408, ``, analysis.DomainLookupFailed, time.Time{}, 2},
		{"rate limited", 429, ``, analysis.DomainLookupFailed, time.Time{}, 2},
		{"server error", 503, ``, analysis.DomainLookupFailed, time.Time{}, 2},
		{"forbidden", 403, ``, analysis.DomainLookupFailed, time.Time{}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)
			c := NewClient()
			c.SetBaseURL("net", srv.URL)
			c.SetRetryDelay(0)
			c.SetRequestInterval(0)
			rec, err := c.Lookup(context.Background(), "atlantis-software.net")
			if rec.Status != tt.want {
				t.Fatalf("record=%+v err=%v, want %v", rec, err, tt.want)
			}
			if wantErr := tt.want == analysis.DomainLookupFailed; (err != nil) != wantErr {
				t.Errorf("err=%v, want error %v", err, wantErr)
			}
			if !rec.Registered.Equal(tt.wantRegistered) {
				t.Errorf("registered=%s, want %s", rec.Registered, tt.wantRegistered)
			}
			// A second lookup is served from memory, failures included.
			if _, err := c.Lookup(context.Background(), "atlantis-software.net"); (err != nil) != (tt.want == analysis.DomainLookupFailed) {
				t.Errorf("cached lookup err=%v", err)
			}
			if got := calls.Load(); got != tt.calls {
				t.Errorf("calls=%d, want %d", got, tt.calls)
			}
		})
	}
	var calls atomic.Int32
	c := NewClient()
	c.SetHTTPClient(&http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, nil })})
	rec, err := c.Lookup(context.Background(), "example.invalidtld")
	if err != nil || rec.Status != analysis.DomainUnsupported || calls.Load() != 0 {
		t.Errorf("unsupported=%+v calls=%d", rec, calls.Load())
	}
	if domain, private := Registrable("mail.example.co.uk"); domain != "example.co.uk" || private {
		t.Errorf("registrable=%s private=%v", domain, private)
	}
	if domain, private := Registrable("me.github.io"); domain != "me.github.io" || !private {
		t.Errorf("registrable=%s private=%v", domain, private)
	}
}

// TestCheckAddress checks address exclusions and classification.
func TestCheckAddress(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}]}`))
	}))
	t.Cleanup(srv.Close)
	c := NewClient()
	c.SetBaseURL("net", srv.URL)
	c.SetRequestInterval(0)
	first := time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)
	base := time.Date(2026, 5, 14, 14, 25, 30, 0, time.UTC)
	tests := []struct {
		name, email string
		first       time.Time
		kind        analysis.MaintainerDomainKind
		reason      analysis.MaintainerDomainReason
	}{
		{"case and whitespace", "A.Tiertant@Atlantis-Software.NET ", first, analysis.MaintainerDomainReregisteredBefore, ""},
		{"shared mail", "person@gmail.com", first, analysis.MaintainerDomainUnchecked, analysis.MaintainerDomainReasonSharedMail},
		{"shared mail regional host", "person@mail.yahoo.co.jp", first, analysis.MaintainerDomainUnchecked, analysis.MaintainerDomainReasonSharedMail},
		{"private suffix", "person@me.github.io", first, analysis.MaintainerDomainUnchecked, analysis.MaintainerDomainReasonPrivateSuffix},
		{"no rdap", "person@example.invalidtld", first, analysis.MaintainerDomainUnchecked, analysis.MaintainerDomainReasonNoRDAP},
		{"not in history", "person@atlantis-software.net", time.Time{}, analysis.MaintainerDomainUnchecked, analysis.MaintainerDomainReasonNotInHistory},
		{"invalid", "broken-address", first, analysis.MaintainerDomainUnchecked, analysis.MaintainerDomainReasonInvalidAddress},
		{"invalid without history", "broken-address", time.Time{}, analysis.MaintainerDomainUnchecked, analysis.MaintainerDomainReasonInvalidAddress},
		{"control character", "person@example.com\nInjected", first, analysis.MaintainerDomainUnchecked, analysis.MaintainerDomainReasonInvalidAddress},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.Check(context.Background(), analysis.MaintainerAddress{Account: "atiertant", Email: tt.email, FirstSeen: tt.first}, base)
			if got.Kind != tt.kind || got.Reason != tt.reason {
				t.Errorf("finding=%+v, want %s reason %s", got, tt.kind, tt.reason)
			}
		})
	}
	if calls.Load() != 1 {
		t.Errorf("RDAP calls=%d, want 1", calls.Load())
	}
}

// TestRegistrarEvidence checks vCard extraction and propagation to findings.
func TestRegistrarEvidence(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}],"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","DropCatch.com 538 LLC"]]]}]}`))
	}))
	t.Cleanup(srv.Close)
	c := NewClient()
	c.SetBaseURL("net", srv.URL)
	rec, err := c.Lookup(context.Background(), "atlantis-software.net")
	if err != nil || rec.Registrar != "DropCatch.com 538 LLC" {
		t.Fatalf("record=%+v err=%v", rec, err)
	}
	f := c.Check(context.Background(), analysis.MaintainerAddress{Email: "a.tiertant@atlantis-software.net", FirstSeen: time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)}, time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC))
	if f.Registrar != rec.Registrar || !f.DropCatch {
		t.Errorf("finding=%+v", f)
	}
}

// TestRegistrarSanitized keeps untrusted RDAP text on one notice line without
// terminal control sequences.
func TestRegistrarSanitized(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, fn, want string }{
		{"newline", `Example Registrar\nInjected`, "Example Registrar Injected"},
		{"escape sequence", `Example \u001b[31mRegistrar\u001b[0m`, "Example [31mRegistrar[0m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := NewClient()
			c.SetHTTPClient(&http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}],"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","` + tc.fn + `"]]]}]}`)), Header: make(http.Header)}, nil
			})})
			rec, err := c.request(context.Background(), "https://example.com/domain/example.com")
			if err != nil || rec.Registrar != tc.want {
				t.Fatalf("record=%+v err=%v, want registrar %q", rec, err, tc.want)
			}
		})
	}
}

// TestEmbeddedBootstrap checks that the embedded IANA file loads and keeps
// only https services.
func TestEmbeddedBootstrap(t *testing.T) {
	t.Parallel()
	embedded := embeddedBootstrap()
	if len(embedded.bases) == 0 || embedded.publication == "" {
		t.Fatalf("embedded registry: %d services, publication %q", len(embedded.bases), embedded.publication)
	}
	base := NewClient().base
	for _, tld := range []string{"com", "net", "org"} {
		if !strings.HasPrefix(base[tld], "https://") {
			t.Errorf("base[%q]=%q, want an https URL", tld, base[tld])
		}
	}
	for tld, u := range base {
		if !strings.HasPrefix(strings.ToLower(u), "https://") {
			t.Errorf("base[%q]=%q is not https", tld, u)
		}
	}
}

// TestCheckRedirect checks the redirect limit and the https downgrade refusal.
func TestCheckRedirect(t *testing.T) {
	t.Parallel()
	req := func(raw string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	https := req("https://rdap.example/domain/a.com")
	tests := []struct {
		name    string
		next    *http.Request
		via     []*http.Request
		wantErr bool
	}{
		{"https to https", req("https://registrar.example/domain/a.com"), []*http.Request{https}, false},
		{"https to http", req("http://registrar.example/domain/a.com"), []*http.Request{https}, true},
		{"userinfo", req("https://user@registrar.example/domain/a.com"), []*http.Request{https}, true},
		{"at hop limit", req("https://registrar.example/"), make([]*http.Request, maxRedirects), false},
		{"over hop limit", req("https://registrar.example/"), make([]*http.Request, maxRedirects+1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			via := tt.via
			if len(via) > 0 && via[0] == nil {
				via = append([]*http.Request{https}, via[1:]...)
			}
			if err := checkRedirect(tt.next, via); (err != nil) != tt.wantErr {
				t.Errorf("err=%v, want error %v", err, tt.wantErr)
			}
		})
	}
}

// TestSetHTTPClient checks nil handling, the default redirect policy on a
// copy, and that a caller's own policy and client are kept.
func TestSetHTTPClient(t *testing.T) {
	t.Parallel()
	c := NewClient()
	before := c.http
	c.SetHTTPClient(nil)
	if c.http != before {
		t.Errorf("nil client replaced the default")
	}
	plain := &http.Client{}
	c.SetHTTPClient(plain)
	if plain.CheckRedirect != nil || c.http == plain || c.http.CheckRedirect == nil {
		t.Errorf("caller client changed or copy lacks a redirect policy")
	}
	own := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.SetHTTPClient(&http.Client{CheckRedirect: own})
	if c.http.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Errorf("caller redirect policy replaced")
	}
}

// TestLookupRefusedRedirectIsNotRetried checks that a downgrade redirect
// fails at once instead of waiting for the retry.
func TestLookupRefusedRedirectIsNotRetried(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := NewClient()
	c.SetHTTPClient(&http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		h := make(http.Header)
		h.Set("Location", "http://registrar.example/domain/example.net")
		return &http.Response{StatusCode: http.StatusFound, Header: h, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})})
	c.SetBaseURL("net", "https://rdap.example")
	c.SetRetryDelay(time.Hour)
	c.SetRequestInterval(0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rec, err := c.Lookup(ctx, "example.net")
	if !errors.Is(err, errRedirectRefused) || rec.Status != analysis.DomainLookupFailed || calls.Load() != 1 {
		t.Fatalf("record=%+v err=%v calls=%d, want one refused request", rec, err, calls.Load())
	}
}

// TestLookupCancelledIsNotCached checks that a lookup cut short by the caller
// is asked again once the context is live.
func TestLookupCancelledIsNotCached(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}]}`))
	}))
	t.Cleanup(srv.Close)
	c := NewClient()
	c.SetBaseURL("net", srv.URL)
	c.SetRequestInterval(0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if rec, err := c.Lookup(ctx, "atlantis-software.net"); err == nil || rec.Status != analysis.DomainLookupFailed {
		t.Fatalf("cancelled record=%+v err=%v", rec, err)
	}
	rec, err := c.Lookup(context.Background(), "atlantis-software.net")
	if err != nil || rec.Status != analysis.DomainFound || calls.Load() != 1 {
		t.Fatalf("record=%+v err=%v calls=%d, want one fresh request", rec, err, calls.Load())
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

// RoundTrip invokes the test transport function.
func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
