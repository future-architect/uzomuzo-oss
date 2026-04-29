package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		header      string
		wantOK      bool
		wantSeconds float64 // approximate; HTTP-date paths may have rounding
	}{
		{name: "empty", header: "", wantOK: false},
		{name: "whitespace", header: "   ", wantOK: false},
		{name: "delay_seconds_30", header: "30", wantOK: true, wantSeconds: 30},
		{name: "delay_seconds_zero", header: "0", wantOK: true, wantSeconds: 0},
		{name: "delay_seconds_with_padding", header: "  30  ", wantOK: true, wantSeconds: 30},
		{name: "delay_seconds_negative_rejected", header: "-1", wantOK: false},
		{name: "delay_seconds_invalid_text", header: "soon", wantOK: false},
		{name: "delay_seconds_overflow_clamped", header: "999999999999", wantOK: true, wantSeconds: maxRetryAfterSeconds},
		{name: "http_date_imf_fixdate_future", header: "Wed, 29 Apr 2026 12:00:30 GMT", wantOK: true, wantSeconds: 30},
		{name: "http_date_imf_fixdate_past_negative", header: "Wed, 29 Apr 2026 11:59:30 GMT", wantOK: true, wantSeconds: -30},
		{name: "http_date_invalid_format", header: "not a date", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tt.header, now)
			if ok != tt.wantOK {
				t.Fatalf("parseRetryAfter(%q) ok = %v, want %v (got=%v)", tt.header, ok, tt.wantOK, got)
			}
			if !tt.wantOK {
				return
			}
			if delta := got.Seconds() - tt.wantSeconds; delta < -1 || delta > 1 {
				t.Errorf("parseRetryAfter(%q) = %v, want approximately %vs", tt.header, got, tt.wantSeconds)
			}
		})
	}
}

func TestRateLimitBackoff(t *testing.T) {
	c := NewClient(nil, RetryConfig{
		MaxRetries:  3,
		BaseBackoff: 10 * time.Millisecond,
		MaxBackoff:  500 * time.Millisecond,
	})
	tests := []struct {
		name        string
		header      string
		attempt     int
		wantBetween [2]time.Duration // inclusive bounds
	}{
		{
			name:        "header_within_cap_used_directly",
			header:      "0", // 0 seconds — well within cap
			attempt:     0,
			wantBetween: [2]time.Duration{0, 0},
		},
		{
			name:        "header_exceeds_cap_falls_back_to_exponential",
			header:      "9999", // far beyond MaxBackoff
			attempt:     0,
			wantBetween: [2]time.Duration{10 * time.Millisecond, 10 * time.Millisecond},
		},
		{
			name:        "no_header_uses_exponential",
			header:      "",
			attempt:     1,
			wantBetween: [2]time.Duration{20 * time.Millisecond, 20 * time.Millisecond},
		},
		{
			name:        "invalid_header_uses_exponential",
			header:      "bogus",
			attempt:     2,
			wantBetween: [2]time.Duration{40 * time.Millisecond, 40 * time.Millisecond},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.rateLimitBackoff(tt.header, tt.attempt)
			if got < tt.wantBetween[0] || got > tt.wantBetween[1] {
				t.Errorf("rateLimitBackoff(%q, %d) = %v, want in [%v, %v]", tt.header, tt.attempt, got, tt.wantBetween[0], tt.wantBetween[1])
			}
		})
	}
}

// TestDo_RateLimitRetriesThenSucceeds verifies that a 429 response with a
// short Retry-After header triggers a retry, and a subsequent 200 ends the
// loop.
func TestDo_RateLimitRetriesThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("slow down"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(nil, RetryConfig{
		MaxRetries:  3,
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  100 * time.Millisecond,
	})
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("server call count = %d, want 2 (one rate-limited, one success)", got)
	}
}

// TestDo_RateLimitRetriesExhaustedReturnsRateLimitError verifies that when a
// server keeps returning 429 past MaxRetries, the client returns a RateLimit
// error tagged with the Retry-After header for diagnostics.
func TestDo_RateLimitRetriesExhaustedReturnsRateLimitError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(nil, RetryConfig{
		MaxRetries:  2,
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  10 * time.Millisecond,
	})
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := c.Do(context.Background(), req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("Do returned nil error, want RateLimitError")
	}
	var sErr *common.ScorecardError
	if !errors.As(err, &sErr) || sErr.Type != common.ErrorTypeRateLimit {
		t.Fatalf("err type = %T %v, want RateLimitError", err, err)
	}
	wantCalls := int32(c.config.MaxRetries + 1)
	if got := atomic.LoadInt32(&calls); got != wantCalls {
		t.Errorf("server call count = %d, want %d", got, wantCalls)
	}
}

// TestDo_RateLimitContextCancellationDuringWait verifies that ctx cancellation
// while sleeping for a Retry-After window terminates the retry loop promptly.
func TestDo_RateLimitContextCancellationDuringWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60") // long enough to outlast the test
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(nil, RetryConfig{
		MaxRetries:  3,
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  120 * time.Second, // accept the 60s header so we sleep, not back off
	})
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	resp, err := c.Do(ctx, req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("Do returned nil error, want context.Canceled")
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context-derived error", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Do took %v after cancel; expected prompt return", elapsed)
	}
}

// TestDo_RateLimitHTTPDateHeader verifies the end-to-end HTTP-date branch:
// the server emits Retry-After as an IMF-fixdate, the client computes a
// duration via time.Sub, and the retry succeeds.
func TestDo_RateLimitHTTPDateHeader(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			// 50ms in the future — short enough to keep the test fast,
			// long enough to be unambiguously a future date.
			w.Header().Set("Retry-After", time.Now().Add(50*time.Millisecond).UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(nil, RetryConfig{
		MaxRetries:  3,
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  5 * time.Second, // generous so the HTTP-date is honored
	})
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("server call count = %d, want 2", got)
	}
}

// TestDo_RateLimitDeadlineExceededDuringWait covers the DeadlineExceeded
// branch in the retry loop, which wraps ctx.Err into a TimeoutError. Pairs
// with TestDo_RateLimitContextCancellationDuringWait (Canceled branch).
func TestDo_RateLimitDeadlineExceededDuringWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(nil, RetryConfig{
		MaxRetries:  3,
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  120 * time.Second,
	})
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	resp, err := c.Do(ctx, req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("Do returned nil error, want TimeoutError")
	}
	var sErr *common.ScorecardError
	if !errors.As(err, &sErr) || sErr.Type != common.ErrorTypeTimeout {
		t.Fatalf("err type = %v %v, want TimeoutError", err, err)
	}
}

// TestDo_RateLimitNoHeaderUsesBackoff verifies that a 429 response without a
// Retry-After header still retries (using the configured exponential backoff).
func TestDo_RateLimitNoHeaderUsesBackoff(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := NewClient(nil, RetryConfig{
		MaxRetries:  3,
		BaseBackoff: 1 * time.Millisecond,
		MaxBackoff:  10 * time.Millisecond,
	})
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("server call count = %d, want 2", got)
	}
}
