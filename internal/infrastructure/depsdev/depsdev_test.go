package depsdev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/maven"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/nuget"
)

func TestConvertRepoURLToProjectKey(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "git_ssh_scheme_with_user_at",
			input:    "git+ssh://git@github.com/Owner/Repo",
			expected: "github.com/owner/repo",
		},
		{
			name:     "ssh_scheme_with_user_at",
			input:    "ssh://git@github.com/Owner/Repo",
			expected: "github.com/owner/repo",
		},
		{
			name:     "scp_style_git_at_colon",
			input:    "git@github.com:Owner/Repo",
			expected: "github.com/owner/repo",
		},
		{
			name:     "mixed_https_git_ssh_prefix",
			input:    "https://git+ssh://git@github.com/Owner/Repo",
			expected: "github.com/owner/repo",
		},
		{
			name:     "https_scheme_mixed_case",
			input:    "https://github.com/Owner/Repo",
			expected: "github.com/owner/repo",
		},
		{
			name:     "https_scheme_trailing_slash_removed",
			input:    "https://github.com/Owner/Repo/",
			expected: "github.com/owner/repo",
		},
		{
			name:     "http_scheme_mixed_case",
			input:    "http://github.com/Owner/Repo",
			expected: "github.com/owner/repo",
		},
		{
			name:     "no_scheme_github_prefix_mixed_case",
			input:    "github.com/Owner/Repo",
			expected: "github.com/owner/repo",
		},
		{
			name:     "already_lowercase",
			input:    "github.com/owner/repo",
			expected: "github.com/owner/repo",
		},
		{
			name:     "github_tree_path_reduced",
			input:    "https://github.com/rustcrypto/utils/tree/master/cpufeatures",
			expected: "github.com/rustcrypto/utils",
		},
		{
			name:     "github_deep_path_reduced",
			input:    "github.com/Org/Repo/issues/123",
			expected: "github.com/org/repo",
		},
		{
			name:     "dot_git_suffix_removed",
			input:    "https://github.com/Owner/Repo.git",
			expected: "github.com/owner/repo",
		},
		{
			name:     "dot_git_with_fragment_removed",
			input:    "https://github.com/puppeteer/puppeteer.git#main",
			expected: "github.com/puppeteer/puppeteer",
		},
		{
			name:     "query_and_fragment_removed",
			input:    "https://github.com/Owner/Repo?foo=bar#section",
			expected: "github.com/owner/repo",
		},
		{
			name:     "non_github_domain_returns_empty",
			input:    "https://gitlab.com/Owner/Repo",
			expected: "",
		},
		{
			name:     "empty_string",
			input:    "",
			expected: "",
		},
	}

	for _, tc := range cases {
		// capture range variable
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := convertRepoURLToProjectKey(tc.input)
			if got != tc.expected {
				t.Fatalf("convertRepoURLToProjectKey(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestExtractRepositoryURL_PriorityAndFallback(t *testing.T) {
	tests := []struct {
		name     string
		links    []Link
		expected string
	}{
		{
			name: "prefers_source_repo_over_others",
			links: []Link{
				{Label: "HOMEPAGE", URL: "https://github.com/owner/wrong"},
				{Label: "SOURCE_REPO", URL: "git+ssh://git@github.com/owner/correct"},
				{Label: "DOCS", URL: "https://example.com"},
			},
			expected: "https://github.com/owner/correct",
		},
		{
			name: "fallback_to_github_when_no_source_repo",
			links: []Link{
				{Label: "NPM", URL: "https://www.npmjs.com/package/pkg"},
				{Label: "HOMEPAGE", URL: "https://github.com/owner/repo"},
			},
			expected: "https://github.com/owner/repo",
		},
		{
			name: "fallback_ignores_non_github",
			links: []Link{
				{Label: "HOMEPAGE", URL: "https://example.com"},
				{Label: "DOCS", URL: "https://gitlab.com/owner/repo"},
			},
			expected: "",
		},
		{
			name: "fallback_accepts_github_com_format",
			links: []Link{
				{Label: "DOCS", URL: "github.com/Owner/Repo"},
			},
			expected: "github.com/Owner/Repo",
		},
		{
			name: "fallback_accepts_git_protocol_after_normalize",
			links: []Link{
				{Label: "DOCS", URL: "git://github.com/owner/repo.git"},
			},
			expected: "https://github.com/owner/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractRepositoryURLFromLinks(tt.links)
			if got != tt.expected {
				t.Fatalf("ExtractRepositoryURLFromLinks() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractRepositoryURL(t *testing.T) {
	cases := []struct {
		name     string
		links    []Link
		expected string
	}{
		{
			name:     "https_github",
			links:    []Link{{Label: "SOURCE_REPO", URL: "https://github.com/owner/repo"}},
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "https_with_git_suffix",
			links:    []Link{{Label: "SOURCE_REPO", URL: "https://github.com/owner/repo.git"}},
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "git_plus_https",
			links:    []Link{{Label: "SOURCE_REPO", URL: "git+https://github.com/owner/repo.git"}},
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "git_protocol",
			links:    []Link{{Label: "SOURCE_REPO", URL: "git://github.com/owner/repo.git"}},
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "git_plus_ssh",
			links:    []Link{{Label: "SOURCE_REPO", URL: "git+ssh://git@github.com/owner/repo"}},
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "ssh_scheme",
			links:    []Link{{Label: "SOURCE_REPO", URL: "ssh://git@github.com/owner/repo"}},
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "scp_style",
			links:    []Link{{Label: "SOURCE_REPO", URL: "git@github.com:owner/repo"}},
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "non_github_domain",
			links:    []Link{{Label: "SOURCE_REPO", URL: "https://gitlab.com/owner/repo.git"}},
			expected: "https://gitlab.com/owner/repo",
		},
		{
			name:     "with_query_and_fragment",
			links:    []Link{{Label: "SOURCE_REPO", URL: "https://github.com/Owner/Repo?foo=bar#section"}},
			expected: "https://github.com/Owner/Repo",
		},
		{
			name:     "no_source_repo_link",
			links:    []Link{{Label: "HOMEPAGE", URL: "https://github.com/owner/repo"}},
			expected: "https://github.com/owner/repo",
		},
		{
			name: "multiple_links_picks_source_repo",
			links: []Link{
				{Label: "HOMEPAGE", URL: "https://example.com"},
				{Label: "SOURCE_REPO", URL: "git+ssh://git@github.com/owner/repo"},
			},
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "empty_links",
			links:    []Link{},
			expected: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractRepositoryURLFromLinks(tc.links)
			if got != tc.expected {
				t.Fatalf("ExtractRepositoryURLFromLinks() = %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestExtractRepositoryURLFromLinks_Expected checks a subset of canonical expectations
// to validate the helper's external contract.
func TestExtractRepositoryURLFromLinks_Expected(t *testing.T) {
	cases := []struct {
		name     string
		links    []Link
		expected string
	}{
		{
			name:     "https_github",
			links:    []Link{{Label: "SOURCE_REPO", URL: "https://github.com/owner/repo"}},
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "git_plus_https_normalized",
			links:    []Link{{Label: "SOURCE_REPO", URL: "git+https://github.com/owner/repo.git"}},
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "fallback_to_github_homepage",
			links:    []Link{{Label: "HOMEPAGE", URL: "https://github.com/owner/repo"}},
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "non_github_domain_returns_empty_on_fallback",
			links:    []Link{{Label: "DOCS", URL: "https://gitlab.com/owner/repo"}},
			expected: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractRepositoryURLFromLinks(tc.links)
			if got != tc.expected {
				t.Fatalf("ExtractRepositoryURLFromLinks() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestNuGetResolverInChain(t *testing.T) {
	// Build a synthetic PackageResponse for nuget ecosystem
	pkg := &PackageResponse{Version: Version{VersionKey: VersionKey{System: "nuget", Name: "Serilog", Version: "2.0.0"}}}

	// Create a tiny server that serves a registration index with an embedded catalogEntry having repositoryUrl
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/registration5-semver2/serilog/index.json":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"items":[{"items":[{"catalogEntry":{"repositoryUrl":"https://github.com/serilog/serilog"}}]}]}`)) // test helper
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Create deps.dev client and wire a NuGet client pointing to test server
	c := NewDepsDevClient(&config.DefaultValues.DepsDev)
	ng := nuget.NewClient()
	ng.SetBaseURL(srv.URL + "/v3/registration5-semver2")
	c = c.WithNuGet(ng)

	url := c.resolveRepoURL(context.Background(), pkg, "pkg:nuget/Serilog@2.0.0")
	if url == "" {
		t.Fatalf("expected NuGet resolver to resolve a repo URL")
	}
	if url != "https://github.com/serilog/serilog" {
		t.Fatalf("got %q, want https://github.com/serilog/serilog", url)
	}
}

func TestTryMavenSearchFallback(t *testing.T) {
	// Simulate a scenario where:
	// 1. deps.dev returns 404 for "pkg:maven/jsr250-api"
	// 2. Maven Central Search returns groupId "javax.annotation"
	// 3. Retry with "pkg:maven/javax.annotation/jsr250-api" succeeds on deps.dev

	depsdevSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Match corrected PURL by checking for the resolved groupId in the request
		if strings.Contains(r.RequestURI, "javax.annotation") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":{"versionKey":{"system":"MAVEN","name":"javax.annotation:jsr250-api","version":"1.0"},"purl":"pkg:maven/javax.annotation/jsr250-api@1.0","links":[]}}`)) // test helper
			return
		}
		// Original (missing namespace) returns 404
		http.NotFound(w, r)
	}))
	defer depsdevSrv.Close()

	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"numFound":1,"docs":[{"g":"javax.annotation","a":"jsr250-api"}]}}`)) // test helper
	}))
	defer searchSrv.Close()

	cfg := config.DefaultValues.DepsDev
	cfg.BaseURL = depsdevSrv.URL
	c := NewDepsDevClient(&cfg)
	mv := maven.NewClient()
	mv.SetSearchBaseURL(searchSrv.URL)
	c = c.WithMaven(mv)

	resp, err := c.fetchPackageInfo(context.Background(), "pkg:maven/jsr250-api")
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response from Maven search fallback")
	}
	if resp.Version.VersionKey.Name != "javax.annotation:jsr250-api" {
		t.Fatalf("got name %q, want javax.annotation:jsr250-api", resp.Version.VersionKey.Name)
	}
}

func TestTryMavenSearchFallback_NamespaceEqualsName(t *testing.T) {
	// pkg:maven/spring-aop/spring-aop → namespace == name, should trigger search

	depsdevSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.RequestURI, "org.springframework") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":{"versionKey":{"system":"MAVEN","name":"org.springframework:spring-aop","version":"6.0.0"},"purl":"pkg:maven/org.springframework/spring-aop@6.0.0","links":[]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer depsdevSrv.Close()

	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"numFound":1,"docs":[{"g":"org.springframework","a":"spring-aop"}]}}`))
	}))
	defer searchSrv.Close()

	cfg := config.DefaultValues.DepsDev
	cfg.BaseURL = depsdevSrv.URL
	c := NewDepsDevClient(&cfg)
	mv := maven.NewClient()
	mv.SetSearchBaseURL(searchSrv.URL)
	c = c.WithMaven(mv)

	resp, err := c.fetchPackageInfo(context.Background(), "pkg:maven/spring-aop/spring-aop")
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response from Maven search fallback")
	}
}

func TestTryMavenSearchFallback_VersionInName(t *testing.T) {
	// pkg:maven/opentelemetry-sdk-extension-autoconfigure-1.28.0 → strip trailing version

	depsdevSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.RequestURI, "io.opentelemetry") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":{"versionKey":{"system":"MAVEN","name":"io.opentelemetry:opentelemetry-sdk-extension-autoconfigure","version":"1.28.0"},"purl":"pkg:maven/io.opentelemetry/opentelemetry-sdk-extension-autoconfigure@1.28.0","links":[]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer depsdevSrv.Close()

	searchSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"numFound":1,"docs":[{"g":"io.opentelemetry","a":"opentelemetry-sdk-extension-autoconfigure"}]}}`))
	}))
	defer searchSrv.Close()

	cfg := config.DefaultValues.DepsDev
	cfg.BaseURL = depsdevSrv.URL
	c := NewDepsDevClient(&cfg)
	mv := maven.NewClient()
	mv.SetSearchBaseURL(searchSrv.URL)
	c = c.WithMaven(mv)

	resp, err := c.fetchPackageInfo(context.Background(), "pkg:maven/opentelemetry-sdk-extension-autoconfigure-1.28.0")
	if err != nil {
		t.Fatalf("expected fallback to succeed, got error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response from Maven search fallback")
	}
}

func TestTryMavenSearchFallback_NoMavenClient(t *testing.T) {
	// Without maven client wired, fallback should not trigger (no panic)
	depsdevSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer depsdevSrv.Close()

	cfg := config.DefaultValues.DepsDev
	cfg.BaseURL = depsdevSrv.URL
	c := NewDepsDevClient(&cfg)
	// No WithMaven call

	_, err := c.fetchPackageInfo(context.Background(), "pkg:maven/jsr250-api")
	if err == nil {
		t.Fatal("expected error when deps.dev returns 404 and no maven client")
	}
}

func TestMavenResolverInChain(t *testing.T) {
	// Build a synthetic PackageResponse for maven ecosystem
	pkg := &PackageResponse{Version: Version{PURL: "pkg:maven/org.ognl/ognl@2.6.11", VersionKey: VersionKey{System: "maven", Name: "ognl", Version: "2.6.11"}}}

	// Create a tiny server that serves a POM with SCM URL
	// Path expected by client: /maven2/org/ognl/ognl/2.6.11/ognl-2.6.11.pom
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/maven2/org/ognl/ognl/2.6.11/ognl-2.6.11.pom":
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(200)
			// Minimal POM body with scm.url
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
			<project xmlns="http://maven.apache.org/POM/4.0.0">
				<modelVersion>4.0.0</modelVersion>
				<groupId>org.ognl</groupId>
				<artifactId>ognl</artifactId>
				<version>2.6.11</version>
				<scm>
					<url>https://github.com/jkuhnert/ognl</url>
				</scm>
			</project>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewDepsDevClient(&config.DefaultValues.DepsDev)
	mv := maven.NewClient()
	mv.SetBaseURL(srv.URL + "/maven2")
	c = c.WithMaven(mv)

	url := c.resolveRepoURL(context.Background(), pkg, pkg.Version.PURL)
	if url == "" {
		t.Fatalf("expected Maven resolver to resolve a repo URL")
	}
	if url != "https://github.com/jkuhnert/ognl" {
		t.Fatalf("got %q, want https://github.com/jkuhnert/ognl", url)
	}
}
