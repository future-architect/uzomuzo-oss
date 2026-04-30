package clearlydefined

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

const (
	bodySingleSPDX = `{
	  "licensed": {
	    "declared": "Apache-2.0",
	    "score": { "total": 100, "declared": 60 }
	  }
	}`

	bodyExpressionOR = `{
	  "licensed": {
	    "declared": "CDDL-1.1 OR GPL-2.0-only",
	    "score": { "total": 80, "declared": 60 }
	  }
	}`

	bodyLicenseRefScancode = `{
	  "licensed": {
	    "declared": "LicenseRef-scancode-public-domain",
	    "score": { "total": 60, "declared": 60 }
	  }
	}`

	bodyBelowThreshold = `{
	  "licensed": {
	    "declared": "Apache-2.0",
	    "score": { "total": 40, "declared": 15 }
	  }
	}`

	bodyEmptyDeclared = `{
	  "licensed": {
	    "declared": "",
	    "score": { "total": 0, "declared": 0 }
	  }
	}`
)

func TestFetchLicenses_SingleSPDX(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/definitions/maven/mavencentral/org.apache.commons/commons-lang3/3.12.0" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(bodySingleSPDX))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	lics, found, err := c.FetchLicenses(context.Background(), "maven", "org.apache.commons", "commons-lang3", "3.12.0")
	if err != nil {
		t.Fatalf("FetchLicenses error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if len(lics) != 1 {
		t.Fatalf("got %d licenses, want 1", len(lics))
	}
	want := domain.ResolvedLicense{
		Identifier: "Apache-2.0",
		Source:     domain.LicenseSourceClearlyDefinedSPDX,
		Raw:        "Apache-2.0",
		IsSPDX:     true,
	}
	if lics[0] != want {
		t.Errorf("got %+v, want %+v", lics[0], want)
	}
}

func TestFetchLicenses_SPDXExpression(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodyExpressionOR))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	lics, found, err := c.FetchLicenses(context.Background(), "maven", "javax.servlet", "javax.servlet-api", "4.0.1")
	if err != nil {
		t.Fatalf("FetchLicenses error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if len(lics) != 2 {
		t.Fatalf("got %d licenses, want 2", len(lics))
	}
	wantIDs := []string{"CDDL-1.1", "GPL-2.0-only"}
	for i, lic := range lics {
		if lic.Identifier != wantIDs[i] {
			t.Errorf("license[%d].Identifier = %q, want %q", i, lic.Identifier, wantIDs[i])
		}
		if lic.Source != domain.LicenseSourceClearlyDefinedSPDX {
			t.Errorf("license[%d].Source = %q, want %q", i, lic.Source, domain.LicenseSourceClearlyDefinedSPDX)
		}
		if !lic.IsSPDX {
			t.Errorf("license[%d].IsSPDX = false, want true", i)
		}
		if lic.Raw != "CDDL-1.1 OR GPL-2.0-only" {
			t.Errorf("license[%d].Raw = %q, want full declared", i, lic.Raw)
		}
	}
}

func TestFetchLicenses_LicenseRefScancodeIsNonStandard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodyLicenseRefScancode))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	lics, found, err := c.FetchLicenses(context.Background(), "maven", "org.json", "json", "20231013")
	if err != nil {
		t.Fatalf("FetchLicenses error: %v", err)
	}
	if !found || len(lics) != 1 {
		t.Fatalf("got found=%v len=%d, want true 1", found, len(lics))
	}
	if lics[0].Source != domain.LicenseSourceClearlyDefinedNonStandard {
		t.Errorf("Source = %q, want %q", lics[0].Source, domain.LicenseSourceClearlyDefinedNonStandard)
	}
	if lics[0].IsSPDX {
		t.Errorf("IsSPDX = true; LicenseRef-scancode-* must classify as non-SPDX")
	}
	if lics[0].Identifier != "" {
		t.Errorf("Identifier = %q, want empty for non-SPDX", lics[0].Identifier)
	}
}

func TestFetchLicenses_ScoreBelowThresholdSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodyBelowThreshold))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	lics, found, err := c.FetchLicenses(context.Background(), "maven", "g", "a", "v")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if found || lics != nil {
		t.Errorf("got found=%v lics=%v, want skipped", found, lics)
	}
}

func TestFetchLicenses_EmptyDeclaredSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodyEmptyDeclared))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	lics, found, err := c.FetchLicenses(context.Background(), "maven", "mysql", "mysql-connector-java", "8.0.33")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if found || lics != nil {
		t.Errorf("got found=%v lics=%v, want skipped (CD returned empty declared)", found, lics)
	}
}

