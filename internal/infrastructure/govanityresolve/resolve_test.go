package govanityresolve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// gopkgInYAMLHTML mirrors the live response from `https://gopkg.in/yaml.v3?go-get=1`:
// go-import advertises gopkg.in itself (useless to us), while go-source embeds the
// canonical GitHub repository in the dir template with a `/tree/<tag>{/dir}` suffix.
const gopkgInYAMLHTML = `<html>
<head>
<meta name="go-import" content="gopkg.in/yaml.v3 git https://gopkg.in/yaml.v3">
<meta name="go-source" content="gopkg.in/yaml.v3 _ https://github.com/go-yaml/yaml/tree/v3.0.1{/dir} https://github.com/go-yaml/yaml/blob/v3.0.1{/dir}/{file}#L{line}">
</head>
<body>go get gopkg.in/yaml.v3</body>
</html>`

// goUberZapHTML mirrors `https://go.uber.org/zap?go-get=1` where go-import already
// points to the canonical GitHub repo.
const goUberZapHTML = `<html>
<head>
<meta name="go-import" content="go.uber.org/zap git https://github.com/uber-go/zap">
</head>
</html>`

// k8sAPIHTML mirrors the k8s.io vanity server, which emits content attributes
// that span multiple lines.
const k8sAPIHTML = `<html><head>
<meta name="go-import"
      content="k8s.io/api
               git https://github.com/kubernetes/api">
</head></html>`

// googleGRPCHTML mirrors `https://google.golang.org/grpc?go-get=1`.
const googleGRPCHTML = `<html><head>
<meta name="go-import" content="google.golang.org/grpc git https://github.com/grpc/grpc-go">
</head></html>`

// goSourceHomeHTML exercises parseGoSource shape 1: the `<home>` field is an
// explicit https://github.com/... URL (not the `_` sentinel). go-import is
// deliberately non-GitHub so the go-source code path is actually taken.
const goSourceHomeHTML = `<html><head>
<meta name="go-import" content="example.com/pkg git https://example.com/pkg">
<meta name="go-source" content="example.com/pkg https://github.com/example-org/pkg https://github.com/example-org/pkg/tree/main{/dir} https://github.com/example-org/pkg/blob/main{/dir}/{file}#L{line}">
</head></html>`

// nonGitHubHTML emits a go-import pointing at a non-GitHub git host —
// we must refuse to resolve it so downstream GitHub-specific consumers
// are not fed URLs they cannot parse.
const nonGitHubHTML = `<html><head>
<meta name="go-import" content="example.com/pkg git https://gitlab.com/example/pkg">
</head></html>`

// nonGitVCSHTML emits a go-import with a non-git VCS. Resolution must fail
// because the rest of the pipeline assumes git / GitHub.
const nonGitVCSHTML = `<html><head>
<meta name="go-import" content="example.com/pkg hg https://example.com/pkg">
</head></html>`

func TestResolveRepoURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/yaml.v3", handleGoGet(t, gopkgInYAMLHTML))
	mux.HandleFunc("/zap", handleGoGet(t, goUberZapHTML))
	mux.HandleFunc("/api", handleGoGet(t, k8sAPIHTML))
	mux.HandleFunc("/grpc", handleGoGet(t, googleGRPCHTML))
	mux.HandleFunc("/gosourcehome", handleGoGet(t, goSourceHomeHTML))
	mux.HandleFunc("/nongithub", handleGoGet(t, nonGitHubHTML))
	mux.HandleFunc("/nongit", handleGoGet(t, nonGitVCSHTML))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	r := NewResolverForTest(srv.Client())

	tests := []struct {
		name  string
		input string
		want  string
		wOK   bool
	}{
		{
			name:  "gopkg.in uses go-source fallback for canonical GitHub URL",
			input: srv.URL + "/yaml.v3",
			want:  "https://github.com/go-yaml/yaml",
			wOK:   true,
		},
		{
			name:  "go.uber.org go-import points directly to GitHub",
			input: srv.URL + "/zap",
			want:  "https://github.com/uber-go/zap",
			wOK:   true,
		},
		{
			name:  "multi-line content attribute (k8s.io shape)",
			input: srv.URL + "/api",
			want:  "https://github.com/kubernetes/api",
			wOK:   true,
		},
		{
			name:  "google.golang.org go-import points directly to GitHub",
			input: srv.URL + "/grpc",
			want:  "https://github.com/grpc/grpc-go",
			wOK:   true,
		},
		{
			name:  "go-source home URL fallback when go-import is non-GitHub",
			input: srv.URL + "/gosourcehome",
			want:  "https://github.com/example-org/pkg",
			wOK:   true,
		},
		{
			name:  "non-GitHub target is refused",
			input: srv.URL + "/nongithub",
			want:  "",
			wOK:   false,
		},
		{
			name:  "non-git VCS is refused",
			input: srv.URL + "/nongit",
			want:  "",
			wOK:   false,
		},
		{
			name:  "already-GitHub URL is a no-op",
			input: "https://github.com/go-yaml/yaml",
			want:  "",
			wOK:   false,
		},
		{
			name:  "empty URL is rejected",
			input: "",
			want:  "",
			wOK:   false,
		},
		{
			name:  "hostless URL is rejected",
			input: "https:///no-host",
			want:  "",
			wOK:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := r.ResolveRepoURL(context.Background(), tc.input)
			if ok != tc.wOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wOK)
			}
			if got != tc.want {
				t.Fatalf("got = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveRepoURLRejectsUnsafeInputs(t *testing.T) {
	r := NewResolver()
	inputs := []string{
		"http://gopkg.in/yaml.v3",             // plain HTTP rejected
		"https://user@gopkg.in/yaml.v3",       // userinfo rejected
		"https://127.0.0.1/pkg",               // loopback literal rejected
		"https://[::1]/pkg",                   // loopback v6 rejected
		"https://169.254.169.254/pkg",         // link-local (cloud metadata) rejected
		"https://10.0.0.5/pkg",                // RFC1918 rejected
		"https://localhost/pkg",               // loopback by name rejected
		"https://metadata.google.internal/p",  // known metadata host rejected
		"https://localhost./pkg",              // trailing-dot SSRF bypass rejected
		"https://metadata.google.internal./p", // trailing-dot metadata bypass rejected
		"https://[fe80::1%25lo0]/pkg",         // IPv6 zone ID link-local bypass rejected
		"https://[::1%25eth0]/pkg",            // IPv6 zone ID loopback bypass rejected
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			got, ok := r.ResolveRepoURL(context.Background(), in)
			if ok || got != "" {
				t.Fatalf("expected (\"\", false), got (%q, %v)", got, ok)
			}
		})
	}
}

func TestResolveRepoURLDoesNotCacheCanceledContext(t *testing.T) {
	// First call runs against a canceled context. If the resolver cached
	// the empty result, a subsequent call against a fresh context would
	// also fail even though the server is healthy. We verify the opposite.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		atomic.AddInt32(&hits, 1)
		handleGoGet(t, goUberZapHTML)(w, req)
	}))
	t.Cleanup(srv.Close)

	r := NewResolverForTest(srv.Client())
	input := srv.URL + "/zap"

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := r.ResolveRepoURL(canceled, input); ok {
		t.Fatalf("expected canceled ctx to yield ok=false")
	}

	// Fresh context must still succeed — prior failure must not have
	// poisoned the cache.
	got, ok := r.ResolveRepoURL(context.Background(), input)
	if !ok {
		t.Fatalf("expected cached-miss recovery after canceled ctx")
	}
	if got != "https://github.com/uber-go/zap" {
		t.Fatalf("unexpected canonical URL: %q", got)
	}
}

