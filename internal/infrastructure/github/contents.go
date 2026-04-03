package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// DirectoryEntry represents a single item returned by the GitHub Contents API.
type DirectoryEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"` // "file" or "dir"
	DownloadURL string `json:"download_url"`
}

// FetchDirectoryContents lists files in a directory using the GitHub REST Contents API.
// Returns nil (not an error) when the directory does not exist (HTTP 404).
func (c *Client) FetchDirectoryContents(ctx context.Context, owner, repo, path string) ([]DirectoryEntry, error) {
	if c.token == "" {
		return nil, fmt.Errorf("GitHub token required for Contents API")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s",
		owner, repo, strings.TrimPrefix(path, "/"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch directory contents for %s/%s/%s: %w", owner, repo, path, err)
	}
	defer func() {
		_, _ = io.CopyN(io.Discard, resp.Body, 1024) // best-effort drain
		_ = resp.Body.Close()                        // best-effort cleanup
	}()

	if resp.StatusCode == http.StatusNotFound {
		slog.Debug("directory not found", "owner", owner, "repo", repo, "path", path)
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub Contents API returned HTTP %d for %s/%s/%s", resp.StatusCode, owner, repo, path)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // cap 1MB
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var entries []DirectoryEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse directory listing for %s/%s/%s: %w", owner, repo, path, err)
	}

	return entries, nil
}

// FetchFileContent fetches raw file content via the GitHub REST Contents API.
// Returns nil (not an error) when the file does not exist (HTTP 404).
func (c *Client) FetchFileContent(ctx context.Context, owner, repo, path string) ([]byte, error) {
	if c.token == "" {
		return nil, fmt.Errorf("GitHub token required for Contents API")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s",
		owner, repo, strings.TrimPrefix(path, "/"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github.raw+json")

	resp, err := c.httpClient.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch file content for %s/%s/%s: %w", owner, repo, path, err)
	}
	defer func() {
		_, _ = io.CopyN(io.Discard, resp.Body, 1024) // best-effort drain
		_ = resp.Body.Close()                        // best-effort cleanup
	}()

	if resp.StatusCode == http.StatusNotFound {
		slog.Debug("file not found", "owner", owner, "repo", repo, "path", path)
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub Contents API returned HTTP %d for %s/%s/%s", resp.StatusCode, owner, repo, path)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // cap 2MB
	if err != nil {
		return nil, fmt.Errorf("failed to read file content: %w", err)
	}

	return body, nil
}
