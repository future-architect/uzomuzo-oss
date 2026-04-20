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
	mux.HandleFunc("/nongithub", handleGoGet(t, nonGitHubHTML))
	mux.HandleFunc("/nongit", handleGoGet(t, nonGitVCSHTML))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	r := NewResolverWithClient(srv.Client())

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
		"http://gopkg.in/yaml.v3",            // plain HTTP rejected
		"https://user@gopkg.in/yaml.v3",      // userinfo rejected
		"https://127.0.0.1/pkg",              // loopback literal rejected
		"https://[::1]/pkg",                  // loopback v6 rejected
		"https://169.254.169.254/pkg",        // link-local (cloud metadata) rejected
		"https://10.0.0.5/pkg",               // RFC1918 rejected
		"https://localhost/pkg",              // loopback by name rejected
		"https://metadata.google.internal/p", // known metadata host rejected
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

	r := NewResolverWithClient(srv.Client())
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

	r := NewResolverWithClient(srv.Client())

	if _, ok := r.ResolveRepoURL(context.Background(), srv.URL+"/loop"); ok {
		t.Fatalf("expected unbounded redirect chain to be refused")
	}
	if got := atomic.LoadInt32(&hits); got > maxRedirects+1 {
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

	r := NewResolverWithClient(srv.Client())
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

	r := NewResolverWithClient(srv.Client())
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := githubRepoFromURL(tc.in); got != tc.want {
				t.Fatalf("githubRepoFromURL(%q) = %q, want %q", tc.in, got, tc.want)
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
