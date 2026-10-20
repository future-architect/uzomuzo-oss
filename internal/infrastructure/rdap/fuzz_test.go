package rdap

import (
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
		c := NewClient()
		c.SetHTTPClient(&http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
		})})
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
