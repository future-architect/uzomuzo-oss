package scorecard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

func TestNewClient_Defaults(t *testing.T) {
	// nil config → uses defaults
	c := NewClient(nil)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.baseURL != "https://api.scorecard.dev" {
		t.Errorf("expected default base URL, got %s", c.baseURL)
	}
	if c.maxConcurrency != 10 {
		t.Errorf("expected default maxConcurrency=10, got %d", c.maxConcurrency)
	}
}

func TestNewClient_EmptyConfig(t *testing.T) {
	// zero-value config → falls back to defaults
	cfg := &config.ScorecardConfig{}
	c := NewClient(cfg)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.baseURL != "https://api.scorecard.dev" {
		t.Errorf("expected default base URL for empty config, got %s", c.baseURL)
	}
	if c.maxConcurrency != 10 {
		t.Errorf("expected default maxConcurrency=10, got %d", c.maxConcurrency)
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(handler)
	hc := httpclient.NewClient(
		&http.Client{Timeout: 5 * time.Second},
		httpclient.RetryConfig{MaxRetries: 0},
	)
	c := NewClientWith(hc, ts.URL)
	return c, ts
}

func TestFetchScorecard_Success(t *testing.T) {
	body := `{
		"date":"2024-01-01",
		"repo":{"name":"github.com/test/repo","commit":"abc"},
		"scorecard":{"version":"v5","commit":"def"},
		"score":7.5,
		"checks":[
			{"name":"Code-Review","score":8,"reason":"good","details":null,"documentation":{"short":"cr","url":"https://example.com"}},
			{"name":"Maintained","score":10,"reason":"active","details":null,"documentation":{"short":"m","url":"https://example.com"}},
			{"name":"Vulnerabilities","score":9,"reason":"no vulns","details":null,"documentation":{"short":"v","url":"https://example.com"}}
		]
	}`
	var gotPath atomic.Value
	c, ts := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body)) // best-effort write
	})
	defer ts.Close()

	result, err := c.FetchScorecard(context.Background(), "github.com/test/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p, _ := gotPath.Load().(string); p != "/projects/github.com/test/repo" {
		t.Errorf("unexpected path: %s", p)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.OverallScore != 7.5 {
		t.Errorf("expected overall score 7.5, got %f", result.OverallScore)
	}
	if len(result.Scores) != 3 {
		t.Errorf("expected 3 checks, got %d", len(result.Scores))
	}
	cr, ok := result.Scores["Code-Review"]
	if !ok {
		t.Fatal("expected Code-Review check")
	}
	if cr.Value() != 8 {
		t.Errorf("expected Code-Review score 8, got %d", cr.Value())
	}
	if cr.MaxValue() != 10 {
		t.Errorf("expected max value 10, got %d", cr.MaxValue())
	}
	vuln, ok := result.Scores["Vulnerabilities"]
	if !ok {
		t.Fatal("expected Vulnerabilities check")
	}
	if vuln.Value() != 9 {
		t.Errorf("expected Vulnerabilities score 9, got %d", vuln.Value())
	}
}

func TestFetchScorecard_NotFound(t *testing.T) {
	c, ts := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	defer ts.Close()

	result, err := c.FetchScorecard(context.Background(), "github.com/unknown/repo")
	if err != nil {
		t.Fatalf("expected nil error for 404, got: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for 404")
	}
}

func TestFetchScorecard_ServerError(t *testing.T) {
	c, ts := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error")) // best-effort write
	})
	defer ts.Close()

	_, err := c.FetchScorecard(context.Background(), "github.com/test/repo")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestFetchScorecard_StripsHTTPSPrefix(t *testing.T) {
	var gotPath atomic.Value
	c, ts := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"score":5.0,"checks":[]}`)) // best-effort write
	})
	defer ts.Close()

	result, err := c.FetchScorecard(context.Background(), "https://github.com/owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p, _ := gotPath.Load().(string); p != "/projects/github.com/owner/repo" {
		t.Errorf("unexpected path: %s", p)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestFetchScorecard_MalformedJSON(t *testing.T) {
	c, ts := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid json`)) // best-effort write
	})
	defer ts.Close()

	_, err := c.FetchScorecard(context.Background(), "github.com/test/repo")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestFetchScorecardBatch(t *testing.T) {
	c, ts := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/github.com/owner/repo1":
			_, _ = w.Write([]byte(`{"score":8.0,"checks":[{"name":"Maintained","score":10,"reason":"active"}]}`))
		case "/projects/github.com/owner/repo2":
			_, _ = w.Write([]byte(`{"score":6.0,"checks":[{"name":"Maintained","score":5,"reason":"stale"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer ts.Close()

	results := c.FetchScorecardBatch(context.Background(), []string{
		"github.com/owner/repo1",
		"github.com/owner/repo2",
		"github.com/owner/missing",
	})

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	r1, ok := results["github.com/owner/repo1"]
	if !ok {
		t.Fatal("expected result for repo1")
	}
	if r1.OverallScore != 8.0 {
		t.Errorf("expected repo1 score 8.0, got %f", r1.OverallScore)
	}
	r2, ok := results["github.com/owner/repo2"]
	if !ok {
		t.Fatal("expected result for repo2")
	}
	if r2.OverallScore != 6.0 {
		t.Errorf("expected repo2 score 6.0, got %f", r2.OverallScore)
	}
}

func TestFetchScorecardBatch_NilClient(t *testing.T) {
	var c *Client
	results := c.FetchScorecardBatch(context.Background(), []string{"github.com/test/repo"})
	if results != nil {
		t.Error("expected nil results for nil client")
	}
}

func TestFetchScorecardBatch_Empty(t *testing.T) {
	c, ts := newTestClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		// handler should not be called for empty input
	})
	defer ts.Close()

	results := c.FetchScorecardBatch(context.Background(), nil)
	if results != nil {
		t.Error("expected nil results for empty input")
	}
}

func TestFetchScorecardBatch_ContextCancelled(t *testing.T) {
	c, ts := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"score":5.0,"checks":[]}`)) // best-effort write
	})
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	results := c.FetchScorecardBatch(ctx, []string{"github.com/test/repo"})
	// With cancelled context, results should be empty or nil
	if len(results) > 0 {
		t.Error("expected no results for cancelled context")
	}
}
