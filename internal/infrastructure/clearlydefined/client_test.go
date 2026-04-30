package clearlydefined

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
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

	bodyExpressionAND = `{
	  "licensed": {
	    "declared": "Apache-2.0 AND MIT",
	    "score": { "total": 80, "declared": 60 }
	  }
	}`

	bodyLicenseRefScancode = `{
	  "licensed": {
	    "declared": "LicenseRef-scancode-public-domain",
	    "score": { "total": 60, "declared": 60 }
	  }
	}`

	// A scancode-internal license name not present in the SPDX table or our
	// alias map. (Pure fabrication; chosen to avoid matching anything real.)
	bodyScancodeInternalName = `{
	  "licensed": {
	    "declared": "AcmeInternalProprietary",
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

	// CD response with a populated declared but no `score` block at all —
	// score.declared defaults to 0 which is below the threshold.
	bodyNoScoreBlock = `{
	  "licensed": {
	    "declared": "Apache-2.0"
	  }
	}`

	bodyMalformedJSON = `{ "licensed": { "declared": ` // truncated

	bodyWithException = `{
	  "licensed": {
	    "declared": "GPL-2.0-only WITH Classpath-exception-2.0",
	    "score": { "total": 80, "declared": 60 }
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
	wantRaws := []string{"CDDL-1.1", "GPL-2.0-only"} // per-leaf, NOT the full expression
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
		if lic.Raw != wantRaws[i] {
			t.Errorf("license[%d].Raw = %q, want per-leaf %q", i, lic.Raw, wantRaws[i])
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

func TestFetchLicenses_RateLimitReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, found, err := c.FetchLicenses(context.Background(), "maven", "g", "a", "v")
	if err == nil {
		t.Fatal("expected non-nil error on HTTP 429")
	}
	if found {
		t.Errorf("found=true, want false on rate limit")
	}
	if !common.IsRateLimitError(err) {
		t.Errorf("IsRateLimitError(err) = false, want true; err = %v", err)
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

func TestFetchLicenses_PathEscapingForDisallowedChars(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A namespace containing characters disallowed in URI path
		// segments per RFC 3986 (e.g. space, which is not in `pchar`)
		// must arrive percent-encoded on the wire. "@" alone does NOT
		// need escaping ("pchar" allows it), so we test a genuinely-
		// disallowed char instead.
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

func TestFetchLicenses_SPDXExpressionAND(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodyExpressionAND))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	lics, found, err := c.FetchLicenses(context.Background(), "maven", "g", "a", "v")
	if err != nil || !found || len(lics) != 2 {
		t.Fatalf("got found=%v err=%v len=%d, want 2 SPDX leaves", found, err, len(lics))
	}
	wantIDs := []string{"Apache-2.0", "MIT"}
	for i, lic := range lics {
		if lic.Identifier != wantIDs[i] || lic.Source != domain.LicenseSourceClearlyDefinedSPDX || !lic.IsSPDX {
			t.Errorf("license[%d] = %+v, want SPDX %q", i, lic, wantIDs[i])
		}
	}
}

func TestFetchLicenses_SPDXWithException(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodyWithException))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	lics, found, err := c.FetchLicenses(context.Background(), "maven", "javax.servlet", "javax.servlet-api", "3.1.0")
	if err != nil {
		t.Fatalf("FetchLicenses error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if len(lics) != 1 {
		t.Fatalf("got %d licenses, want 1", len(lics))
	}
	lic := lics[0]
	if lic.Identifier != "GPL-2.0-only" {
		t.Errorf("Identifier = %q, want %q", lic.Identifier, "GPL-2.0-only")
	}
	if lic.Source != domain.LicenseSourceClearlyDefinedSPDX {
		t.Errorf("Source = %q, want %q", lic.Source, domain.LicenseSourceClearlyDefinedSPDX)
	}
	if !lic.IsSPDX {
		t.Error("IsSPDX = false, want true")
	}
	// Raw must preserve the full WITH operand so downstream consumers retain
	// the exception clause for display and compliance purposes.
	if lic.Raw != "GPL-2.0-only WITH Classpath-exception-2.0" {
		t.Errorf("Raw = %q, want full WITH operand %q", lic.Raw, "GPL-2.0-only WITH Classpath-exception-2.0")
	}
}

func TestFetchLicenses_ScancodeInternalNameNonStandard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodyScancodeInternalName))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	lics, found, err := c.FetchLicenses(context.Background(), "maven", "dom4j", "dom4j", "1.6.1")
	if err != nil || !found || len(lics) != 1 {
		t.Fatalf("got found=%v err=%v len=%d", found, err, len(lics))
	}
	if lics[0].Source != domain.LicenseSourceClearlyDefinedNonStandard {
		t.Errorf("Source = %q, want non-standard", lics[0].Source)
	}
	if lics[0].IsSPDX {
		t.Errorf("IsSPDX = true; scancode-internal name (raw=%q) must classify as non-SPDX", lics[0].Raw)
	}
	if lics[0].Identifier != "" {
		t.Errorf("Identifier = %q, want empty", lics[0].Identifier)
	}
}

func TestFetchLicenses_NoScoreBlockSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodyNoScoreBlock))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	// Missing score block ⇒ score.declared defaults to 0 ⇒ below threshold.
	lics, found, err := c.FetchLicenses(context.Background(), "maven", "g", "a", "v")
	if err != nil {
		t.Fatalf("err = %v, want nil (below-threshold should not surface as error)", err)
	}
	if found || lics != nil {
		t.Errorf("got found=%v lics=%v, want skipped", found, lics)
	}
}

