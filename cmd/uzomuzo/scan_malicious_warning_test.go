package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/application"
	"github.com/future-architect/uzomuzo-oss/internal/application/scan"
	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainscan "github.com/future-architect/uzomuzo-oss/internal/domain/scan"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/osv"
)

type failingOSVSource struct{ client *osv.Client }

func (s failingOSVSource) AnalyzeFromPURLs(ctx context.Context, purls []string) (map[string]*analysis.Analysis, error) {
	return s.analyze(ctx, purls, false)
}

func (s failingOSVSource) AnalyzeFromGitHubURLs(ctx context.Context, urls []string) (map[string]*analysis.Analysis, error) {
	return s.analyze(ctx, urls, true)
}

func (s failingOSVSource) analyze(ctx context.Context, keys []string, github bool) (map[string]*analysis.Analysis, error) {
	result := make(map[string]*analysis.Analysis, len(keys))
	for _, key := range keys {
		_, err := s.client.QueryPackage(ctx, "npm", "x")
		if err == nil {
			return nil, errors.New("expected OSV failure")
		}
		a := &analysis.Analysis{OriginalPURL: key, Error: errors.New("package unavailable")}
		if !github || !strings.Contains(key, "detached") {
			a.MaliciousState = &analysis.MaliciousState{Status: analysis.MaliciousStatusLookupFailed}
		}
		result[key] = a
	}
	return result, nil
}

func TestScanWarnsOnceForFinalMaliciousLookupFailures(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := osv.NewClient()
	client.SetBaseURL(srv.URL)
	client.SetCacheTTL(0)
	svc, err := scan.NewService(application.NewAnalysisService(failingOSVSource{client: client}))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := domainscan.ParseFailPolicy("")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.RunFromPURLs(context.Background(), []string{"pkg:npm/x@1.0.0"}, []string{
		"https://github.com/a/one", "https://github.com/a/two", "https://github.com/a/detached",
	}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(logs.String(), "malicious check incomplete"); got != 1 {
		t.Fatalf("warning count = %d, logs = %s", got, logs.String())
	}
	if !strings.Contains(logs.String(), "count=3") {
		t.Fatalf("warning should count only the three final unknown states: %s", logs.String())
	}
}
