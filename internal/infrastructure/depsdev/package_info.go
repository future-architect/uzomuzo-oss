package depsdev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	neturl "net/url"
	"strings"

	commonpurl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

// fetchPackageInfo fetches basic package information from PURL
func (c *DepsDevClient) fetchPackageInfo(ctx context.Context, purlStr string) (*PackageResponse, error) {
	// Pre-flight normalization + diagnostics for suspicious Maven PURLs
	original := purlStr
	normalizedApplied := false
	if pr, err := purlpkgToParsed(purlStr); err == nil && pr != nil && strings.EqualFold(pr.Ecosystem(), "maven") {
		ns := strings.TrimSpace(pr.Namespace())
		n := strings.TrimSpace(pr.Name())
		// Attempt normalization only if namespace empty (collapsed form)
		if ns == "" {
			if np := commonpurl.NormalizeMavenCollapsedCoordinates(purlStr); np != purlStr {
				slog.Debug("maven_purl_normalized", "original", purlStr, "normalized", np)
				purlStr = np
				normalizedApplied = true
				// Re-parse to update namespace/name for warning evaluation
				if pr2, err2 := purlpkgToParsed(purlStr); err2 == nil && pr2 != nil {
					ns = strings.TrimSpace(pr2.Namespace())
					n = strings.TrimSpace(pr2.Name())
				}
			}
		}
		// Re-check after normalization; suppress warning if now valid
		if ns == "" || strings.EqualFold(ns, n) || !strings.Contains(ns, ".") {
			slog.Debug("Suspicious Maven PURL - namespace (groupId) may be missing or incorrect",
				"purl", original,
				"effective", purlStr,
				"namespace", ns,
				"name", n,
				"normalized_applied", normalizedApplied,
				"hint", "Maven PURLs must be pkg:maven/<groupId>/<artifactId>@<version> (e.g., pkg:maven/org.javapos/javapos-contracts@1.14.3)")
		}
	}
	result, err := c.fetchPURLRaw(ctx, purlStr)
	if err != nil {
		// Maven Central Search fallback: when deps.dev returns 404 for a Maven
		// PURL with a missing or suspicious namespace, try to resolve the correct
		// groupId via Maven Central Search API and retry.
		if c.maven != nil && errors.Is(err, errPURLNotFound) {
			if corrected := c.tryMavenSearchFallback(ctx, original); corrected != nil {
				return corrected, nil
			}
		}
		return nil, err
	}
	return result, nil
}

// fetchPURLRaw performs a single HTTP GET against the deps.dev PURL endpoint
// and returns the decoded PackageResponse. No fallback logic is applied here.
func (c *DepsDevClient) fetchPURLRaw(ctx context.Context, purlStr string) (*PackageResponse, error) {
	decoded := purlStr
	if unescaped, err := neturl.PathUnescape(purlStr); err == nil {
		decoded = unescaped
	}
	encodedPURL := neturl.PathEscape(decoded)
	apiURL := c.baseURL + "/purl/" + encodedPURL

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request (url=%s): %w", apiURL, err)
	}
	req.Header.Set("User-Agent", "uzomuzo-depsdev-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")

	resp, err := c.client.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("request failed (url=%s): %w", apiURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w (url=%s)", errPURLNotFound, apiURL)
		}
		body, _ := io.ReadAll(resp.Body)
		snippet := truncateString(string(body), 1024)
		slog.Debug("deps.dev PURL endpoint non-OK response", "method", "GET", "url", apiURL, "status", resp.StatusCode, "body_snippet", snippet)
		return nil, fmt.Errorf("API returned status %d (url=%s): %s", resp.StatusCode, apiURL, snippet)
	}

	var packageResp PackageResponse
	if err := json.NewDecoder(resp.Body).Decode(&packageResp); err != nil {
		slog.Debug("deps.dev PURL endpoint JSON decode failed", "method", "GET", "url", apiURL, "error", err)
		return nil, fmt.Errorf("failed to decode response (url=%s): %w", apiURL, err)
	}

	return &packageResp, nil
}

// tryMavenSearchFallback attempts to resolve a Maven PURL with missing or suspicious
// namespace by querying the Maven Central Search API. If a unique groupId is found,
// it reconstructs the PURL and retries the deps.dev lookup.
//
// Triggers:
//   - namespace is empty (e.g., pkg:maven/jsr250-api)
//   - namespace equals name (e.g., pkg:maven/spring-aop/spring-aop)
//
// Returns the PackageResponse from the retry, or nil if fallback is not applicable
// or the retry also fails.
func (c *DepsDevClient) tryMavenSearchFallback(ctx context.Context, purlStr string) *PackageResponse {
	pr, err := purlpkgToParsed(purlStr)
	if err != nil || pr == nil {
		return nil
	}
	if !strings.EqualFold(pr.Ecosystem(), "maven") {
		return nil
	}

	ns := strings.TrimSpace(pr.Namespace())
	name := strings.TrimSpace(pr.Name())
	if name == "" {
		return nil
	}
	// Only trigger for missing namespace or namespace == name
	if ns != "" && !strings.EqualFold(ns, name) {
		return nil
	}

	// Strip trailing version-like suffix from artifact name
	// (e.g., "opentelemetry-sdk-extension-autoconfigure-1.28.0" → "opentelemetry-sdk-extension-autoconfigure")
	searchName := stripTrailingVersion(name)

	groupID, found, searchErr := c.maven.SearchByArtifactID(ctx, searchName)
	if searchErr != nil {
		slog.Debug("maven search fallback failed", "purl", purlStr, "error", searchErr)
		return nil
	}
	if !found {
		return nil
	}

	// Reconstruct PURL with resolved groupId
	version := strings.TrimSpace(pr.Version())
	correctedPURL := "pkg:maven/" + groupID + "/" + searchName
	if version != "" {
		correctedPURL += "@" + version
	}

	slog.Debug("maven_search_fallback_retry",
		"original", purlStr,
		"corrected", correctedPURL,
	)

	// Retry deps.dev lookup with corrected PURL via shared HTTP helper
	result, err := c.fetchPURLRaw(ctx, correctedPURL)
	if err != nil {
		slog.Debug("maven search fallback: retry also failed",
			"corrected_purl", correctedPURL,
			"error", err,
		)
		return nil
	}

	slog.Debug("maven_search_fallback_resolved",
		"original", purlStr,
		"corrected", correctedPURL,
	)
	return result
}

// stripTrailingVersion removes a trailing version-like suffix from an artifact name.
// e.g., "opentelemetry-sdk-extension-autoconfigure-1.28.0" → "opentelemetry-sdk-extension-autoconfigure"
// Only strips if the trailing segment consists entirely of digits and dots (e.g., "1.28.0", "6.4").
// Pre-release suffixes like "-SNAPSHOT", "-M1", "-rc1" are NOT stripped; this is intentional
// to avoid false positives on artifact names ending with alphabetic segments.
func stripTrailingVersion(name string) string {
	idx := strings.LastIndex(name, "-")
	if idx < 0 || idx == len(name)-1 {
		return name
	}
	suffix := name[idx+1:]
	// Check if suffix looks like a version: all chars are digits or dots, starts with digit
	if len(suffix) == 0 || suffix[0] < '0' || suffix[0] > '9' {
		return name
	}
	for _, ch := range suffix {
		if (ch < '0' || ch > '9') && ch != '.' {
			return name
		}
	}
	return name[:idx]
}
