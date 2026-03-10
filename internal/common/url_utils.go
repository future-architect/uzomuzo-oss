package common

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeRepositoryURL normalizes repository URLs from various formats to HTTPS.
// Handles cases like:
// - git+https://github.com/owner/repo.git -> https://github.com/owner/repo
// - git://github.com/owner/repo.git -> https://github.com/owner/repo
// - git+ssh://git@github.com/owner/repo -> https://github.com/owner/repo
// - ssh://git@github.com/owner/repo -> https://github.com/owner/repo
// - git@github.com:owner/repo -> https://github.com/owner/repo
// - https://github.com/owner/repo.git -> https://github.com/owner/repo
// - https://github.com/owner/repo/ -> https://github.com/owner/repo
func NormalizeRepositoryURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	// Handle npm-style shorthand: github:owner/repo or github:owner/repo#ref
	if strings.HasPrefix(rawURL, "github:") {
		s := strings.TrimPrefix(rawURL, "github:")
		s = strings.TrimLeft(s, "/")
		if !strings.HasPrefix(strings.ToLower(s), "github.com/") {
			s = "github.com/" + s
		}
		rawURL = "https://" + s
	}

	// Handle git+https:// prefix
	if strings.HasPrefix(rawURL, "git+https://") {
		rawURL = strings.TrimPrefix(rawURL, "git+")
	}

	// Handle git+ssh:// and ssh:// schemes by converting to https:// and stripping user@ if present
	if strings.HasPrefix(rawURL, "git+ssh://") || strings.HasPrefix(rawURL, "ssh://") {
		s := rawURL
		if strings.HasPrefix(s, "git+ssh://") {
			s = strings.TrimPrefix(s, "git+ssh://")
		} else {
			s = strings.TrimPrefix(s, "ssh://")
		}
		// drop leading git@
		s = strings.TrimPrefix(s, "git@")
		// Normalize host:path to host/path if a colon appears before the first slash
		if colon := strings.Index(s, ":"); colon >= 0 {
			if slash := strings.Index(s, "/"); slash == -1 || colon < slash {
				s = strings.Replace(s, ":", "/", 1)
			}
		}
		rawURL = "https://" + s
	}

	// Handle scp-like form: git@host:owner/repo
	if strings.HasPrefix(rawURL, "git@") {
		s := strings.TrimPrefix(rawURL, "git@")
		if colon := strings.Index(s, ":"); colon >= 0 {
			if slash := strings.Index(s, "/"); slash == -1 || colon < slash {
				s = strings.Replace(s, ":", "/", 1)
				rawURL = "https://" + s
			}
		}
	}

	// Handle git:// prefix - convert to https://
	if strings.HasPrefix(rawURL, "git://") {
		rawURL = strings.Replace(rawURL, "git://", "https://", 1)
	}

	// Remove URL fragment and query if present
	if idx := strings.Index(rawURL, "#"); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	if idx := strings.Index(rawURL, "?"); idx >= 0 {
		rawURL = rawURL[:idx]
	}

	// Remove any trailing slashes (including multiple)
	rawURL = strings.TrimRight(rawURL, "/")

	// Remove .git suffix if present (after trimming trailing slash to handle ".git/")
	rawURL = strings.TrimSuffix(rawURL, ".git")

	return rawURL
}

// IsValidGitHubURL validates if a URL is a valid GitHub repository URL
// Supports multiple formats:
// - https://github.com/owner/repo
// - http://github.com/owner/repo
// - github.com/owner/repo
// - owner/repo (if it looks like a valid GitHub path)
func IsValidGitHubURL(urlStr string) bool {
	if urlStr == "" {
		return false
	}

	// Normalize the URL string for validation
	normalizedURL := strings.TrimSpace(urlStr)

	// Try to parse as full URL first
	if strings.HasPrefix(normalizedURL, "http://") || strings.HasPrefix(normalizedURL, "https://") {
		parsed, err := url.Parse(normalizedURL)
		if err != nil {
			return false
		}

		// Must be GitHub domain
		if !strings.Contains(strings.ToLower(parsed.Host), "github.com") {
			return false
		}

		// Must have owner/repo path
		pathParts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		return len(pathParts) >= 2 && pathParts[0] != "" && pathParts[1] != ""
	}

	// Handle github.com/owner/repo format
	if strings.HasPrefix(normalizedURL, "github.com/") {
		pathPart := strings.TrimPrefix(normalizedURL, "github.com/")
		pathParts := strings.Split(pathPart, "/")
		return len(pathParts) >= 2 && pathParts[0] != "" && pathParts[1] != ""
	}

	// Handle owner/repo format (assume it's GitHub if it has exactly 2 parts)
	pathParts := strings.Split(normalizedURL, "/")
	if len(pathParts) == 2 && pathParts[0] != "" && pathParts[1] != "" {
		// Additional validation: check if it doesn't look like a PURL
		if strings.HasPrefix(normalizedURL, "pkg:") {
			return false
		}
		// Additional validation: check if it doesn't contain invalid characters for GitHub usernames/repos
		for _, part := range pathParts {
			if strings.Contains(part, "@") || strings.Contains(part, ":") {
				return false
			}
		}
		return true
	}

	return false
}

