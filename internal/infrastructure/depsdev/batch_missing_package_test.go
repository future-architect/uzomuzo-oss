package depsdev

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

func TestGetDetailsForPURLs_ReleaseInfoWithoutPackageInfo(t *testing.T) {
	const purl = "pkg:cargo/example"
	const selected = purl + "@1.2.3"
	var unexpected unexpectedRequests
	// The versions endpoint returns a version, but its versioned PURL lookup is 404.
	// Package info is therefore absent while release info remains available.
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var status int
		var body string
		switch r.URL.EscapedPath() {
		case "/v3alpha/systems/cargo/packages/example":
			status = http.StatusOK
			body = `{"versions":[{"versionKey":{"version":"1.2.3"},"publishedAt":"2026-01-01T00:00:00Z","isDefault":true}]}`
		case "/v3alpha/purl/" + url.PathEscape(selected):
			status = http.StatusNotFound
		default:
			return nil, unexpected.record(r)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})
	c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "https://deps.example", Timeout: time.Second, BatchSize: 100})
	c.client = httpclient.NewClient(&http.Client{Transport: transport}, httpclient.RetryConfig{})

	results, err := c.GetDetailsForPURLs(context.Background(), []string{purl})
	unexpected.assertEmpty(t)
	if err != nil {
		t.Fatalf("GetDetailsForPURLs: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	result, ok := results[purl]
	if !ok || result == nil {
		t.Fatalf("result for %q = %+v, want entry", purl, result)
	}
	if result.Package != nil {
		t.Errorf("Package = %+v, want nil", result.Package)
	}
	if result.Error != nil {
		t.Errorf("Error = %q, want nil", *result.Error)
	}
	if got := result.ReleaseInfo.StableVersion.VersionKey.Version; got != "1.2.3" {
		t.Errorf("StableVersion = %q, want 1.2.3", got)
	}
	if got := result.ReleaseInfo.StableVersion.PURL; got != selected {
		t.Errorf("StableVersion.PURL = %q, want %q", got, selected)
	}
}

func TestGetDetailsForPURLs_UnknownPackageHasNoEntry(t *testing.T) {
	const purl = "pkg:cargo/unknown"
	var unexpected unexpectedRequests
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.EscapedPath() {
		case "/v3alpha/systems/cargo/packages/unknown", "/v3alpha/purl/" + url.PathEscape(purl):
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
		default:
			return nil, unexpected.record(r)
		}
	})
	c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "https://deps.example", Timeout: time.Second, BatchSize: 100})
	c.client = httpclient.NewClient(&http.Client{Transport: transport}, httpclient.RetryConfig{})

	results, err := c.GetDetailsForPURLs(context.Background(), []string{purl})
	unexpected.assertEmpty(t)
	if err != nil {
		t.Fatalf("GetDetailsForPURLs: %v", err)
	}
	if _, ok := results[purl]; ok {
		t.Errorf("result for %q exists, want no entry", purl)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0", len(results))
	}
}

type unexpectedRequests struct {
	mu       sync.Mutex
	requests []string
}

func (u *unexpectedRequests) record(r *http.Request) error {
	request := fmt.Sprintf("%s %s", r.Method, r.URL.String())
	u.mu.Lock()
	u.requests = append(u.requests, request)
	u.mu.Unlock()
	return fmt.Errorf("unexpected request: %s", request)
}

func (u *unexpectedRequests) assertEmpty(t *testing.T) {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.requests) != 0 {
		t.Errorf("unexpected requests = %v, want none", u.requests)
	}
}