func TestResolveRepoURLHonorsRedirectCap(t *testing.T) {
	// Every hop redirects; the resolver must reject the chain once the
	// hop count exceeds maxRedirects, yielding ok=false. We bind a test
	// server then re-attach a test-mode-aware resolver's CheckRedirect
	// so the cap is still enforced against the loopback address.
	var hits int32
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Redirect(w, req, srv.URL+"/loop?go-get=1", http.StatusFound)
	})
	srv.Start()
	t.Cleanup(srv.Close)

	// NewResolverForTest installs the redirect-capping CheckRedirect onto
	// the httptest client for us (the loopback-host bypass is handled via
	// allowNonPublic), so we do not need to reach into internal fields.
	r := NewResolverForTest(srv.Client())

	if _, ok := r.ResolveRepoURL(context.Background(), srv.URL+"/loop"); ok {
		t.Fatalf("expected unbounded redirect chain to be refused")
	}
	got := atomic.LoadInt32(&hits)
	if got < 1 {
		t.Fatalf("expected at least 1 HTTP hit (first request), got %d — redirect cap short-circuited before dispatch", got)
	}
	if got > maxRedirects+1 {
		t.Fatalf("expected at most %d HTTP hits, got %d", maxRedirects+1, got)
	}
}

func TestResolveRepoURLCachesRepeatedLookups(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		atomic.AddInt32(&hits, 1)
		handleGoGet(t, goUberZapHTML)(w, req)
	}))
	t.Cleanup(srv.Close)

	r := NewResolverForTest(srv.Client())
	input := srv.URL + "/zap"

	for i := 0; i < 3; i++ {
		got, ok := r.ResolveRepoURL(context.Background(), input)
		if !ok {
			t.Fatalf("iteration %d: ok=false, expected cached success", i)
		}
		if got != "https://github.com/uber-go/zap" {
			t.Fatalf("iteration %d: got %q", i, got)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected exactly 1 HTTP hit, got %d", got)
	}
}

