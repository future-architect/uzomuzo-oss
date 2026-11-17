package depsdev

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// TestResolveRepoURLsBatch_Cancellation pins issue #450: once ctx is cancelled,
// resolveRepoURLsBatch must not start new resolutions, and without cancellation
// the PURL order under one repo URL is deterministic.
//
// wantRepoLen is the number of PURLs that end up in repoURLMap or purlsWithoutRepo.
// It is what separates "dispatch stopped" from "nothing ran": a PURL that was
// dispatched is always accounted for in one of the two outputs. wantStarted counts
// transport calls; httpclient returns before the transport once ctx is cancelled,
// so it cannot detect extra dispatch on its own.
func TestResolveRepoURLsBatch_Cancellation(t *testing.T) {
	const workers = 10 // matches maxWorkers in resolveRepoURLsBatch

	tests := []struct {
		name        string
		keyCount    int
		cancel      string // "", "before", "midrun"
		repeat      int    // the pre-cancelled case races a select, so run it many times
		wantStarted int32
		wantRepoLen int
	}{
		{name: "cancelled before call resolves nothing", keyCount: 30, cancel: "before", repeat: 50, wantStarted: 0, wantRepoLen: 0},
		{name: "cancelled mid-run resolves only in-flight keys", keyCount: 30, cancel: "midrun", repeat: 1, wantStarted: workers, wantRepoLen: workers},
		{name: "not cancelled resolves every key in sorted order", keyCount: 8, cancel: "", repeat: 1, wantStarted: 8, wantRepoLen: 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for n := 0; n < tt.repeat; n++ {
				runResolveCancelCase(t, tt.keyCount, tt.cancel, workers, tt.wantStarted, tt.wantRepoLen)
			}
		})
	}
}

func runResolveCancelCase(t *testing.T, keyCount int, cancelMode string, workers int, wantStarted int32, wantRepoLen int) {
	t.Helper()
	var started atomic.Int32
	release := make(chan struct{})
	closeRelease := sync.OnceFunc(func() { close(release) })
	t.Cleanup(closeRelease) // never leave blocked transport goroutines behind on t.Fatal
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		started.Add(1)
		<-release
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})
	c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "https://deps.example", Timeout: time.Second})
	c.client = httpclient.NewClient(&http.Client{Transport: transport}, httpclient.RetryConfig{})
	c.goproxy = nil

	// Every key has a replaced default, so each resolution makes exactly one
	// transport call (the replaced-default package lookup), which blocks on release.
	pkgs := make(map[string]*PackageResponse, keyCount)
	rel := make(map[string]ReleaseInfo, keyCount)
	for i := 0; i < keyCount; i++ {
		p := fmt.Sprintf("pkg:golang/github.com/acme/demo@v%02d", i)
		pkgs[p] = nil
		rel[p] = ReleaseInfo{replacedDefault: Version{PURL: p + "-rc1"}}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if cancelMode == "before" {
		cancel()
	}
	if cancelMode != "midrun" {
		closeRelease()
	}

	type out struct {
		repos   map[string][]string
		missing []string
	}
	done := make(chan out, 1)
	go func() {
		r, m := c.resolveRepoURLsBatch(ctx, pkgs, rel)
		done <- out{r, m}
	}()

	if cancelMode == "midrun" {
		deadline := time.After(5 * time.Second)
		for started.Load() < int32(workers) {
			select {
			case <-deadline:
				t.Fatalf("only %d workers started, want %d", started.Load(), workers)
			case <-time.After(time.Millisecond):
			}
		}
		cancel()
		closeRelease()
	}

	var got out
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("resolveRepoURLsBatch did not return")
	}

	if gotStarted := started.Load(); gotStarted != wantStarted {
		t.Errorf("transport calls = %d, want %d", gotStarted, wantStarted)
	}
	all := append([]string(nil), got.missing...)
	for _, ps := range got.repos {
		all = append(all, ps...)
	}
	if len(all) != wantRepoLen {
		t.Errorf("resolved PURLs = %d, want %d", len(all), wantRepoLen)
	}
	if cancelMode == "" {
		// Zero-padded suffixes make lexical order equal numeric order.
		wantOrder := make([]string, 0, keyCount)
		for i := 0; i < keyCount; i++ {
			wantOrder = append(wantOrder, fmt.Sprintf("pkg:golang/github.com/acme/demo@v%02d", i))
		}
		if !reflect.DeepEqual(all, wantOrder) {
			t.Errorf("PURL order = %v, want sorted %v", all, wantOrder)
		}
	}
}

// TestGetDetailsForPURLs_CancelledAfterProjectBatch covers the second ctx check
// in fetchBatchPURLs: the context is cancelled while the project batch call runs.
func TestGetDetailsForPURLs_CancelledAfterProjectBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var projectBatchCalls atomic.Int32
	c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "https://deps.example", Timeout: time.Second})
	c.client = httpclient.NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/projectbatch") {
			projectBatchCalls.Add(1)
			cancel()
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"responses":[]}`)), Header: make(http.Header), Request: r}, nil
		}
		if strings.HasPrefix(r.URL.Path, "/v3alpha/purl/") {
			// A package whose related project yields a repo URL, so the project batch is called.
			body := `{"version":{"versionKey":{"system":"GO","name":"github.com/acme/demo","version":"v1.0.0"},"relatedProjects":[{"projectKey":{"id":"github.com/acme/demo"},"relationType":"SOURCE_REPO"}]}}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})}, httpclient.RetryConfig{})
	c.goproxy = nil

	_, err := c.GetDetailsForPURLs(ctx, []string{"pkg:golang/github.com/acme/demo"})
	if projectBatchCalls.Load() == 0 {
		t.Fatal("project batch was never called; the second ctx check was not reached")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// TestGetDetailsForPURLs_CancelledContextReturnsError ensures a cancelled ctx surfaces
// as an error rather than as per-PURL "not found" results (issue #450).
func TestGetDetailsForPURLs_CancelledContextReturnsError(t *testing.T) {
	c := NewDepsDevClient(&config.DepsDevConfig{BaseURL: "https://deps.example", Timeout: time.Second})
	c.client = httpclient.NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("unexpected request: %s", r.URL.String())
	})}, httpclient.RetryConfig{})
	c.goproxy = nil

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.GetDetailsForPURLs(ctx, []string{"pkg:golang/github.com/acme/demo"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