func TestFetchLicenses_MalformedJSONSurfacesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bodyMalformedJSON))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, found, err := c.FetchLicenses(context.Background(), "maven", "g", "a", "v")
	if err == nil {
		t.Fatal("expected non-nil error on malformed JSON body")
	}
	if found {
		t.Errorf("found = true on decode error, want false")
	}
}

// TestFetchLicenses_CacheKeyDoesNotCollideOnDashNamespace pins the fix for
// the cache-key collision where the URL builder's "-" placeholder for empty
// namespace would otherwise share a cache slot with packages whose namespace
// is genuinely "-". Calls the same coordinate twice with two namespace
// inputs ("" and "-") and verifies the server is hit twice.
func TestFetchLicenses_CacheKeyDoesNotCollideOnDashNamespace(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(bodySingleSPDX))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if _, _, err := c.FetchLicenses(context.Background(), "npm", "", "lodash", "1.0"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, _, err := c.FetchLicenses(context.Background(), "npm", "-", "lodash", "1.0"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("server hit %d times, want 2 (empty namespace must not share cache slot with literal '-')", got)
	}
}

// TestFetchLicenses_CacheTTLExpiry exercises the cache-expiry branch by
// reaching directly into the unexported cache map to backdate an entry.
// Calls FetchLicenses twice and asserts the server is hit twice when the
// cached entry is expired.
func TestFetchLicenses_CacheTTLExpiry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(bodySingleSPDX))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if _, _, err := c.FetchLicenses(context.Background(), "maven", "g", "a", "1.0"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Backdate every entry so the TTL check fires on next lookup.
	c.cacheMu.Lock()
	for k, e := range c.cache {
		e.expires = time.Now().Add(-1 * time.Hour)
		c.cache[k] = e
	}
	c.cacheMu.Unlock()
	if _, _, err := c.FetchLicenses(context.Background(), "maven", "g", "a", "1.0"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("server hit %d times, want 2 (expired cache entry must trigger re-fetch)", got)
	}
}

func TestSupportsEcosystem(t *testing.T) {
	tests := []struct {
		name string
		eco  string
		want bool
	}{
		{name: "maven", eco: "maven", want: true},
		{name: "npm", eco: "npm", want: true},
		{name: "uppercase_normalized", eco: "Maven", want: true},
		{name: "with_padding", eco: "  pypi  ", want: true},
		{name: "go_unsupported", eco: "go", want: false},
		{name: "github_unsupported", eco: "github", want: false},
		{name: "empty", eco: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SupportsEcosystem(tt.eco); got != tt.want {
				t.Errorf("SupportsEcosystem(%q) = %v, want %v", tt.eco, got, tt.want)
			}
		})
	}
}
