package golangresolve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/goproxy"
)

func TestNormalizePathToModuleRoot(t *testing.T) {
	ctx := context.Background()
	mod, latest, ok := NormalizePathToModuleRoot(ctx, nil, "")
	if ok || mod != "" || latest != "" {
		t.Fatalf("expected empty on nil + blank input")
	}

	_, _, ok = NormalizePathToModuleRoot(ctx, nil, "github.com/org/repo")
	if ok {
		t.Fatalf("nil client should fail")
	}
}

// We provide an adapter type to reuse NormalizePathToModuleRoot logic expecting *goproxy.Client via minimal real instance and HTTP test server.

func TestNormalizePathToModuleRoot_WithRealClientTraversal(t *testing.T) {
	// Spin up HTTP server to simulate proxy responses for latest queries on multiple paths.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// pattern: /<module>/@latest
		if strings.HasSuffix(r.URL.Path, "/@latest") {
			path := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/@latest")
			// success only for github.com/org/repo and nested sub paths should fall back
			if path == "github.com/org/repo" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"Version":"v1.2.3"}`)) // test helper
				return
			}
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(500)
	}))
	defer ts.Close()

	real := goproxy.NewClientWith(ts.Client(), ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	mod, latest, ok := NormalizePathToModuleRoot(ctx, real, "github.com/org/repo/sub/pkg")
	if !ok || mod != "github.com/org/repo" || latest != "v1.2.3" {
		t.Fatalf("unexpected resolution: %v %v %v", ok, mod, latest)
	}
}

func TestNormalizePURLToModuleRoot(t *testing.T) {
	parser := purl.NewParser()
	ctx := context.Background()
	// Provide a proxy server.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/@latest") {
			// Only root path returns success
			if strings.Contains(r.URL.Path, "github.com%2Forg%2Frepo") && !strings.Contains(r.URL.Path, "sub") {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"Version":"v9.9.9"}`)) // test helper
				return
			}
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(500)
	}))
	defer ts.Close()

	gp := goproxy.NewClientWith(ts.Client(), ts.URL)

	p, err := parser.Parse("pkg:golang/github.com/org/repo/sub/pkg@v1.0.0")
	if err != nil {
		t.Fatalf("parse purl: %v", err)
	}

	mod, esc, ok := NormalizePURLToModuleRoot(ctx, gp, p, "")
	// Proxy returns 404 for module paths; we expect fallback to fail normalization (ok=false) and return original full path escaped
	if ok || mod != "github.com/org/repo/sub/pkg" || esc != "github.com%2Forg%2Frepo%2Fsub%2Fpkg" {
		t.Fatalf("expected no normalization: %v %v %v", mod, esc, ok)
	}

	// Non-golang PURL
	p2, _ := parser.Parse("pkg:npm/left-pad@1.1.0")
	if m, e, ok2 := NormalizePURLToModuleRoot(ctx, gp, p2, ""); ok2 || m != "" || e != "" {
		t.Fatalf("expected non-golang failure")
	}
}

