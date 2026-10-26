package integration

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
)

func TestGitHubFallbackDropsMaliciousPackageFact(t *testing.T) {
	const (
		githubURL = "https://github.com/a/x"
		purl      = "pkg:npm/x@1.0.0"
	)
	for _, tt := range []struct {
		name          string
		packageResult *depsdev.Package
		repoURL       string
	}{
		{"package missing", nil, githubURL},
		{"repository mismatch", &depsdev.Package{}, "https://github.com/other/x"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := &osvRecorder{}
			srv := httptest.NewServer(rec.handler(func(string) string {
				return `{"vulns":[{"id":"MAL-1","affected":[{"package":{"ecosystem":"npm","name":"x"},"versions":["1.0.0"]}]}]}`
			}))
			defer srv.Close()
			s := newAdvisoryDBService(t, srv.URL)
			s.githubClient = github.NewClient(&config.Config{})
			s.depsdevClient = &identityDepsDevClient{details: map[string]*depsdev.BatchResult{
				purl: {PURL: purl, Package: tt.packageResult, RepoURL: tt.repoURL},
			}}
			got, err := s.fetchAndValidateGitHubAnalysis(context.Background(), purl, "pkg:npm/x", githubURL)
			if err != nil {
				t.Fatal(err)
			}
			if rec.calls.Load() != 1 {
				t.Fatalf("OSV calls=%d, want 1", rec.calls.Load())
			}
			if got.MaliciousState != nil || got.Package != nil || got.OriginalPURL != githubURL {
				t.Fatalf("fallback retained package identity: %+v", got)
			}
		})
	}
}
