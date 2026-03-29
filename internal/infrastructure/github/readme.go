package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// FetchREADME fetches the README raw text for a GitHub repository at its canonical default branch.
// This implementation requires the caller to supply the already-resolved default branch (from GraphQL)
// and therefore does not guess or fall back to common branch names. This removes redundant network
// requests and eliminates ambiguity when repositories use non-standard default branches.
// Filenames tried (in order): README.md, README.MD, README, README.txt, README.rst.
// Returns (contents, rawURL, nil) on success; ("", "", error) otherwise.
func FetchREADME(ctx context.Context, owner, repo, defaultBranch string) (string, string, error) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	defaultBranch = strings.TrimSpace(defaultBranch)
	if owner == "" || repo == "" {
		return "", "", common.NewValidationError("owner and repo are required")
	}
	if defaultBranch == "" {
		return "", "", common.NewValidationError("default branch is required (provide GraphQL-resolved name)")
	}

	// Short timeout and minimal retries to avoid slowing batches.
	httpCli := httpclient.NewClient(&http.Client{Timeout: 5 * time.Second}, httpclient.RetryConfig{MaxRetries: 1, BaseBackoff: 300 * time.Millisecond, MaxBackoff: 1 * time.Second, RetryOn5xx: true, RetryOnNetworkErr: true})

	names := []string{"README.md", "README.MD", "README", "README.txt", "README.rst"}
	for _, nm := range names {
		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, defaultBranch, nm)
		attemptCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, rawURL, nil)
		if err != nil {
			cancel()
			continue
		}
		req.Header.Set("Accept", "text/plain, text/markdown;q=0.9, */*;q=0.8")
		resp, err := httpCli.Do(attemptCtx, req)
		if err != nil {
			cancel()
			continue
		}
		if resp.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // cap 1MB
			_ = resp.Body.Close() // best-effort cleanup
			cancel()
			if len(body) > 0 {
				return string(body), rawURL, nil
			}
			continue
		}
		_, _ = io.CopyN(io.Discard, resp.Body, 1024) // best-effort drain before close
		_ = resp.Body.Close() // best-effort cleanup
		cancel()
	}
	return "", "", common.NewResourceNotFoundError("readme not found on default branch")
}
