package github

import "time"

// resetRateLimitAggregation resets aggregated rate limit tracking counters.
func (c *Client) resetRateLimitAggregation() {
	c.rateMu.Lock()
	c.rateLimitTotalCost = 0
	c.rateLimitQueries = 0
	c.rateLimitRemaining = 0
	c.rateLimitResetAt = ""
	c.rateMu.Unlock()
}

// recordRateLimit records a single query's rate limit info into aggregated counters.
func (c *Client) recordRateLimit(cost, remaining int, resetAt string) {
	c.rateMu.Lock()
	c.rateLimitTotalCost += cost
	c.rateLimitQueries++
	c.rateLimitRemaining = remaining // latest snapshot
	c.rateLimitResetAt = resetAt
	c.rateMu.Unlock()
}

// snapshotRateLimit returns a consistent snapshot of aggregated rate limit metrics.
func (c *Client) snapshotRateLimit() (costTotal int, remaining int, resetAt string, avgCost float64) {
	c.rateMu.Lock()
	defer c.rateMu.Unlock()
	costTotal = c.rateLimitTotalCost
	remaining = c.rateLimitRemaining
	resetAt = c.rateLimitResetAt
	if c.rateLimitQueries > 0 {
		avgCost = float64(costTotal) / float64(c.rateLimitQueries)
	}
	return
}

// RateLimitSummary returns the latest remaining quota and reset time
// from GitHub GraphQL API rate limit tracking.
// resetAt is an ISO-8601 timestamp (empty if no API calls were made).
func (c *Client) RateLimitSummary() (remaining int, resetAt string) {
	_, remaining, resetAt, _ = c.snapshotRateLimit()
	return
}

// formatResetLocal converts an RFC3339 reset timestamp to local time for display.
// Returns the original string unchanged if parsing fails, or empty string if input is empty.
func formatResetLocal(resetAt string) string {
	if resetAt == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, resetAt); err == nil {
		return t.Local().Format("15:04 MST")
	}
	return resetAt
}