func TestResolveRepoURLCachesNegativeResults(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	r := NewResolverForTest(srv.Client())
	input := srv.URL + "/dead"

	for i := 0; i < 3; i++ {
		if _, ok := r.ResolveRepoURL(context.Background(), input); ok {
			t.Fatalf("iteration %d: expected ok=false", i)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected exactly 1 HTTP hit (negative caching), got %d", got)
	}
}

func TestResolveRepoURLNilReceiver(t *testing.T) {
	var r *Resolver
	got, ok := r.ResolveRepoURL(context.Background(), "https://gopkg.in/yaml.v3")
	if ok || got != "" {
		t.Fatalf("nil receiver must return (\"\", false); got (%q, %v)", got, ok)
	}
}

func TestGithubRepoFromURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"underscore sentinel", "_", ""},
		{"non-github host", "https://gitlab.com/a/b", ""},
		{"owner only", "https://github.com/owner", ""},
		{"owner and repo", "https://github.com/owner/repo", "https://github.com/owner/repo"},
		{"strips .git suffix", "https://github.com/owner/repo.git", "https://github.com/owner/repo"},
		{"case-insensitive host", "https://GitHub.COM/owner/repo", "https://github.com/owner/repo"},
		{"deep path truncated to owner/repo", "https://github.com/owner/repo/tree/main", "https://github.com/owner/repo"},
		{"explicit port 443", "https://github.com:443/owner/repo", "https://github.com/owner/repo"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := githubRepoFromURL(tc.in); got != tc.want {
				t.Fatalf("githubRepoFromURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseGoImportPrefixMatching(t *testing.T) {
	html := `<html><head>
<meta name="go-import" content="example.com git https://github.com/example/root">
<meta name="go-import" content="example.com/sub git https://github.com/example/sub">
</head></html>`

	tests := []struct {
		name       string
		importPath string
		want       string
	}{
		{
			name:       "root prefix matches root import path",
			importPath: "example.com",
			want:       "https://github.com/example/root",
		},
		{
			name:       "sub prefix wins for sub import path",
			importPath: "example.com/sub",
			want:       "https://github.com/example/sub",
		},
		{
			name:       "deep sub path matches sub prefix",
			importPath: "example.com/sub/deep",
			want:       "https://github.com/example/sub",
		},
		{
			name:       "empty importPath falls back to first match",
			importPath: "",
			want:       "https://github.com/example/root",
		},
		{
			name:       "unrelated importPath falls back to first match",
			importPath: "other.com/pkg",
			want:       "https://github.com/example/root",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGoImport(html, tc.importPath)
			if got != tc.want {
				t.Fatalf("parseGoImport(importPath=%q) = %q, want %q", tc.importPath, got, tc.want)
			}
		})
	}
}

func TestTrimTemplateTail(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://github.com/a/b/tree/master{/dir}", "https://github.com/a/b"},
		{"https://github.com/a/b/blob/master{/dir}/{file}", "https://github.com/a/b"},
		{"https://github.com/a/b", "https://github.com/a/b"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := trimTemplateTail(tc.in); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// handleGoGet is the standard go-get handler shape: returns the fixture HTML
// for GET requests carrying `?go-get=1`. Any other shape is treated as a test
// bug and fails the test loudly.
func handleGoGet(t *testing.T, body string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			t.Errorf("unexpected method %s on %s", req.Method, req.URL)
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		if req.URL.Query().Get("go-get") != "1" {
			t.Errorf("missing go-get=1 query on %s", req.URL)
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		if ua := req.Header.Get("User-Agent"); !strings.Contains(ua, "uzomuzo-govanityresolve") {
			t.Errorf("unexpected user agent %q", ua)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}
}

func TestIsPublicHostTrailingDot(t *testing.T) {
	// FQDN-form hostnames (trailing dot) must be rejected just like their
	// non-FQDN equivalents — a common SSRF bypass vector.
	cases := []struct {
		name string
		host string
		want bool
	}{
		{"localhost trailing dot", "localhost.", false},
		{"metadata trailing dot", "metadata.google.internal.", false},
		{"ip6-localhost trailing dot", "ip6-localhost.", false},
		{"only dots", "...", false},
		{"empty", "", false},
		{"public host trailing dot", "gopkg.in.", true},
		{"public host no dot", "gopkg.in", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isPublicHost(tc.host)
			if got != tc.want {
				t.Errorf("isPublicHost(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

func TestIsPublicHostIPv6ZoneID(t *testing.T) {
	// IPv6 literals with zone identifiers (e.g., "fe80::1%lo0") must be
	// rejected. net.ParseIP does not parse zone IDs, so without explicit
	// stripping they bypass the IP classification — an SSRF bypass vector.
	cases := []struct {
		name string
		host string
		want bool
	}{
		{"link-local with zone", "fe80::1%lo0", false},
		{"loopback with zone", "::1%eth0", false},
		{"private with zone", "10.0.0.1%zone", false},
		{"link-local no zone", "fe80::1", false},
		{"loopback no zone", "::1", false},
		{"public IPv6 with zone", "2607:f8b0:4004:800::200e%eth0", true},
		{"public IPv6 no zone", "2607:f8b0:4004:800::200e", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isPublicHost(tc.host)
			if got != tc.want {
				t.Errorf("isPublicHost(%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
}

func TestNormalizeVanityURLCanonicalizesHostCase(t *testing.T) {
	// Mixed-case hosts must produce the same cache key as lowercase hosts.
	url1, _, ok1 := normalizeVanityURL("https://GOPKG.IN/yaml.v3", false)
	url2, _, ok2 := normalizeVanityURL("https://gopkg.in/yaml.v3", false)
	if !ok1 || !ok2 {
		t.Fatalf("expected both URLs to normalize successfully, got ok1=%v ok2=%v", ok1, ok2)
	}
	if url1 != url2 {
		t.Errorf("host casing produced different cache keys:\n  %q\n  %q", url1, url2)
	}
}
