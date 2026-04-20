package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/govanityresolve"
)

// zapGoGetHTML mirrors the live response from `go.uber.org/zap?go-get=1`.
const zapGoGetHTML = `<html><head>
<meta name="go-import" content="go.uber.org/zap git https://github.com/uber-go/zap">
</head></html>`

// gopkgYAMLGoGetHTML mirrors `gopkg.in/yaml.v3?go-get=1` — go-import points
// back at gopkg.in; go-source embeds the canonical GitHub URL.
const gopkgYAMLGoGetHTML = `<html><head>
<meta name="go-import" content="gopkg.in/yaml.v3 git https://gopkg.in/yaml.v3">
<meta name="go-source" content="gopkg.in/yaml.v3 _ https://github.com/go-yaml/yaml/tree/v3.0.1{/dir} https://github.com/go-yaml/yaml/blob/v3.0.1{/dir}/{file}#L{line}">
</head></html>`

func TestResolveVanityRepoURLs(t *testing.T) {
	var zapHits, yamlHits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/zap", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&zapHits, 1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(zapGoGetHTML))
	})
	mux.HandleFunc("/yaml.v3", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&yamlHits, 1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(gopkgYAMLGoGetHTML))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	svc := &IntegrationService{
		vanityResolver: govanityresolve.NewResolverWithClient(srv.Client()),
	}

	zapURL := srv.URL + "/zap"
	yamlURL := srv.URL + "/yaml.v3"

	analyses := map[string]*domain.Analysis{
		// Two analyses share the zap vanity URL → single HTTP hit.
		"pkg:golang/go.uber.org/zap@v1.27.0": {
			OriginalPURL:  "pkg:golang/go.uber.org/zap@v1.27.0",
			EffectivePURL: "pkg:golang/go.uber.org/zap@v1.27.0",
			Package:       &domain.Package{Ecosystem: "golang"},
			RepoURL:       zapURL,
			Repository:    &domain.Repository{URL: zapURL},
		},
		"pkg:golang/go.uber.org/zap@v1.26.0": {
			OriginalPURL:  "pkg:golang/go.uber.org/zap@v1.26.0",
			EffectivePURL: "pkg:golang/go.uber.org/zap@v1.26.0",
			Package:       &domain.Package{Ecosystem: "golang"},
			RepoURL:       zapURL,
			Repository:    &domain.Repository{URL: zapURL},
		},
		// gopkg.in-style — resolution must use the go-source fallback.
		"pkg:golang/gopkg.in/yaml.v3": {
			OriginalPURL:  "pkg:golang/gopkg.in/yaml.v3",
			EffectivePURL: "pkg:golang/gopkg.in/yaml.v3",
			Package:       &domain.Package{Ecosystem: "golang"},
			RepoURL:       yamlURL,
			Repository:    &domain.Repository{URL: yamlURL},
		},
		// Already GitHub — must be skipped (no HTTP hit, no rewrite).
		"pkg:golang/github.com/owner/project": {
			OriginalPURL:  "pkg:golang/github.com/owner/project",
			EffectivePURL: "pkg:golang/github.com/owner/project",
			Package:       &domain.Package{Ecosystem: "golang"},
			RepoURL:       "https://github.com/owner/project",
			Repository:    &domain.Repository{URL: "https://github.com/owner/project"},
		},
		// Non-golang ecosystem with vanity-shaped URL — must be skipped.
		"pkg:npm/example@1.0.0": {
			OriginalPURL:  "pkg:npm/example@1.0.0",
			EffectivePURL: "pkg:npm/example@1.0.0",
			Package:       &domain.Package{Ecosystem: "npm"},
			RepoURL:       zapURL,
			Repository:    &domain.Repository{URL: zapURL},
		},
	}

	svc.resolveVanityRepoURLs(context.Background(), analyses)

	assertRepo := func(key, want string) {
		t.Helper()
		a := analyses[key]
		if a.RepoURL != want {
			t.Errorf("%s: RepoURL = %q, want %q", key, a.RepoURL, want)
		}
		if a.Repository.URL != want {
			t.Errorf("%s: Repository.URL = %q, want %q", key, a.Repository.URL, want)
		}
	}

	assertRepo("pkg:golang/go.uber.org/zap@v1.27.0", "https://github.com/uber-go/zap")
	assertRepo("pkg:golang/go.uber.org/zap@v1.26.0", "https://github.com/uber-go/zap")
	assertRepo("pkg:golang/gopkg.in/yaml.v3", "https://github.com/go-yaml/yaml")
	assertRepo("pkg:golang/github.com/owner/project", "https://github.com/owner/project")
	// npm analysis must retain its original URL — we do not rewrite non-Go ecosystems.
	assertRepo("pkg:npm/example@1.0.0", zapURL)

	if got := atomic.LoadInt32(&zapHits); got != 1 {
		t.Errorf("expected 1 HTTP hit for zap (dedup), got %d", got)
	}
	if got := atomic.LoadInt32(&yamlHits); got != 1 {
		t.Errorf("expected 1 HTTP hit for yaml, got %d", got)
	}
}

