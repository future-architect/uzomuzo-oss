// Standalone fetch helpers for external callers that need raw text / metadata
// without running the full evaluation pipeline.
// These are thin wrappers over internal infrastructure packages.
package uzomuzo

import (
	"context"

	"github.com/future-architect/uzomuzo/internal/common"
	gh "github.com/future-architect/uzomuzo/internal/infrastructure/github"
	"github.com/future-architect/uzomuzo/internal/infrastructure/pypi"
)

// FetchGitHubREADME fetches raw README text from a GitHub repository.
// Tries filenames in order: README.md, README.MD, README, README.txt, README.rst.
// defaultBranch must be the resolved branch name (e.g. from GraphQL or API).
// Returns (content, rawURL, error). On not-found returns ("", "", error).
func FetchGitHubREADME(ctx context.Context, owner, repo, defaultBranch string) (string, string, error) {
	return gh.FetchREADME(ctx, owner, repo, defaultBranch)
}

// PyPIProjectInfo holds minimal project metadata from PyPI.
type PyPIProjectInfo struct {
	Name        string
	Summary     string
	Description string
	Classifiers []string
}

// FetchPyPIProject fetches project metadata from the PyPI JSON API.
// Returns (info, found, error). found=false with nil error means 404 (project does not exist).
func FetchPyPIProject(ctx context.Context, name string) (*PyPIProjectInfo, bool, error) {
	c := pypi.NewClient()
	info, ok, err := c.GetProject(ctx, name)
	if err != nil || !ok || info == nil {
		return nil, ok, err
	}
	return &PyPIProjectInfo{
		Name:        info.Name,
		Summary:     info.Summary,
		Description: info.Description,
		Classifiers: info.Classifiers,
	}, true, nil
}

// IsGitHubURL reports whether the given URL points to a GitHub repository.
// Accepts various formats: https://, http://, git+https://, ssh://, git@, github:owner/repo.
func IsGitHubURL(rawURL string) bool {
	return common.IsValidGitHubURL(rawURL)
}

// ParseGitHubURL extracts owner and repo from a GitHub URL.
// Accepts formats like https://github.com/owner/repo, git@github.com:owner/repo, etc.
// Returns an error for non-GitHub URLs or malformed input.
func ParseGitHubURL(rawURL string) (owner, repo string, err error) {
	return common.ExtractGitHubOwnerRepo(rawURL)
}
