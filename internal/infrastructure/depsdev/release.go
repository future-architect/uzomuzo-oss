package depsdev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common/links"
	commonpurl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

// GetLatestReleasesForPURLs fetches latest release information for multiple PURLs
// Flow: GitHub URL
// Purpose: Used by the GitHub URL flow to resolve default/latest versions for base PURLs.
// Called from: integration.IntegrationService.AnalyzeFromGitHubURL
func (c *DepsDevClient) GetLatestReleasesForPURLs(ctx context.Context, purls []string) (map[string]*ReleaseInfo, error) {
	const maxWorkers = 10

	if len(purls) > 1 {
		slog.Debug("Starting PURL batch processing", "total", len(purls), "max_workers", maxWorkers)
	}

	// Process all PURLs concurrently; collectBounded bounds parallelism by maxWorkers
	// (no BatchSize chunking — the full list is fanned out under the worker cap).
	// GetLatestReleasesForPURLs stores error-bearing ReleaseInfo (ok=true with error
	// value) — divergent from fetchReleaseInfoBatch which drops errored items.
	type ptrReleaseInfo = *ReleaseInfo
	results := collectBounded[ptrReleaseInfo](ctx, purls, maxWorkers, func(ctx context.Context, p string) (string, *ReleaseInfo, bool) {
		releaseInfo, _ := c.fetchLatestRelease(ctx, p) // intentionally ignore error: details captured in releaseInfo.Error
		slog.Debug("Processing progress", "purl", p)
		return p, &releaseInfo, true // always store (even with error) — ok=true preserves error-bearing semantics
	})

	if len(purls) > 1 {
		slog.Debug("PURL processing completed", "processed", len(purls), "total", len(purls))
	}

	return results, nil
}

