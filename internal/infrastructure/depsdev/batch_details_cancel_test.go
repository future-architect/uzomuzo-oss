package depsdev

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// TestResolveRepoURLsBatch_Cancellation pins issue #450: once ctx is cancelled,
// resolveRepoURLsBatch must not start new resolutions, and without cancellation
// the PURL order under one repo URL is deterministic.
func TestResolveRepoURLsBatch_Cancellation(t *testing.T) {
	const workers = 10 // matches maxWorkers in resolveRepoURLsBatch

	tests := []struct {
		name        string
		keyCount    int
		cancel      string // "", "before", "midrun"
		wantStarted int32
		wantRepoLen int // total PURLs across repoURLMap + purlsWithoutRepo
	}{
		{name: "cancelled before call starts nothing", keyCount: 30, cancel: "before", wantStarted: 0, wantRepoLen: 0},
		{name: "cancelled mid-run starts no new work", keyCount: 30, cancel: "midrun", wantStarted: workers},
		{name: "not cancelled resolves every key", keyCount: 3, cancel: "", wantStarted: 3, wantRepoLen: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var started atomic.Int32
			release := make(chan struct{})
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
			pkgs := make(map[string]*PackageResponse, tt.keyCount)
			rel := make(map[string]ReleaseInfo, tt.keyCount)
			wantOrder := make([]string, 0, tt.keyCount)
			for i := 0; i < tt.keyCount; i++ {
				// Reverse suffix order so sorted order differs from construction order.
				p := fmt.Sprintf("pkg:golang/github.com/acme/demo@v%d", tt.keyCount-i)
				pkgs[p] = nil
				rel[p] = ReleaseInfo{replacedDefault: Version{PURL: p + "-rc1"}}
			}
			for i := 1; i <= tt.keyCount; i++ {
				wantOrder = append(wantOrder, fmt.Sprintf("pkg:golang/github.com/acme/demo@v%d", i))
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel == "before" {
				cancel()
			}
			if tt.cancel == "" || tt.cancel == "before" {
				close(release)
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

			if tt.cancel == "midrun" {
				deadline := time.After(5 * time.Second)
				for started.Load() < workers {
					select {
					case <-deadline:
						t.Fatalf("only %d workers started, want %d", started.Load(), workers)
					case <-time.After(time.Millisecond):
					}
				}
				cancel()
				close(release)
			}

			var got out
			select {
			case got = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("resolveRepoURLsBatch did not return")
			}

			if gotStarted := started.Load(); gotStarted != tt.wantStarted {
				t.Errorf("started = %d, want %d", gotStarted, tt.wantStarted)
			}
			total := len(got.missing)
			for _, ps := range got.repos {
				total += len(ps)
			}
			if tt.cancel != "midrun" && total != tt.wantRepoLen {
				t.Errorf("resolved PURLs = %d, want %d", total, tt.wantRepoLen)
			}
			if tt.cancel == "midrun" && total > workers {
				t.Errorf("resolved PURLs = %d, want at most %d", total, workers)
			}
			if tt.cancel == "" {
				all := append([]string(nil), got.missing...)
				for _, ps := range got.repos {
					all = append(all, ps...)
				}
				if !reflect.DeepEqual(all, wantOrder) {
					t.Errorf("PURL order = %v, want sorted %v", all, wantOrder)
				}
			}
		})
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
