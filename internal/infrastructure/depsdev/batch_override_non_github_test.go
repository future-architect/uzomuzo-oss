package depsdev

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

func TestFetchBatchPURLs_MissingGAKeepsNonGitHubDefaultRepo(t *testing.T) {
	for _, tc := range []struct {
		name, input, packagePath, stablePURL, defaultPURL, sourceRepo, wantRepo string
		gaFound, defaultNotFound                                                bool
	}{
		{
			name:        "npm GitLab",
			input:       "pkg:npm/example",
			packagePath: "/v3alpha/systems/npm/packages/example",
			stablePURL:  "pkg:npm/example@1.0.0",
			defaultPURL: "pkg:npm/example@2.0.0-beta.1",
			sourceRepo:  "https://gitlab.example/org/example.git",
			wantRepo:    "https://gitlab.example/org/example",
		},
		{
			name:        "versioned Maven non-GitHub SCM",
			input:       "pkg:maven/org.example/demo@2.0.0-M1",
			packagePath: "/v3alpha/systems/maven/packages/org.example:demo",
			stablePURL:  "pkg:maven/org.example/demo@1.0.0",
			defaultPURL: "pkg:maven/org.example/demo@2.0.0-M1",
			sourceRepo:  "scm:git:https://gitlab.example/org/demo.git",
			wantRepo:    "scm:git:https://gitlab.example/org/demo",
		},
		{
			name:        "npm GA metadata has no repository",
			input:       "pkg:npm/example",
			packagePath: "/v3alpha/systems/npm/packages/example",
			stablePURL:  "pkg:npm/example@1.0.0",
			defaultPURL: "pkg:npm/example@2.0.0-beta.1",
			sourceRepo:  "https://gitlab.example/org/example.git",
			wantRepo:    "https://gitlab.example/org/example",
			gaFound:     true,
		},
		{
			name:            "npm both package lookups empty",
			input:           "pkg:npm/example",
			packagePath:     "/v3alpha/systems/npm/packages/example",
			stablePURL:      "pkg:npm/example@1.0.0",
			defaultPURL:     "pkg:npm/example@2.0.0-beta.1",
			defaultNotFound: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defaultLookups := 0
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var body string
				switch r.URL.EscapedPath() {
				case tc.packagePath:
					stableVersion := tc.stablePURL[strings.LastIndex(tc.stablePURL, "@")+1:]
					defaultVersion := tc.defaultPURL[strings.LastIndex(tc.defaultPURL, "@")+1:]
					body = fmt.Sprintf(`{"versions":[{"versionKey":{"version":%q},"publishedAt":"2025-01-01T00:00:00Z"},{"versionKey":{"version":%q},"publishedAt":"2026-01-01T00:00:00Z","isDefault":true}]}`, stableVersion, defaultVersion)
				case "/v3alpha/purl/" + url.PathEscape(tc.stablePURL):
					if tc.gaFound {
						body = fmt.Sprintf(`{"version":{"purl":%q}}`, tc.stablePURL)
						break
					}
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
				case "/v3alpha/purl/" + url.PathEscape(tc.defaultPURL):
					defaultLookups++
					if tc.defaultNotFound {
						return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
					}
					body = fmt.Sprintf(`{"version":{"purl":%q,"links":[{"label":"SOURCE_REPO","url":%q}]}}`, tc.defaultPURL, tc.sourceRepo)
				default:
					return nil, fmt.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})
			c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "https://deps.example", Timeout: time.Second, BatchSize: 100})
			c.client = httpclient.NewClient(&http.Client{Transport: transport}, httpclient.RetryConfig{})

			results, err := c.GetDetailsForPURLs(context.Background(), []string{tc.input})
			if err != nil {
				t.Fatalf("GetDetailsForPURLs: %v", err)
			}
			result := results[tc.input]
			if result == nil {
				t.Fatal("missing batch result")
			}
			if !result.StableDefaultOverridden {
				t.Error("StableDefaultOverridden = false, want true")
			}
			if got := result.ReleaseInfo.StableVersion.PURL; got != tc.stablePURL {
				t.Errorf("Stable PURL = %q, want %q", got, tc.stablePURL)
			}
			if (result.Package != nil) != tc.gaFound {
				t.Errorf("Package = %+v, want GA metadata presence %v", result.Package, tc.gaFound)
			}
			if result.Error != nil {
				t.Errorf("Error = %q, want nil with selected GA release", *result.Error)
			}
			if got := result.RepoURL; got != tc.wantRepo {
				t.Errorf("RepoURL = %q, want %q", got, tc.wantRepo)
			}
			if defaultLookups != 1 {
				t.Errorf("default lookups = %d, want 1", defaultLookups)
			}
		})
	}
}

func TestResolveRepoURLsBatch_GoOverrideMissingGAMetadata(t *testing.T) {
	const original = "pkg:golang/github.com/acme/demo"
	const replacedDefault = original + "@v2.0.0-beta1"
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.EscapedPath() != "/v3alpha/purl/"+url.PathEscape(replacedDefault) {
			return nil, fmt.Errorf("unexpected request: %s", r.URL.String())
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})
	c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "https://deps.example", Timeout: time.Second})
	c.client = httpclient.NewClient(&http.Client{Transport: transport}, httpclient.RetryConfig{})
	c.goproxy = nil
	repos, missing := c.resolveRepoURLsBatch(context.Background(),
		map[string]*PackageResponse{original: nil},
		map[string]ReleaseInfo{original: {replacedDefault: Version{PURL: replacedDefault}}})
	if got := repos["https://github.com/acme/demo"]; len(got) != 1 || got[0] != original {
		t.Errorf("GitHub repository PURLs = %v, want [%s]", got, original)
	}
	if len(missing) != 0 {
		t.Errorf("missing repository PURLs = %v, want none", missing)
	}
}