// ExtractGitHubOwnerRepo extracts {owner, repo} from a GitHub repository reference.
//
// It accepts common real-world variants and trims noise so downstream consumers
// (GraphQL queries, PURL generation) can rely on a stable owner/repo pair.
//
// Accepted examples:
//   - https://github.com/owner/repo
//   - http://github.com/owner/repo
//   - github.com/owner/repo
//   - owner/repo
//   - git@github.com:owner/repo(.git)?
//   - git+ssh://git@github.com/owner/repo
//   - ssh://git@github.com/owner/repo
//
// Notes:
//   - It first normalizes with NormalizeRepositoryURL, then extracts the first two
//     non-empty segments as owner/repo and strips a trailing ".git".
//   - Returns an error if the input cannot be interpreted as a GitHub repo.
func ExtractGitHubOwnerRepo(raw string) (string, string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", "", fmt.Errorf("empty GitHub URL")
	}

	// Normalize (handles ssh/scp/git+ prefixes, .git suffix, query/fragment, slashes)
	norm := NormalizeRepositoryURL(raw)
	if norm == "" {
		return "", "", fmt.Errorf("invalid GitHub URL: %s", raw)
	}

	// Reject obvious non-URL owner/repo forms like PURL
	if strings.HasPrefix(strings.ToLower(norm), "pkg:") {
		return "", "", fmt.Errorf("unsupported input (PURL not allowed): %s", raw)
	}

	// Handle three general shapes after normalization:
	//  a) Full URL: http(s)://<host>/owner/repo[/...]
	//  b) Host path: github.com/owner/repo[/...]
	//  c) Owner/repo
	var host string
	var path string

	if strings.HasPrefix(norm, "http://") || strings.HasPrefix(norm, "https://") {
		u, err := url.Parse(norm)
		if err != nil || u.Host == "" {
			return "", "", fmt.Errorf("invalid GitHub URL: %s", raw)
		}
		host = strings.ToLower(u.Host)
		path = strings.Trim(u.Path, "/")
	} else if strings.HasPrefix(norm, "github.com/") {
		host = "github.com"
		path = strings.TrimPrefix(norm, "github.com/")
		path = strings.Trim(path, "/")
	} else {
		// Treat as owner/repo
		host = "github.com"
		path = strings.Trim(norm, "/")
	}

	if !strings.Contains(host, "github.com") {
		return "", "", fmt.Errorf("non-github host: %s", host)
	}

	parts := []string{}
	if path != "" {
		for _, p := range strings.Split(path, "/") {
			if p != "" {
				parts = append(parts, p)
			}
		}
	}
	if len(parts) < 2 {
		return "", "", fmt.Errorf("missing owner/repo: %s", raw)
	}

	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("invalid owner/repo: %s", raw)
	}
	// Disallow characters that cannot appear in GitHub owner/repo identifiers
	if strings.Contains(owner, "@") || strings.Contains(owner, ":") || strings.Contains(repo, "@") || strings.Contains(repo, ":") {
		return "", "", fmt.Errorf("invalid characters in owner/repo: %s", raw)
	}
	return owner, repo, nil
}

// MapApacheHostedToGitHub maps Apache-hosted GitBox/legacy git-wip-us URLs to the canonical GitHub repository URL.
//
// Supported patterns (best-effort):
//   - https://gitbox.apache.org/repos/asf?p=<repo>.git;...
//   - https://gitbox.apache.org/repos/asf/<repo>.git
//   - https://git-wip-us.apache.org/repos/asf/<repo>.git
//
// Returns: https://github.com/apache/<repo> when derivable, otherwise "".
func MapApacheHostedToGitHub(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	if !strings.Contains(lower, "gitbox.apache.org") && !strings.Contains(lower, "git-wip-us.apache.org") {
		return ""
	}

	// Try URL parsing first
	if u, err := url.Parse(s); err == nil && u != nil {
		// 1) cgit-style: ?p=<repo>.git[;a=summary...]
		if q := u.Query().Get("p"); q != "" {
			name := strings.TrimSuffix(strings.TrimSpace(q), ".git")
			name = strings.Trim(name, "/")
			if name != "" {
				return "https://github.com/apache/" + name
			}
		}
		// 2) path-style: /repos/asf/<repo>.git (or trailing slash variations)
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		for i := len(parts) - 1; i >= 0; i-- {
			seg := strings.TrimSpace(parts[i])
			if seg == "" || seg == "repos" || seg == "asf" {
				continue
			}
			name := strings.TrimSuffix(seg, ".git")
			name = strings.Trim(name, "/")
			if name != "" {
				return "https://github.com/apache/" + name
			}
			break
		}
	}

	// 3) Fallback: best-effort extraction from raw string using "p=<repo>.git"
	if idx := strings.Index(lower, "p="); idx >= 0 {
		rest := s[idx+2:]
		cut := len(rest)
		for _, delim := range []string{"&", ";", " ", "\t", "\n"} {
			if j := strings.Index(rest, delim); j >= 0 && j < cut {
				cut = j
			}
		}
		val := strings.TrimSpace(rest[:cut])
		val = strings.TrimSuffix(val, ".git")
		val = strings.Trim(val, "/")
		if val != "" {
			return "https://github.com/apache/" + val
		}
	}
	return ""
}
