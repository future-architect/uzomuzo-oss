package rdap

import (
	"context"
	"encoding/json"
	"io"
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
	if err != nil || first.Status != analysis.DomainLookupFailed || calls.Load() != 2 {
		t.Fatalf("first=%+v err=%v calls=%d", first, err, calls.Load())
	}
	if _, err := os.Stat(client.cachePath("atlantis-software.net")); !os.IsNotExist(err) {
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
	tests := []struct {
		name   string
		status int
		body   string
		want   analysis.DomainLookupStatus
		calls  int32
	}{
		{"found", 200, `{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}]}`, analysis.DomainFound, 1},
		{"two registrations", 200, `{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"},{"eventAction":"registration","eventDate":"2020-01-01T00:00:00Z"}]}`, analysis.DomainLookupFailed, 1},
		{"invalid date", 200, `{"events":[{"eventAction":"registration","eventDate":"yesterday"}]}`, analysis.DomainLookupFailed, 1},
		{"undated pending", 200, `{"status":["pending delete"]}`, analysis.DomainFound, 1},
		{"undated plain", 200, `{"status":["active"]}`, analysis.DomainLookupFailed, 1},
		{"not found", 404, ``, analysis.DomainNotFound, 1},
		{"rate limited", 429, ``, analysis.DomainLookupFailed, 2},
		{"forbidden", 403, ``, analysis.DomainLookupFailed, 1},
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
			if err != nil || rec.Status != tt.want {
				t.Fatalf("record=%+v err=%v, want %v", rec, err, tt.want)
			}
			if tt.want == analysis.DomainFound && tt.name != "undated pending" && !rec.Registered.Equal(time.Date(2026, 5, 7, 11, 49, 33, 0, time.UTC)) {
				t.Errorf("registered=%s", rec.Registered)
			}
			_, _ = c.Lookup(context.Background(), "atlantis-software.net")
			if got := calls.Load(); got != tt.calls {
				t.Errorf("calls=%d, want %d", got, tt.calls)
			}
		})
	}
	var calls atomic.Int32
	c := NewClient()
	c.SetHTTPClient(&http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, nil })})
	rec, _ := c.Lookup(context.Background(), "example.invalidtld")
	if rec.Status != analysis.DomainUnsupported || calls.Load() != 0 {
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
		reason      string
	}{
		{"case and whitespace", "A.Tiertant@Atlantis-Software.NET ", first, analysis.MaintainerDomainReregisteredBefore, ""},
		{"shared mail", "person@gmail.com", first, analysis.MaintainerDomainUnchecked, "free_mail"},
		{"private suffix", "person@me.github.io", first, analysis.MaintainerDomainUnchecked, "private_suffix"},
		{"no rdap", "person@example.invalidtld", first, analysis.MaintainerDomainUnchecked, "no_rdap"},
		{"not in history", "person@atlantis-software.net", time.Time{}, analysis.MaintainerDomainUnchecked, "not_in_history"},
		{"invalid", "broken-address", first, analysis.MaintainerDomainUnchecked, "not_in_history"},
		{"control character", "person@example.com\nInjected", first, analysis.MaintainerDomainUnchecked, "not_in_history"},
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

// TestRegistrarWhitespace keeps untrusted RDAP text on one notice line.
func TestRegistrarWhitespace(t *testing.T) {
	c := NewClient()
	c.SetHTTPClient(&http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}],"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","Example Registrar\nInjected"]]]}]}`)), Header: make(http.Header)}, nil
	})})
	rec, err := c.request(context.Background(), "https://example.com/domain/example.com")
	if err != nil || rec.Registrar != "Example Registrar Injected" {
		t.Fatalf("record=%+v err=%v", rec, err)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

// RoundTrip invokes the test transport function.
func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
