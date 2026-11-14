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
	const githubRepo = "https://github.com/apache/directory-server"
	var defaultLookups atomic.Int32
	var pomLookups atomic.Int32

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.EscapedPath() {
		case "/v3alpha/systems/maven/packages/org.apache.directory.server:apacheds-core":
			body = `{"versions":[{"versionKey":{"version":"1.5.7"},"publishedAt":"2010-01-01T00:00:00Z"},{"versionKey":{"version":"2.0.0.AM27"},"publishedAt":"2023-10-09T00:00:00Z","isDefault":true}]}`
		case "/v3alpha/purl/" + url.PathEscape(stable):
			body = `{"version":{"versionKey":{"system":"maven","name":"org.apache.directory.server:apacheds-core","version":"1.5.7"},"purl":"` + stable + `","links":[{"label":"SOURCE_REPO","url":"http://svn.apache.org/viewvc/directory/apacheds/tags/1.5.7"}]}}`
		case "/v3alpha/purl/" + url.PathEscape(replacedDefault):
			defaultLookups.Add(1)
			body = `{"version":{"versionKey":{"system":"maven","name":"org.apache.directory.server:apacheds-core","version":"2.0.0.AM27"},"purl":"` + replacedDefault + `"}}`
		case "/v3alpha/projectbatch":
			var request ProjectBatchRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				return nil, err
			}
			if len(request.Requests) != 1 || request.Requests[0].ProjectKey.ID != "github.com/apache/directory-server" {
				return nil, fmt.Errorf("unexpected project keys: %+v", request.Requests)
			}
			body = `{"responses":[{"project":{"projectKey":{"id":"github.com/apache/directory-server"}}}]}`
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
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<project><scm><url>` + githubRepo + `</url></scm></project>`)), Header: make(http.Header), Request: r}, nil
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
	if got := result.RepoURL; got != githubRepo {
		t.Errorf("RepoURL = %q, want %q", got, githubRepo)
	}
	if result.Project == nil {
		t.Error("Project = nil, want GitHub project details")
	}
	if got := defaultLookups.Load(); got != 1 {
		t.Errorf("replaced default lookups = %d, want 1", got)
	}
	if got := pomLookups.Load(); got != 1 {
		t.Errorf("POM lookups = %d, want 1", got)
	}
}
