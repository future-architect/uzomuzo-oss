package httpclient

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
)

// RetryConfig defines retry configuration.
// NOTE: Moved from pkg/httpclient (now internal) to avoid exposing infrastructure concerns publicly.
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
		req.Body.Close()
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
				resp.Body.Close()
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
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, common.NewRateLimitError("rate limit reached", nil).WithContext("request_url", req.URL.String()).WithContext("response_body", string(body)).WithContext("status_code", resp.StatusCode)
		}

		if resp.StatusCode < 500 {
			return resp, nil
		} // non-retryable 4xx

		if resp.StatusCode >= 500 && c.config.RetryOn5xx { // server error retry
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
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
