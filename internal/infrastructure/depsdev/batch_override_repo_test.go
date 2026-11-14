package depsdev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/maven"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchBatchPURLs_OverrideKeepsDefaultGitHubRepo(t *testing.T) {
	const original = "pkg:maven/org.apache.directory.server/apacheds-core"
	const stable = original + "@1.5.7"
	const replacedDefault = original + "@2.0.0.AM27"
	const stableSVN = "http://svn.apache.org/viewvc/directory/apacheds/tags/1.5.7"
	const defaultSVN = "http://svn.apache.org/viewvc/directory/apacheds/tags/2.0.0.AM27"
	const defaultGitHub = "https://github.com/apache/directory-server"
	const stableGitHub = "https://github.com/apache/old-directory-server"

	for _, tc := range []struct {
		name, stableRepo, defaultRepo, wantRepo string
		gaNotFound, defaultNotFound             bool
		wantDefaultLookups, wantPOMLookups      int32
	}{
		{"stable SVN, default POM GitHub", stableSVN, "", defaultGitHub, false, false, 1, 1},
		{"stable GitHub stays", stableGitHub, "", stableGitHub, false, false, 0, 0},
		{"stable SCM GitHub stays", "scm:git:" + stableGitHub + ".git", "", stableGitHub, false, false, 0, 0},
		{"default SCM GitHub accepted", stableSVN, "scm:git:" + defaultGitHub + ".git", defaultGitHub, false, false, 1, 0},
		{"default also SVN", stableSVN, defaultSVN, stableSVN, false, false, 1, 0},
		{"GA package lookup fails, default POM GitHub", stableSVN, "", defaultGitHub, true, false, 1, 1},
		{"default package lookup fails, POM GitHub", stableSVN, "", defaultGitHub, false, true, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var defaultLookups, pomLookups atomic.Int32
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var body string
				switch r.URL.EscapedPath() {
				case "/v3alpha/systems/maven/packages/org.apache.directory.server:apacheds-core":
					body = `{"versions":[{"versionKey":{"version":"1.5.7"},"publishedAt":"2010-01-01T00:00:00Z"},{"versionKey":{"version":"2.0.0.AM27"},"publishedAt":"2023-10-09T00:00:00Z","isDefault":true}]}`
				case "/v3alpha/purl/" + url.PathEscape(stable):
					if tc.gaNotFound {
						return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
					}
					body = `{"version":{"versionKey":{"system":"maven","name":"org.apache.directory.server:apacheds-core","version":"1.5.7"},"purl":"` + stable + `","links":[{"label":"SOURCE_REPO","url":"` + tc.stableRepo + `"}]}}`
				case "/v3alpha/purl/" + url.PathEscape(replacedDefault):
					defaultLookups.Add(1)
					if tc.defaultNotFound {
						return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
					}
					body = `{"version":{"versionKey":{"system":"maven","name":"org.apache.directory.server:apacheds-core","version":"2.0.0.AM27"},"purl":"` + replacedDefault + `"`
					if tc.defaultRepo != "" {
						body += `,"links":[{"label":"SOURCE_REPO","url":"` + tc.defaultRepo + `"}]`
					}
					body += `}}`
				case "/v3alpha/projectbatch":
					var request ProjectBatchRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						return nil, err
					}
					key := strings.TrimPrefix(tc.wantRepo, "https://")
					if !strings.HasPrefix(tc.wantRepo, "https://github.com/") || len(request.Requests) != 1 || request.Requests[0].ProjectKey.ID != key {
						return nil, fmt.Errorf("unexpected project keys: %+v", request.Requests)
					}
					body = `{"responses":[{"project":{"projectKey":{"id":"` + key + `"}}}]}`
				default:
					return nil, fmt.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})
			c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "https://deps.example", Timeout: time.Second, BatchSize: 100})
			c.client = httpclient.NewClient(&http.Client{Transport: transport}, httpclient.RetryConfig{})
			mavenClient := maven.NewClient()
			mavenClient.SetBaseURL("https://maven.example/maven2")
			mavenClient.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				const pomPath = "/maven2/org/apache/directory/server/apacheds-core/2.0.0.AM27/apacheds-core-2.0.0.AM27.pom"
				if r.URL.EscapedPath() != pomPath {
					return nil, fmt.Errorf("unexpected POM request: %s", r.URL.String())
				}
				pomLookups.Add(1)
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<project><scm><url>` + defaultGitHub + `</url></scm></project>`)), Header: make(http.Header), Request: r}, nil
			})})
			c.WithMaven(mavenClient)

			results, err := c.GetDetailsForPURLs(context.Background(), []string{original})
			if err != nil {
				t.Fatalf("GetDetailsForPURLs: %v", err)
			}
			result := results[original]
			if result == nil {
				t.Fatal("missing batch result")
			}
			if got := result.ReleaseInfo.StableVersion.VersionKey.Version; got != "1.5.7" {
				t.Errorf("Stable = %q, want 1.5.7", got)
			}
			if got := result.ReleaseInfo.StableVersion.PURL; got != stable {
				t.Errorf("Stable PURL = %q, want %q", got, stable)
			}
			if tc.gaNotFound && result.Package != nil {
				t.Errorf("Package = %+v, want nil after GA metadata lookup fails", result.Package)
			}
			if got := result.RepoURL; got != tc.wantRepo {
				t.Errorf("RepoURL = %q, want %q", got, tc.wantRepo)
			}
			if strings.HasPrefix(tc.wantRepo, "https://github.com/") && result.Project == nil {
				t.Error("Project = nil, want GitHub project details")
			}
			if got := defaultLookups.Load(); got != tc.wantDefaultLookups {
				t.Errorf("replaced default lookups = %d, want %d", got, tc.wantDefaultLookups)
			}
			if got := pomLookups.Load(); got != tc.wantPOMLookups {
				t.Errorf("POM lookups = %d, want %d", got, tc.wantPOMLookups)
			}
		})
	}
}
