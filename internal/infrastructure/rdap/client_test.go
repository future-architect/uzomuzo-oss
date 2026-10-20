package rdap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

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
			if tt.want == analysis.DomainFound && !rec.Registered.Equal(time.Date(2026, 5, 7, 11, 49, 33, 0, time.UTC)) {
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

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