func TestFetchLicenses_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	lics, found, err := c.FetchLicenses(context.Background(), "maven", "g", "a", "v")
	if err != nil {
		t.Fatalf("404 should not return error, got: %v", err)
	}
	if found || lics != nil {
		t.Errorf("got found=%v lics=%v, want not-found", found, lics)
	}
}

func TestFetchLicenses_ServerErrorReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, found, err := c.FetchLicenses(context.Background(), "maven", "g", "a", "v")
	if err == nil {
		t.Fatal("expected non-nil error on persistent 5xx")
	}
	if found {
		t.Errorf("found=true, want false on error")
	}
}

func TestFetchLicenses_UnknownEcosystemSkipsHTTP(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	lics, found, err := c.FetchLicenses(context.Background(), "rpm", "ns", "name", "1.0")
	if err != nil || found || lics != nil {
		t.Errorf("unknown ecosystem must short-circuit; got lics=%v found=%v err=%v", lics, found, err)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("HTTP called for unknown ecosystem (calls=%d)", calls)
	}
}

func TestFetchLicenses_CachesPositiveResponse(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(bodySingleSPDX))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	for i := 0; i < 3; i++ {
		_, found, err := c.FetchLicenses(context.Background(), "maven", "ns", "name", "1.0")
		if err != nil || !found {
			t.Fatalf("iter %d: error=%v found=%v", i, err, found)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server hit %d times, want 1 (cache should serve repeats)", got)
	}
}

func TestFetchLicenses_CachesNegativeResponse(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	for i := 0; i < 3; i++ {
		_, found, err := c.FetchLicenses(context.Background(), "maven", "ns", "name", "1.0")
		if err != nil || found {
			t.Fatalf("iter %d: error=%v found=%v", i, err, found)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server hit %d times, want 1 (negative cache should serve repeats)", got)
	}
}

func TestFetchLicenses_BlankInputsSkipped(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	tests := []struct {
		name                    string
		eco, ns, n, v           string
		wantFound               bool
		wantHTTPCalledThisInput bool
	}{
		{name: "blank_name", eco: "maven", ns: "g", n: "", v: "1.0"},
		{name: "blank_version", eco: "maven", ns: "g", n: "n", v: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := atomic.LoadInt32(&calls)
			lics, found, err := c.FetchLicenses(context.Background(), tt.eco, tt.ns, tt.n, tt.v)
			if err != nil || found || lics != nil {
				t.Errorf("got lics=%v found=%v err=%v, want all-zero", lics, found, err)
			}
			if got := atomic.LoadInt32(&calls); got != before {
				t.Errorf("HTTP issued for invalid input (delta=%d)", got-before)
			}
		})
	}
}

func TestFetchLicenses_EmptyNamespaceUsesPlaceholder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Empty namespace must be substituted with "-" per CD's URL convention.
		// Path shape: /definitions/<type>/<provider>/<namespace>/<name>/<version>
		if !strings.Contains(r.URL.Path, "/-/lodash/") {
			t.Errorf("path %q missing '-' placeholder for empty namespace", r.URL.Path)
		}
		_, _ = w.Write([]byte(bodySingleSPDX))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, found, err := c.FetchLicenses(context.Background(), "npm", "", "lodash", "4.17.21")
	if err != nil || !found {
		t.Errorf("got found=%v err=%v, want found=true", found, err)
	}
}

func TestFetchLicenses_PathEscapingForReservedChars(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A namespace containing characters that need escaping in path
		// segments per RFC 3986 (e.g. spaces -> %20, "?" -> %3F) must
		// arrive percent-encoded on the wire. "@" alone does NOT need
		// escaping in a path segment ("pchar" allows it) so we test a
		// genuinely-reserved char instead.
		if !strings.Contains(r.RequestURI, "%20") {
			t.Errorf("RequestURI %q missing %%20 escape for space", r.RequestURI)
		}
		_, _ = w.Write([]byte(bodySingleSPDX))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, found, err := c.FetchLicenses(context.Background(), "npm", "with spaces", "lodash", "1.0")
	if err != nil || !found {
		t.Errorf("got found=%v err=%v, want found=true", found, err)
	}
}

// newTestClient wires a Client with the test server's URL and HTTP client.
func newTestClient(srv *httptest.Server) *Client {
	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetHTTPClient(srv.Client())
	return c
}
