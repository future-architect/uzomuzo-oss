package actionscan

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/ghaworkflow"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
)

func TestNewDiscoveryService_NilClient(t *testing.T) {
	_, err := NewDiscoveryService(nil, 5)
	if err == nil {
		t.Fatal("expected error for nil github client")
	}
}

func TestDiscoverActions_InvalidURLs(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token:          "test-token",
			MaxConcurrency: 5,
		},
	}
	client := github.NewClient(cfg)
	svc, err := NewDiscoveryService(client, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	directURLs, localActions, transitiveActions, errs, err := svc.DiscoverActions(context.Background(), []string{"not-a-url", "https://gitlab.com/foo/bar"}, false)
	if err != nil {
		t.Fatalf("DiscoverActions should not return fatal error: %v", err)
	}
	if len(directURLs) != 0 {
		t.Errorf("expected 0 direct URLs, got %d", len(directURLs))
	}
	if len(localActions) != 0 {
		t.Errorf("expected 0 local actions, got %d", len(localActions))
	}
	if len(transitiveActions) != 0 {
		t.Errorf("expected 0 transitive actions, got %d", len(transitiveActions))
	}
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got %d", len(errs))
	}
}

func TestBuildActionRefFromGitHubURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantPath  string
		wantErr   bool
	}{
		{
			name:      "simple repo URL",
			url:       "https://github.com/actions/checkout",
			wantOwner: "actions",
			wantRepo:  "checkout",
		},
		{
			name:      "subdirectory action",
			url:       "https://github.com/actions/cache/save",
			wantOwner: "actions",
			wantRepo:  "cache",
			wantPath:  "save",
		},
		{
			name:      "deep subdirectory",
			url:       "https://github.com/owner/repo/path/to/action",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantPath:  "path/to/action",
		},
		{
			name:      "URL with query params",
			url:       "https://github.com/owner/repo?tab=readme",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:    "invalid URL",
			url:     "not-a-github-url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := buildActionRefFromGitHubURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ref.Owner != tt.wantOwner {
				t.Errorf("Owner = %q, want %q", ref.Owner, tt.wantOwner)
			}
			if ref.Repo != tt.wantRepo {
				t.Errorf("Repo = %q, want %q", ref.Repo, tt.wantRepo)
			}
			if ref.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", ref.Path, tt.wantPath)
			}
		})
	}
}

func TestActionRefKey(t *testing.T) {
	tests := []struct {
		name string
		ref  ghaworkflow.ActionRef
		want string
	}{
		{
			name: "root action",
			ref:  ghaworkflow.ActionRef{Owner: "actions", Repo: "checkout"},
			want: "actions/checkout",
		},
		{
			name: "subdirectory action",
			ref:  ghaworkflow.ActionRef{Owner: "actions", Repo: "cache", Path: "save"},
			want: "actions/cache/save",
		},
		{
			name: "deep subdirectory",
			ref:  ghaworkflow.ActionRef{Owner: "owner", Repo: "repo", Path: "path/to/action"},
			want: "owner/repo/path/to/action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := actionRefKey(tt.ref)
			if got != tt.want {
				t.Errorf("actionRefKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDiscoverActions_EmptyInput(t *testing.T) {
	cfg := &config.Config{
		GitHub: config.GitHubConfig{
			Token:          "test-token",
			MaxConcurrency: 5,
		},
	}
	client := github.NewClient(cfg)
	svc, err := NewDiscoveryService(client, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	directURLs, localActions, transitiveActions, errs, err := svc.DiscoverActions(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(directURLs) != 0 {
		t.Errorf("expected 0 direct URLs, got %d", len(directURLs))
	}
	if len(localActions) != 0 {
		t.Errorf("expected 0 local actions, got %d", len(localActions))
	}
	if len(transitiveActions) != 0 {
		t.Errorf("expected 0 transitive actions, got %d", len(transitiveActions))
	}
	if len(errs) != 0 {
		t.Errorf("expected 0 errors, got %d", len(errs))
	}
}

// TestDiscoverFromRepo_ContextCancellation verifies that when the context is cancelled
// while workflow file fetches are queued behind the semaphore, the cancellation path
// records context errors and does not deadlock or leak goroutines.
func TestDiscoverFromRepo_ContextCancellation(t *testing.T) {
	// We create more YAML files than maxFileFetchConcurrency (5) so that some
	// iterations must block on semaphore acquisition, hitting the ctx.Done() branch.
	fileCount := maxFileFetchConcurrency + 3 // 8 files total
	fileNames := make([]string, fileCount)
	for i := range fileNames {
		fileNames[i] = fmt.Sprintf("wf%d.yml", i)
	}

	// Precompute directory listing JSON outside the handler goroutine so that
	// t.Fatalf (called by directoryJSON on marshal error) is never invoked
	// from a non-test goroutine.
	dirJSON := directoryJSON(t, fileNames)

	// Track how many file-content requests arrive at the server.
	var fetchStarted atomic.Int32
	// Gate: file-content handlers block until this channel is closed.
	gate := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/repos/")
		parts := strings.SplitN(path, "/contents/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		contentPath := parts[1]

		accept := r.Header.Get("Accept")

		// Directory listing request.
		if contentPath == ".github/workflows" && accept != "application/vnd.github.raw" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(dirJSON)
			return
		}

		// File content request — signal arrival and block until gate opens.
		if accept == "application/vnd.github.raw" {
			fetchStarted.Add(1)
			<-gate
			// Return a minimal valid workflow YAML.
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("name: CI\non: push\njobs:\n  b:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n"))
			return
		}

		http.NotFound(w, r)
	}))
	defer srv.Close()

	svc := newTestService(t, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		resultURLs []string
		resultErrs map[string]error
		done       sync.WaitGroup
	)
	done.Add(1)
	go func() {
		defer done.Done()
		resultURLs, _, resultErrs = svc.discoverFromRepo(ctx, "testowner", "testrepo")
	}()

	// Wait until all semaphore slots are occupied (maxFileFetchConcurrency goroutines
	// are blocking on the gate inside the HTTP handler).
	deadline := time.After(10 * time.Second)
	for fetchStarted.Load() < int32(maxFileFetchConcurrency) {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d fetches to start (got %d)",
				maxFileFetchConcurrency, fetchStarted.Load())
		default:
			runtime.Gosched()
			time.Sleep(1 * time.Millisecond)
		}
	}

	// Cancel the context. The remaining files (fileCount - maxFileFetchConcurrency)
	// are waiting to acquire the semaphore and should take the ctx.Done() branch.
	cancel()

	// Unblock the in-flight HTTP handlers so their goroutines can complete.
	close(gate)

	doneCh := make(chan struct{})
	go func() {
		done.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for discoverFromRepo workers to finish after cancellation; fetches started=%d", fetchStarted.Load())
	}

	// Verify: at least one error should be context.Canceled from the cancellation path.
	cancelledCount := 0
	for _, e := range resultErrs {
		if e == context.Canceled {
			cancelledCount++
		}
	}
	if cancelledCount == 0 {
		t.Error("expected at least one context.Canceled error from the semaphore cancellation path")
	}

	// The total of fetched files + cancelled files should equal fileCount.
	// (Some in-flight fetches may also fail due to cancelled context, which is acceptable.)
	t.Logf("fetches started: %d, cancelled errors: %d, total errors: %d, URLs found: %d",
		fetchStarted.Load(), cancelledCount, len(resultErrs), len(resultURLs))
}
