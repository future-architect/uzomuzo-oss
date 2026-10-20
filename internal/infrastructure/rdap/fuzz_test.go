package rdap

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// FuzzRDAPResponse checks that arbitrary bounded responses cannot panic or hang.
func FuzzRDAPResponse(f *testing.F) {
	f.Add([]byte(`{"status":["pending delete"]}`))
	f.Add([]byte(`{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}],"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","DropCatch.com 538 LLC"]]]}]}`))
	f.Add([]byte(`{"events":[{"eventAction":"registration"},{"eventAction":"registration"}]}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > rdapResponseLimit {
			return
		}
		client := &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
		})}
		c := &Client{http: client}
		// Only the absence of a panic or hang is checked; decode errors are expected.
		_, _ = c.request(context.Background(), "https://example.com/domain/example.com")
	})
}

// FuzzMailHost checks host parsing and public-suffix handling on arbitrary strings.
func FuzzMailHost(f *testing.F) {
	f.Add("mail.example.co.uk")
	f.Add("me.github.io")
	f.Add("..")
	f.Fuzz(func(t *testing.T, host string) {
		if len(host) > 4096 {
			return
		}
		_ = validMailHost(strings.ToLower(host))
		_, _ = Registrable(host)
	})
}

// FuzzBootstrap checks that registry parsing never panics and only keeps https services.
func FuzzBootstrap(f *testing.F) {
	f.Add([]byte(`{"publication":"2026-10-20T00:00:00Z","services":[[["com"],["https://rdap.verisign.com/com/v1/"]]]}`))
	f.Add([]byte(`{"services":[[["kg"],["http://rdap.cctld.kg/"]]]}`))
	f.Add([]byte(`{"services":[[]]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > rdapResponseLimit {
			return
		}
		list, err := parseBootstrap(data)
		if err != nil {
			return
		}
		for tld, base := range list.bases {
			if !strings.HasPrefix(strings.ToLower(base), "https://") {
				t.Fatalf("base[%q]=%q is not https", tld, base)
			}
		}
	})
}
