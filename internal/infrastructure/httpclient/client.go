package httpclient

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
)

// RetryConfig defines retry configuration.
// NOTE: Moved from pkg/httpclient (now internal) to avoid exposing infrastructure concerns publicly.
//
// 429 Too Many Requests responses are always retried up to MaxRetries —
// independently of RetryOn5xx — because rate-limiting is a transient condition
// and the server's Retry-After header carries the only timing signal we have.
// This behavior is built into the retrying client, including DoWithRetryFunc, so
// callers that need 429 to fail fast must set MaxRetries to 0 or bypass this client.
type RetryConfig struct {
	MaxRetries        int           // Maximum number of retries
	BaseBackoff       time.Duration // Base backoff time
	MaxBackoff        time.Duration // Maximum backoff time
	RetryOn5xx        bool          // Whether to retry on 5xx errors
	RetryOnNetworkErr bool          // Whether to retry on network errors
}

// DefaultRetryConfig returns default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:        3,
		BaseBackoff:       1 * time.Second,
		MaxBackoff:        60 * time.Second,
		RetryOn5xx:        true,
		RetryOnNetworkErr: true,
	}
}

// Client is an HTTP client with retry functionality.
type Client struct {
	http   *http.Client
	config RetryConfig
}

// NewClient creates a new HTTP client with retry support.
func NewClient(httpClient *http.Client, config RetryConfig) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	return &Client{http: httpClient, config: config}
}

// Do executes HTTP request with retry functionality.
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	return c.DoWithRetryFunc(ctx, req, nil)
}

// RetryDecider is a function type that determines whether to retry.
type RetryDecider func(resp *http.Response, err error, attempt int) (bool, time.Duration)

// DoWithRetryFunc executes HTTP request with custom retry logic.
func (c *Client) DoWithRetryFunc(ctx context.Context, req *http.Request, retryDecider RetryDecider) (*http.Response, error) {
	var lastErr error
	var originalBody []byte

	if req.Body != nil { // capture body for retries
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, common.NewIOError("failed to read request body", err).WithContext("request_url", req.URL.String())
		}
		_ = req.Body.Close() // best-effort cleanup, body already read
		originalBody = body
		req.Body = io.NopCloser(bytes.NewReader(originalBody))
	}

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		select { // context cancellation
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return nil, common.NewTimeoutError("request timeout", ctx.Err()).WithContext("request_url", req.URL.String())
			}
			return nil, ctx.Err()
		default:
		}

		if originalBody != nil { // reset body each attempt
			req.Body = io.NopCloser(bytes.NewReader(originalBody))
		}

		resp, err := c.http.Do(req.WithContext(ctx))

		if retryDecider != nil { // custom retry path
			shouldRetry, waitTime := retryDecider(resp, err, attempt)
			if !shouldRetry || attempt >= c.config.MaxRetries {
				if err != nil {
					return nil, err
				}
				return resp, nil
			}
			if resp != nil {
				_ = resp.Body.Close() // best-effort cleanup
			}
			slog.Debug("custom_retry_logic", "attempt", attempt+1, "max_attempts", c.config.MaxRetries+1, "wait_time", waitTime)
			select {
			case <-ctx.Done():
				if ctx.Err() == context.DeadlineExceeded {
					return nil, common.NewTimeoutError("request timeout during retry", ctx.Err()).WithContext("request_url", req.URL.String())
				}
				return nil, ctx.Err()
			case <-time.After(waitTime):
				continue
			}
		}

		if err != nil { // network error handling
			lastErr = common.NewNetworkError("network error during HTTP request", err).WithContext("request_url", req.URL.String()).WithContext("attempt", attempt+1)
			if c.config.RetryOnNetworkErr && attempt < c.config.MaxRetries {
				backoff := c.calculateBackoff(attempt)
				slog.Warn("network error, retrying", "attempt", attempt+1, "max_attempts", c.config.MaxRetries+1, "error", err, "backoff", backoff)
				select {
				case <-ctx.Done():
					if ctx.Err() == context.DeadlineExceeded {
						return nil, common.NewTimeoutError("request timeout during network retry", ctx.Err()).WithContext("request_url", req.URL.String())
					}
					return nil, ctx.Err()
				case <-time.After(backoff):
					continue
				}
			}
			continue
		}

		if resp.StatusCode < 400 {
			return resp, nil
		} // success

		if resp.StatusCode == http.StatusTooManyRequests { // rate limit
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
			retryAfter := resp.Header.Get("Retry-After")
			_ = resp.Body.Close() // best-effort cleanup
			if attempt < c.config.MaxRetries {
				wait := c.rateLimitBackoff(retryAfter, attempt)
				slog.Warn("rate limit reached, retrying",
					"status_code", resp.StatusCode,
					"attempt", attempt+1,
					"max_attempts", c.config.MaxRetries+1,
					"retry_after_header", retryAfter,
					"wait", wait,
					"response_body", string(body))
				timer := time.NewTimer(wait)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					if ctx.Err() == context.DeadlineExceeded {
						return nil, common.NewTimeoutError("request timeout during rate limit retry", ctx.Err()).WithContext("request_url", req.URL.String())
					}
					return nil, ctx.Err()
				case <-timer.C:
					continue
				}
			}
			return nil, common.NewRateLimitError("rate limit reached", nil).
				WithContext("request_url", req.URL.String()).
				WithContext("response_body", string(body)).
				WithContext("status_code", resp.StatusCode).
				WithContext("retry_after_header", retryAfter).
				WithContext("max_attempts", c.config.MaxRetries+1)
		}

		if resp.StatusCode < 500 {
			return resp, nil
		} // non-retryable 4xx

		if resp.StatusCode >= 500 && c.config.RetryOn5xx { // server error retry
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
			_ = resp.Body.Close() // best-effort cleanup
			if attempt < c.config.MaxRetries {
				backoff := c.calculateBackoff(attempt)
				slog.Warn("server error, retrying", "status_code", resp.StatusCode, "attempt", attempt+1, "max_attempts", c.config.MaxRetries+1, "response_body", string(body), "backoff", backoff)
				select {
				case <-ctx.Done():
					if ctx.Err() == context.DeadlineExceeded {
						return nil, common.NewTimeoutError("request timeout during server error retry", ctx.Err()).WithContext("request_url", req.URL.String())
					}
					return nil, ctx.Err()
				case <-time.After(backoff):
					continue
				}
			}
			lastErr = common.NewNetworkError("server error", nil).WithContext("request_url", req.URL.String()).WithContext("status_code", resp.StatusCode).WithContext("response_body", string(body))
			continue
		}

		return resp, nil // other status codes
	}

	if lastErr != nil {
		return nil, common.NewNetworkError("request failed after all retries", lastErr).WithContext("max_attempts", c.config.MaxRetries+1).WithContext("request_url", req.URL.String())
	}
	return nil, common.NewNetworkError("request failed after all retries with no specific error", nil).WithContext("max_attempts", c.config.MaxRetries+1).WithContext("request_url", req.URL.String())
}

