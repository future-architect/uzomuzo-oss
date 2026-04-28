package links

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBuildDepsDevURL(t *testing.T) {
	tests := []struct {
		name    string
		eco, in string // in = the canonical single-segment package name
		want    string
	}{
		// Single-name ecosystems
		{"npm simple", "npm", "express", "https://deps.dev/npm/express"},
		{"cargo", "cargo", "serde", "https://deps.dev/cargo/serde"},
		{"rubygems passthrough", "rubygems", "rails", "https://deps.dev/rubygems/rails"},
		{"nuget preserves case", "nuget", "Newtonsoft.Json", "https://deps.dev/nuget/Newtonsoft.Json"},
		{"pypi", "pypi", "requests", "https://deps.dev/pypi/requests"},

		// Ecosystem aliases
		{"PyPI uppercase normalizes", "PyPI", "requests", "https://deps.dev/pypi/requests"},
		{"golang -> go", "golang", "golang.org/x/sys", "https://deps.dev/go/golang.org%2Fx%2Fsys"},
		{"gem -> rubygems", "gem", "rails", "https://deps.dev/rubygems/rails"},

		// Multi-segment names get path-escaped (slashes become %2F so the URL
		// stays a single React Router :name segment)
		{"npm scoped slash encoded", "npm", "@types/node", "https://deps.dev/npm/@types%2Fnode"},
		{"go github multi-segment", "golang", "github.com/spf13/cobra", "https://deps.dev/go/github.com%2Fspf13%2Fcobra"},

		// Maven uses ":" separator (caller's responsibility — see packageEcoName)
		{"maven groupId:artifactId", "maven", "org.springframework:spring-core", "https://deps.dev/maven/org.springframework:spring-core"},
		{"maven dotted groupId", "maven", "org.springframework.boot:spring-boot-starter-json", "https://deps.dev/maven/org.springframework.boot:spring-boot-starter-json"},

		// Decoded PURL components carrying special characters re-encode once
		// (callers receive the unescaped form from packageurl.FromString;
		// PathEscape — not QueryEscape — preserves "+" and encodes " " as %20).
		{"name with space encodes as %20", "npm", "weird name", "https://deps.dev/npm/weird%20name"},
		{"name with plus preserved", "cargo", "a+b", "https://deps.dev/cargo/a+b"},
		{"name with literal percent escapes to %25", "npm", "100%pkg", "https://deps.dev/npm/100%25pkg"},

		// Unsupported ecosystems return ""
		{"composer not hosted", "composer", "laravel/framework", ""},
		{"hex not hosted", "hex", "phoenix", ""},
		{"swift not hosted", "swift", "swift-collections", ""},
		{"unknown ecosystem", "customtype", "foo", ""},

		// Empty inputs
		{"empty ecosystem", "", "express", ""},
		{"empty name", "npm", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildDepsDevURL(tt.eco, tt.in)
			if got != tt.want {
				t.Errorf("BuildDepsDevURL(%q, %q) = %q, want %q", tt.eco, tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildDepsDevVersionURL(t *testing.T) {
	tests := []struct {
		name               string
		eco, in, ver, want string
	}{
		{"npm with version", "npm", "express", "4.18.2", "https://deps.dev/npm/express/4.18.2"},
		{"go vanity with version", "golang", "golang.org/x/sys", "v0.20.0", "https://deps.dev/go/golang.org%2Fx%2Fsys/v0.20.0"},
		{"maven with version", "maven", "org.springframework:spring-core", "5.3.30", "https://deps.dev/maven/org.springframework:spring-core/5.3.30"},
		{"npm scoped with version", "npm", "@types/node", "20.11.0", "https://deps.dev/npm/@types%2Fnode/20.11.0"},
		{"rubygems via gem alias", "gem", "rails", "7.0.0", "https://deps.dev/rubygems/rails/7.0.0"},

		{"composer not hosted — empty even with version", "composer", "laravel/framework", "10.0.0", ""},
		{"empty ecosystem", "", "express", "1.0.0", ""},
		{"empty name", "npm", "", "1.0.0", ""},
		{"empty version", "npm", "express", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildDepsDevVersionURL(tt.eco, tt.in, tt.ver)
			if got != tt.want {
				t.Errorf("BuildDepsDevVersionURL(%q, %q, %q) = %q, want %q", tt.eco, tt.in, tt.ver, got, tt.want)
			}
		})
	}
}

func TestEncodeDepsDevPath(t *testing.T) {
	tests := []struct {
		name        string
		eco, in     string
		wantSystem  string
		wantEncoded string
	}{
		// Single-name ecosystems
		{"npm simple", "npm", "express", "npm", "express"},
		{"cargo", "cargo", "serde", "cargo", "serde"},
		{"rubygems passthrough", "rubygems", "rails", "rubygems", "rails"},
		{"nuget preserves case", "nuget", "Newtonsoft.Json", "nuget", "Newtonsoft.Json"},
		{"pypi", "pypi", "requests", "pypi", "requests"},

		// Aliases
		{"PyPI uppercase normalizes", "PyPI", "requests", "pypi", "requests"},
		{"golang -> go", "golang", "golang.org/x/sys", "go", "golang.org%2Fx%2Fsys"},
		{"gem -> rubygems", "gem", "rails", "rubygems", "rails"},

		// Multi-segment names get path-escaped
		{"npm scoped", "npm", "@types/node", "npm", "@types%2Fnode"},
		{"go github multi-segment", "golang", "github.com/spf13/cobra", "go", "github.com%2Fspf13%2Fcobra"},

		// Maven uses ":" separator (caller pre-joins via JoinMavenName)
		{"maven groupId:artifactId", "maven", "org.springframework:spring-core", "maven", "org.springframework:spring-core"},

		// PathEscape preserves "+" and encodes " " as %20
		{"name with space encodes as %20", "npm", "weird name", "npm", "weird%20name"},
		{"name with plus preserved", "cargo", "a+b", "cargo", "a+b"},
		{"name with literal percent escapes to %25", "npm", "100%pkg", "npm", "100%25pkg"},

		// Unsupported ecosystems return ("", "")
		{"composer not hosted", "composer", "laravel/framework", "", ""},
		{"hex not hosted", "hex", "phoenix", "", ""},
		{"swift not hosted", "swift", "swift-collections", "", ""},
		{"unknown ecosystem", "customtype", "foo", "", ""},

		// Empty inputs
		{"empty ecosystem", "", "express", "", ""},
		{"empty name", "npm", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSys, gotEnc := EncodeDepsDevPath(tt.eco, tt.in)
			if gotSys != tt.wantSystem || gotEnc != tt.wantEncoded {
				t.Errorf("EncodeDepsDevPath(%q, %q) = (%q, %q), want (%q, %q)",
					tt.eco, tt.in, gotSys, gotEnc, tt.wantSystem, tt.wantEncoded)
			}
		})
	}
}

func TestJoinMavenName(t *testing.T) {
	tests := []struct {
		name            string
		group, artifact string
		want            string
	}{
		{"both present", "org.springframework", "spring-core", "org.springframework:spring-core"},
		{"dotted group", "org.apache.logging.log4j", "log4j-api", "org.apache.logging.log4j:log4j-api"},
		{"empty group", "", "spring-core", "spring-core"},
		{"whitespace group treated as empty", "  ", "spring-core", "spring-core"},
		{"empty artifact", "org.springframework", "", ""},
		{"both empty", "", "", ""},
		{"trims surrounding whitespace", "  org.springframework  ", "  spring-core  ", "org.springframework:spring-core"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JoinMavenName(tt.group, tt.artifact); got != tt.want {
				t.Errorf("JoinMavenName(%q, %q) = %q, want %q", tt.group, tt.artifact, got, tt.want)
			}
		})
	}
}

func TestJoinNpmName(t *testing.T) {
	tests := []struct {
		name       string
		scope, pkg string
		want       string
	}{
		{"scoped with @ prefix", "@vue", "runtime-dom", "@vue/runtime-dom"},
		{"scoped without @ prefix", "types", "node", "@types/node"},
		{"unscoped", "", "lodash", "lodash"},
		{"whitespace scope treated as empty", "  ", "lodash", "lodash"},
		{"bare @ scope treated as empty", "@", "lodash", "lodash"},
		{"empty name", "@types", "", ""},
		{"both empty", "", "", ""},
		{"trims surrounding whitespace", "  @types  ", "  node  ", "@types/node"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JoinNpmName(tt.scope, tt.pkg); got != tt.want {
				t.Errorf("JoinNpmName(%q, %q) = %q, want %q", tt.scope, tt.pkg, got, tt.want)
			}
		})
	}
}

func TestErrUnsupportedEcosystemSentinel(t *testing.T) {
	wrapped := errors.Join(errors.New("normalize PURL"), ErrUnsupportedEcosystem)
	if !errors.Is(wrapped, ErrUnsupportedEcosystem) {
		t.Fatalf("wrapped error should match ErrUnsupportedEcosystem via errors.Is")
	}
}

func TestNormalizeDepsDevEcosystem(t *testing.T) {
	tests := []struct{ in, want string }{
		// Supported (passthrough)
		{"npm", "npm"},
		{"cargo", "cargo"},
		{"maven", "maven"},
		{"pypi", "pypi"},
		{"nuget", "nuget"},
		{"rubygems", "rubygems"},
		{"go", "go"},

		// Aliases
		{"golang", "go"},
		{"gem", "rubygems"},

		// Case + whitespace
		{"PyPI", "pypi"},
		{"  cargo  ", "cargo"},

		// Rejected (deps.dev does not host these)
		{"composer", ""},
		{"packagist", ""},
		{"hex", ""},
		{"swift", ""},
		{"customtype", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := normalizeDepsDevEcosystem(tt.in); got != tt.want {
				t.Errorf("normalizeDepsDevEcosystem(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestBuildDepsDevURL_LiveProbe verifies that every URL the helper produces
// for a known-real package resolves on deps.dev's SPA.
//
// Opt-in only: set UZOMUZO_LIVE_PROBE=1 to run. This keeps `go test ./...`
// hermetic in CI and restricted-network environments.
//
// deps.dev's frontend is a React Router v5 SPA whose package route pattern
// is `/:system/:name/:version?` — :name matches a single path segment
// ([^/]+), so the URL the helper emits must collapse multi-segment
// namespaces (Go modules, npm scopes, Maven groupId+artifactId) into one
// URL-encoded segment.
func TestBuildDepsDevURL_LiveProbe(t *testing.T) {
	if os.Getenv("UZOMUZO_LIVE_PROBE") == "" {
		t.Skip("network probe — set UZOMUZO_LIVE_PROBE=1 to enable")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	cases := []struct {
		name, eco, pkg string
	}{
		{"npm simple", "npm", "express"},
		{"npm scoped", "npm", "@types/node"},
		{"cargo", "cargo", "serde"},
		{"rubygems via gem", "gem", "rails"},
		{"nuget", "nuget", "Newtonsoft.Json"},
		{"pypi", "pypi", "django"},
		{"go vanity", "golang", "golang.org/x/sys"},
		{"go github", "golang", "github.com/spf13/cobra"},
		{"maven spring-core", "maven", "org.springframework:spring-core"},
		{"maven spring-boot-starter-json", "maven", "org.springframework.boot:spring-boot-starter-json"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			helper := BuildDepsDevURL(tt.eco, tt.pkg)
			if helper == "" {
				t.Fatalf("helper empty for %q/%q", tt.eco, tt.pkg)
			}
			rest := strings.TrimPrefix(helper, "https://deps.dev/")
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) != 2 {
				t.Fatalf("unexpected helper output %q", helper)
			}
			system, after := parts[0], parts[1]
			if strings.Contains(after, "/") {
				t.Fatalf("helper output %q has unencoded '/' in :name segment", helper)
			}
			decoded, err := url.PathUnescape(after)
			if err != nil {
				t.Fatalf("unescape helper path segment %q: %v", after, err)
			}
			api := "https://deps.dev/_/s/" + strings.ToUpper(system) + "/p/" + url.PathEscape(decoded)
			req, err := http.NewRequest(http.MethodGet, api, nil)
			if err != nil {
				t.Fatalf("create request for %q: %v", api, err)
			}
			req.Header.Set("User-Agent", "uzomuzo-oss test/0.1")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("probe %s: %v", api, err)
			}
			_ = resp.Body.Close()
			t.Logf("helper=%-70s api=%-70s status=%d", helper, api, resp.StatusCode)
			if resp.StatusCode != 200 {
				t.Errorf("URL %q does not resolve a real package on deps.dev (api status %d)", helper, resp.StatusCode)
			}
		})
	}
}