func TestNormalizePURLToModuleRoot_RealSpelling(t *testing.T) {
	const lowerRoot = "github.com/burntsushi/toml"
	const lowerEsc = "github.com%2Fburntsushi%2Ftoml"
	const realRoot = "github.com/BurntSushi/toml"
	const realEsc = "github.com%2FBurntSushi%2Ftoml"
	tests := []struct {
		name       string
		purl       string
		modStatus  int
		modBody    string
		wantModule string
		wantEsc    string
	}{
		{
			name:       "lowercase PURL takes the spelling from go.mod",
			purl:       "pkg:golang/github.com/burntsushi/toml@v1.5.0",
			modStatus:  http.StatusOK,
			modBody:    "module github.com/BurntSushi/toml\n\ngo 1.18\n",
			wantModule: realRoot,
			wantEsc:    realEsc,
		},
		{
			name:       "mixed-case PURL takes the spelling from go.mod",
			purl:       "pkg:golang/github.com/BurntSushi/toml@v1.5.0",
			modStatus:  http.StatusOK,
			modBody:    "module github.com/BurntSushi/toml\n",
			wantModule: realRoot,
			wantEsc:    realEsc,
		},
		{
			name:       "quoted module directive with a comment",
			purl:       "pkg:golang/github.com/burntsushi/toml@v1.5.0",
			modStatus:  http.StatusOK,
			modBody:    "module \"github.com/BurntSushi/toml\" // TOML\n",
			wantModule: realRoot,
			wantEsc:    realEsc,
		},
		{
			name:       "go.mod naming another module keeps the resolved root",
			purl:       "pkg:golang/github.com/burntsushi/toml@v1.5.0",
			modStatus:  http.StatusOK,
			modBody:    "module github.com/someone/else\n",
			wantModule: lowerRoot,
			wantEsc:    lowerEsc,
		},
		{
			name:       "go.mod request failure keeps the resolved root",
			purl:       "pkg:golang/github.com/burntsushi/toml@v1.5.0",
			modStatus:  http.StatusInternalServerError,
			wantModule: lowerRoot,
			wantEsc:    lowerEsc,
		},
		{
			name:       "unparseable go.mod keeps the resolved root",
			purl:       "pkg:golang/github.com/burntsushi/toml@v1.5.0",
			modStatus:  http.StatusOK,
			modBody:    "not a go.mod\n",
			wantModule: lowerRoot,
			wantEsc:    lowerEsc,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var modRequests []string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/"+lowerRoot+"/@latest":
					_, _ = w.Write([]byte(`{"Version":"v1.6.0"}`)) // test helper
				case strings.HasSuffix(r.URL.Path, ".mod"):
					modRequests = append(modRequests, r.URL.Path)
					w.WriteHeader(tt.modStatus)
					_, _ = w.Write([]byte(tt.modBody)) // test helper
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer ts.Close()
			gp := goproxy.NewClientWith(ts.Client(), ts.URL)

			p, err := purl.NewParser().Parse(tt.purl)
			if err != nil {
				t.Fatalf("parse purl: %v", err)
			}
			mod, esc, ok := NormalizePURLToModuleRoot(context.Background(), gp, p, "")
			if !ok {
				t.Fatalf("ok = false, want true")
			}
			if mod != tt.wantModule {
				t.Errorf("module = %q, want %q", mod, tt.wantModule)
			}
			if esc != tt.wantEsc {
				t.Errorf("escaped = %q, want %q", esc, tt.wantEsc)
			}
			wantMod := "/" + lowerRoot + "/@v/v1.6.0.mod"
			if len(modRequests) != 1 || modRequests[0] != wantMod {
				t.Errorf("go.mod requests = %v, want [%s]", modRequests, wantMod)
			}
		})
	}
}

func TestNormalizePURLToModuleRoot_GitHubFallback(t *testing.T) {
	// Override githubRawBase to point to a local server simulating go.mod contents
	old := githubRawBase
	defer func() { githubRawBase = old }()

	modContent := "module github.com/other/repo\nrequire (\n\tX Y\n)"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/go.mod") {
			_, _ = w.Write([]byte(modContent)) // test helper
			return
		}
		w.WriteHeader(404)
	}))
	defer ts.Close()
	githubRawBase = ts.URL

	parser := purl.NewParser()
	p, _ := parser.Parse("pkg:golang/github.com/other/repo/sub@v0.0.1")
	mod, esc, ok := NormalizePURLToModuleRoot(context.Background(), nil, p, "develop")
	if !ok || mod != "github.com/other/repo" || esc != "github.com%2Fother%2Frepo" {
		t.Fatalf("fallback failed: %v %v %v", mod, esc, ok)
	}
}

func TestParseModuleDirective(t *testing.T) {
	cases := []struct{ in, expect string }{
		{"", ""},
		{"// comment only", ""},
		{"module github.com/a/b", "github.com/a/b"},
		{"   module   github.com/a/b/v2   ", "github.com/a/b/v2"},
		{"notmodule line\nmodule example.com/mod\n", "example.com/mod"},
	}
	for i, c := range cases {
		if got := parseModuleDirective(c.in); got != c.expect {
			t.Fatalf("case %d expected %q got %q", i, c.expect, got)
		}
	}
}