func TestResolveVanityRepoURLsEmptyAnalyses(t *testing.T) {
	svc := &IntegrationService{}
	// Must not panic on zero-value struct (no analyses, no resolver).
	svc.resolveVanityRepoURLs(context.Background(), map[string]*domain.Analysis{})
}

func TestResolveVanityRepoURLsNoOpWithoutResolver(t *testing.T) {
	// Zero-value IntegrationService never auto-constructs a resolver —
	// callers must use NewIntegrationService to get one. This test pins
	// that contract: the step must silently do nothing instead of
	// panicking or reaching for a default.
	svc := &IntegrationService{}
	original := "https://gopkg.in/yaml.v3"
	a := &domain.Analysis{
		OriginalPURL:  "pkg:golang/gopkg.in/yaml.v3",
		EffectivePURL: "pkg:golang/gopkg.in/yaml.v3",
		Package:       &domain.Package{Ecosystem: "golang"},
		RepoURL:       original,
		Repository:    &domain.Repository{URL: original},
	}
	svc.resolveVanityRepoURLs(context.Background(), map[string]*domain.Analysis{
		"k": a,
	})
	if a.RepoURL != original {
		t.Fatalf("RepoURL was unexpectedly rewritten to %q", a.RepoURL)
	}
}

func TestResolveVanityRepoURLsContextCanceled(t *testing.T) {
	// Handler blocks until the test's per-request context is canceled,
	// simulating a slow vanity host. Resolution must return promptly and
	// leave RepoURL untouched.
	handlerReleased := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		<-req.Context().Done()
		close(handlerReleased)
	}))
	t.Cleanup(srv.Close)

	svc := &IntegrationService{
		vanityResolver: govanityresolve.NewResolverWithClient(srv.Client()),
	}
	original := srv.URL + "/slow"
	a := &domain.Analysis{
		Package:    &domain.Package{Ecosystem: "golang"},
		RepoURL:    original,
		Repository: &domain.Repository{URL: original},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.resolveVanityRepoURLs(ctx, map[string]*domain.Analysis{"k": a})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("resolveVanityRepoURLs did not return after ctx cancel")
	}
	// Drain the handler so the test server shuts down cleanly.
	select {
	case <-handlerReleased:
	default:
	}
	if a.RepoURL != original {
		t.Fatalf("RepoURL changed on canceled ctx: %q", a.RepoURL)
	}
}

func TestIsVanityCandidate(t *testing.T) {
	tests := []struct {
		name string
		a    *domain.Analysis
		want bool
	}{
		{"nil analysis", nil, false},
		{"nil package", &domain.Analysis{RepoURL: "https://gopkg.in/yaml.v3"}, false},
		{
			name: "non-golang ecosystem",
			a: &domain.Analysis{
				Package: &domain.Package{Ecosystem: "npm"},
				RepoURL: "https://gopkg.in/yaml.v3",
			},
			want: false,
		},
		{
			name: "empty RepoURL",
			a: &domain.Analysis{
				Package: &domain.Package{Ecosystem: "golang"},
			},
			want: false,
		},
		{
			name: "github.com host",
			a: &domain.Analysis{
				Package: &domain.Package{Ecosystem: "golang"},
				RepoURL: "https://github.com/a/b",
			},
			want: false,
		},
		{
			name: "github.com host case-insensitive",
			a: &domain.Analysis{
				Package: &domain.Package{Ecosystem: "golang"},
				RepoURL: "https://GitHub.COM/a/b",
			},
			want: false,
		},
		{
			name: "gopkg.in vanity host",
			a: &domain.Analysis{
				Package: &domain.Package{Ecosystem: "golang"},
				RepoURL: "https://gopkg.in/yaml.v3",
			},
			want: true,
		},
		{
			name: "go.uber.org vanity host",
			a: &domain.Analysis{
				Package: &domain.Package{Ecosystem: "golang"},
				RepoURL: "https://go.uber.org/zap",
			},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isVanityCandidate(tc.a); got != tc.want {
				t.Fatalf("isVanityCandidate = %v, want %v", got, tc.want)
			}
		})
	}
}