func (c *Client) calculateBackoff(attempt int) time.Duration { // exponential backoff with cap
	backoff := time.Duration(math.Pow(2, float64(attempt))) * c.config.BaseBackoff
	if backoff > c.config.MaxBackoff {
		backoff = c.config.MaxBackoff
	}
	return backoff
}

// rateLimitBackoff returns the wait duration before retrying a 429 response.
// Honors the server's Retry-After header (per RFC 9110 §10.2.3) according to
// the following precedence:
//
//  1. Header parses to a past or present HTTP-date (non-positive duration) →
//     return 0 ("retry immediately"); do not fall through to exponential backoff.
//  2. Header parses and the resulting duration fits within MaxBackoff →
//     return the exact duration.
//  3. Header is missing, unparseable, or its duration exceeds MaxBackoff →
//     fall back to the same exponential backoff used for 5xx retries.
func (c *Client) rateLimitBackoff(retryAfter string, attempt int) time.Duration {
	if d, ok := parseRetryAfter(retryAfter, time.Now()); ok {
		if d <= 0 {
			return 0
		}
		if d <= c.config.MaxBackoff {
			return d
		}
	}
	return c.calculateBackoff(attempt)
}

// maxErrorBodyBytes caps how much of a 429/5xx response body we read and
// log/attach to errors. Rate-limited responses are retried, so an uncapped
// read would accumulate memory and log volume across attempts. 64 KiB is
// enough for diagnostic messages without risking large HTML error pages.
const maxErrorBodyBytes = 64 << 10

// maxSafeSeconds is the largest delay-seconds value whose multiplication by
// time.Second will not overflow time.Duration (int64 nanoseconds). Values
// beyond this are rejected so the caller falls back to exponential backoff.
const maxSafeSeconds = math.MaxInt64 / int64(time.Second)

// parseRetryAfter parses an RFC 9110 Retry-After header value. The header
// can be either a delay-seconds integer (e.g., "30") or an HTTP-date. Returns
// the resolved duration and true when the header was parseable, otherwise
// (0, false). The "now" parameter is injected so tests can pin time.
//
// Per RFC, delay-seconds must be a non-negative integer; values exceeding
// maxSafeSeconds (the time.Duration overflow boundary) are rejected so the
// caller falls back to exponential backoff. HTTP-date in the past resolves
// to a non-positive duration which the caller should treat as "retry
// immediately".
func parseRetryAfter(header string, now time.Time) (time.Duration, bool) {
	s := strings.TrimSpace(header)
	if s == "" {
		return 0, false
	}
	if secs, err := strconv.ParseInt(s, 10, 64); err == nil {
		if secs < 0 {
			return 0, false
		}
		if secs > maxSafeSeconds {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(s); err == nil {
		return t.Sub(now), true
	}
	return 0, false
}