// fetchLatestRelease fetches the latest release information for the specified PURL
func (c *DepsDevClient) fetchLatestRelease(ctx context.Context, purlStr string) (ReleaseInfo, error) {
	parser := commonpurl.NewParser()
	parsed, err := parser.Parse(purlStr)
	if err != nil {
		return ReleaseInfo{
			Error: fmt.Errorf("failed to parse PURL: %w", err),
		}, err
	}

	// Map PURL ecosystem and package name to deps.dev expectations (breaking simplified API)
	system, name, err := toDepsDevSystemAndName(parsed)
	if err != nil {
		wrappedErr := fmt.Errorf("fetchLatestRelease: normalize PURL: %w", err)
		if errors.Is(err, links.ErrUnsupportedEcosystem) {
			slog.Debug("fetchLatestRelease: skipping unsupported ecosystem", "purl", purlStr, "error", err)
			return ReleaseInfo{Error: wrappedErr}, nil
		}
		return ReleaseInfo{Error: wrappedErr}, wrappedErr
	}
	origSystem, origName := system, name // capture before any normalization so we can log only when changed

	// Track normalized module name locally (avoid context misuse for intra-function data)
	normalizedRawName := parsed.PackageName()
	if strings.EqualFold(parsed.Ecosystem(), "golang") {
		// Use helper to encapsulate Go-specific normalization logic for versions endpoint.
		norm := normalizeGoModuleForVersions(ctx, c.goproxy, parsed)
		switch norm.Strategy {
		case "proxy", "fallback", "fallback-no-proxy":
			if norm.Changed {
				slog.Debug("deps.dev: go module name normalized", "strategy", norm.Strategy, "from", name, "to", norm.ModuleRootRaw)
			}
			name = norm.EscapedName
			normalizedRawName = norm.ModuleRootRaw
		}
	}
	// Maven PURL validation is done in fetchPackageInfo after normalization
	// to avoid duplicate warnings.

	// Log only when mapping changed (noise reduction for high-volume batches)
	if system != origSystem || name != origName {
		fields := []any{"purl", purlStr, "system", system, "name", name, "from_system", origSystem, "from_name", origName}
		if strings.EqualFold(parsed.Ecosystem(), "golang") && normalizedRawName != parsed.PackageName() {
			fields = append(fields, "normalized_raw", normalizedRawName)
		}
		slog.Debug("deps.dev versions endpoint mapping changed", fields...)
	}
	endpoint := fmt.Sprintf("%s/systems/%s/packages/%s", c.baseURL, system, name)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		slog.Debug("deps.dev request creation failed", "method", "GET", "url", endpoint, "error", err)
		return ReleaseInfo{
			Endpoint: endpoint,
			Error:    fmt.Errorf("failed to create request (url=%s): %w", endpoint, err),
		}, err
	}
	// Set descriptive User-Agent for deps.dev requests
	req.Header.Set("User-Agent", "uzomuzo-depsdev-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")

	resp, err := c.client.Do(ctx, req)
	if err != nil {
		slog.Debug("deps.dev HTTP request failed", "method", "GET", "url", endpoint, "error", err)
		return ReleaseInfo{
			Endpoint: endpoint,
			Error:    fmt.Errorf("HTTP request failed (url=%s): %w", endpoint, err),
		}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Treat 404 as benign: deps.dev may not index certain Go forks or packages; continue without versions.
		if resp.StatusCode == http.StatusNotFound {
			slog.Debug("deps.dev versions endpoint returned 404", "method", "GET", "url", endpoint)
			return ReleaseInfo{Endpoint: endpoint}, nil
		}
		// Other status codes remain errors.
		body, _ := io.ReadAll(resp.Body)
		snippet := truncateString(string(body), 1024)
		derr := fmt.Errorf("HTTP %d %s (url=%s): %s", resp.StatusCode, resp.Status, endpoint, snippet)
		slog.Debug("deps.dev versions endpoint non-OK response", "method", "GET", "url", endpoint, "status", resp.StatusCode, "body_snippet", snippet)
		return ReleaseInfo{Endpoint: endpoint, Error: derr}, derr
	}

	var result struct {
		Versions []struct {
			VersionKey struct {
				Version string `json:"version"`
			} `json:"versionKey"`
			PublishedAt  string `json:"publishedAt"`
			IsDefault    bool   `json:"isDefault"`
			IsDeprecated bool   `json:"isDeprecated"`
		} `json:"versions"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Debug("deps.dev versions endpoint JSON decode failed", "method", "GET", "url", endpoint, "error", err)
		return ReleaseInfo{
			Endpoint: endpoint,
			Error:    fmt.Errorf("JSON decode failed (url=%s): %w", endpoint, err),
		}, err
	}

	releaseInfo := ReleaseInfo{
		Endpoint: endpoint,
	}

	requestedVersion := parsed.Version()

	// Collect built versions for selection
	builtVersions := make([]Version, 0, len(result.Versions))

	// Process version information
	for _, version := range result.Versions {
		versionInfo := Version{
			VersionKey: VersionKey{
				System: parsed.Ecosystem(),
				// Prefer the normalized module name captured earlier for Go; fallback to original
				Name:    parsed.PackageName(),
				Version: version.VersionKey.Version,
			},
		}

		// If Go normalization provided a different module name, reconstruct the PURL accordingly
		if strings.EqualFold(parsed.Ecosystem(), "golang") {
			if newPURL, ok := reconstructGoVersionPURL(purlStr, normalizedRawName, version.VersionKey.Version); ok {
				versionInfo.PURL = newPURL
				versionInfo.VersionKey.Name = normalizedRawName
			} else if newPURL, err := commonpurl.WithVersion(purlStr, version.VersionKey.Version); err == nil {
				versionInfo.PURL = newPURL
			} else {
				slog.Debug("failed to update PURL version", "purl", purlStr, "to_version", version.VersionKey.Version, "error", err)
			}
		} else {
			// Non-Go: keep previous behavior (version-only update)
			if newPURL, err := commonpurl.WithVersion(purlStr, version.VersionKey.Version); err == nil {
				versionInfo.PURL = newPURL
			} else {
				slog.Debug("failed to update PURL version", "purl", purlStr, "to_version", version.VersionKey.Version, "error", err)
			}
		}

		if version.PublishedAt != "" {
			if publishedAt, err := time.Parse(time.RFC3339, version.PublishedAt); err == nil {
				versionInfo.PublishedAt = publishedAt
			}
		}

		// carry flags
		versionInfo.IsDefault = version.IsDefault
		versionInfo.IsDeprecated = version.IsDeprecated

		if version.VersionKey.Version == requestedVersion {
			releaseInfo.RequestedVersion = versionInfo
		}

		builtVersions = append(builtVersions, versionInfo)
	}

	// Determine Stable/Dev/Max using unified selection logic
	stable, dev, max := pickStableDevAndMax(builtVersions)
	releaseInfo.StableVersion = stable
	if dev.VersionKey.Version != "" {
		releaseInfo.PreReleaseVersion = dev
	}
	if max.VersionKey.Version != "" {
		releaseInfo.MaxSemverVersion = max
	}
	return releaseInfo, nil
}

// fetchReleaseInfoBatch fetches release information for multiple PURLs with internal parallelization.
// Errored items are dropped (ok=false) — divergent from GetLatestReleasesForPURLs which stores
// error-bearing ReleaseInfo.
func (c *DepsDevClient) fetchReleaseInfoBatch(ctx context.Context, purls []string) (map[string]ReleaseInfo, error) {
	const maxWorkers = 10
	totalPURLs := len(purls)

	results := collectBounded[ReleaseInfo](ctx, purls, maxWorkers, func(ctx context.Context, p string) (string, ReleaseInfo, bool) {
		releaseInfo, err := c.fetchLatestRelease(ctx, p)
		if err != nil {
			slog.Debug("Failed to fetch release information", "purl", p, "error", err)
			return "", ReleaseInfo{}, false // drop errored items
		}
		if totalPURLs >= 1000 {
			slog.Debug("Release info progress", "purl", p, "total", totalPURLs)
		}
		return p, releaseInfo, true
	})

	return results, nil
}
