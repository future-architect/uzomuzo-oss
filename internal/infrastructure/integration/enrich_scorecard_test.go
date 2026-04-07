package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/scorecard"
)

func TestRepoKeyFromURL(t *testing.T) {
	tests := []struct {
		name     string
		repoURL  string
		expected string
	}{
		{
			name:     "standard GitHub URL",
			repoURL:  "https://github.com/owner/repo",
			expected: "github.com/owner/repo",
		},
		{
			name:     "GitHub URL with trailing slash",
			repoURL:  "https://github.com/owner/repo/",
			expected: "github.com/owner/repo",
		},
		{
			name:     "empty URL",
			repoURL:  "",
			expected: "",
		},
		{
			name:     "non-GitHub URL",
			repoURL:  "https://gitlab.com/owner/repo",
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repoKeyFromURL(tt.repoURL)
			if got != tt.expected {
				t.Errorf("repoKeyFromURL(%q) = %q, want %q", tt.repoURL, got, tt.expected)
			}
		})
	}
}

func TestDetectArchivedFromScorecard(t *testing.T) {
	t.Run("archived project detected", func(t *testing.T) {
		a := &domain.Analysis{
			Scores: map[string]*domain.ScoreEntity{
				"Maintained": domain.NewScoreEntity("Maintained", 0, 10, "project is archived on GitHub"),
			},
		}
		detectArchivedFromScorecard(a)
		if a.RepoState == nil || !a.RepoState.IsArchived {
			t.Error("expected IsArchived=true")
		}
	})

	t.Run("active project not archived", func(t *testing.T) {
		a := &domain.Analysis{
			Scores: map[string]*domain.ScoreEntity{
				"Maintained": domain.NewScoreEntity("Maintained", 10, 10, "30 commits in last 90 days"),
			},
		}
		detectArchivedFromScorecard(a)
		if a.RepoState != nil && a.RepoState.IsArchived {
			t.Error("expected IsArchived=false for active project")
		}
	})

	t.Run("no Maintained check", func(t *testing.T) {
		a := &domain.Analysis{
			Scores: map[string]*domain.ScoreEntity{
				"Code-Review": domain.NewScoreEntity("Code-Review", 8, 10, "found reviews"),
			},
		}
		detectArchivedFromScorecard(a)
		if a.RepoState != nil && a.RepoState.IsArchived {
			t.Error("expected no archived detection without Maintained check")
		}
	})

	t.Run("nil analysis", func(t *testing.T) {
		detectArchivedFromScorecard(nil) // should not panic
	})

	t.Run("nil scores", func(t *testing.T) {
		a := &domain.Analysis{}
		detectArchivedFromScorecard(a) // should not panic
	})
}

func TestEnrichScorecardFromAPI(t *testing.T) {
	t.Run("overwrites deps.dev scorecard with scorecard.dev data", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/projects/github.com/owner/repo" {
				_, _ = w.Write([]byte(`{
					"score": 8.5,
					"checks": [
						{"name":"Code-Review","score":9,"reason":"good"},
						{"name":"Maintained","score":10,"reason":"active"},
						{"name":"Vulnerabilities","score":10,"reason":"no vulnerabilities"},
						{"name":"CI-Tests","score":8,"reason":"CI tests detected"},
						{"name":"Contributors","score":7,"reason":"multiple contributors"}
					]
				}`))
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		hc := httpclient.NewClient(
			&http.Client{Timeout: 5 * time.Second},
			httpclient.RetryConfig{MaxRetries: 0},
		)
		sc := scorecard.NewClientWith(hc, ts.URL)
		svc := &IntegrationService{scorecardClient: sc}

		analyses := map[string]*domain.Analysis{
			"pkg:npm/express": {
				RepoURL:      "https://github.com/owner/repo",
				OverallScore: 5.0,
				Scores: map[string]*domain.ScoreEntity{
					"Code-Review": domain.NewScoreEntity("Code-Review", 5, 10, "old"),
					"Maintained":  domain.NewScoreEntity("Maintained", 6, 10, "old"),
				},
			},
		}

		svc.enrichScorecardFromAPI(context.Background(), analyses)

		a := analyses["pkg:npm/express"]
		if a.OverallScore != 8.5 {
			t.Errorf("expected OverallScore=8.5, got %f", a.OverallScore)
		}
		if len(a.Scores) != 5 {
			t.Errorf("expected 5 checks, got %d", len(a.Scores))
		}
		if _, ok := a.Scores["Vulnerabilities"]; !ok {
			t.Error("expected Vulnerabilities check to be present")
		}
		if _, ok := a.Scores["CI-Tests"]; !ok {
			t.Error("expected CI-Tests check to be present")
		}
	})

	t.Run("skips when scorecard.dev returns fewer checks", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"score": 3.0, "checks": [{"name":"Maintained","score":2,"reason":"stale"}]}`))
		}))
		defer ts.Close()

		hc := httpclient.NewClient(
			&http.Client{Timeout: 5 * time.Second},
			httpclient.RetryConfig{MaxRetries: 0},
		)
		sc := scorecard.NewClientWith(hc, ts.URL)
		svc := &IntegrationService{scorecardClient: sc}

		analyses := map[string]*domain.Analysis{
			"pkg:npm/express": {
				RepoURL:      "https://github.com/owner/repo",
				OverallScore: 7.0,
				Scores: map[string]*domain.ScoreEntity{
					"Code-Review": domain.NewScoreEntity("Code-Review", 8, 10, "good"),
					"Maintained":  domain.NewScoreEntity("Maintained", 9, 10, "active"),
					"SAST":        domain.NewScoreEntity("SAST", 5, 10, "ok"),
				},
			},
		}

		svc.enrichScorecardFromAPI(context.Background(), analyses)

		a := analyses["pkg:npm/express"]
		// Should NOT overwrite: scorecard.dev returned 1 check, deps.dev had 3
		if a.OverallScore != 7.0 {
			t.Errorf("expected OverallScore=7.0 (unchanged), got %f", a.OverallScore)
		}
		if len(a.Scores) != 3 {
			t.Errorf("expected 3 checks (unchanged), got %d", len(a.Scores))
		}
	})

	t.Run("no-op when scorecardClient is nil", func(t *testing.T) {
		svc := &IntegrationService{scorecardClient: nil}
		analyses := map[string]*domain.Analysis{
			"pkg:npm/express": {
				RepoURL:      "https://github.com/owner/repo",
				OverallScore: 5.0,
			},
		}
		svc.enrichScorecardFromAPI(context.Background(), analyses)
		if analyses["pkg:npm/express"].OverallScore != 5.0 {
			t.Error("expected no change when scorecardClient is nil")
		}
	})

	t.Run("skips non-GitHub URLs", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			// Should not be called for non-GitHub URLs
		}))
		defer ts.Close()

		hc := httpclient.NewClient(
			&http.Client{Timeout: 5 * time.Second},
			httpclient.RetryConfig{MaxRetries: 0},
		)
		sc := scorecard.NewClientWith(hc, ts.URL)
		svc := &IntegrationService{scorecardClient: sc}

		analyses := map[string]*domain.Analysis{
			"pkg:npm/express": {
				RepoURL:      "https://gitlab.com/owner/repo",
				OverallScore: 5.0,
			},
		}
		svc.enrichScorecardFromAPI(context.Background(), analyses)
		if analyses["pkg:npm/express"].OverallScore != 5.0 {
			t.Error("expected no change for non-GitHub URL")
		}
	})
}
