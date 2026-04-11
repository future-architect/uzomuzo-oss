package pypi

// Wheel-based import name resolution for PyPI packages.
//
// DDD Layer: Infrastructure
// Responsibility: Download the smallest wheel for a PyPI package and extract
// the actual Python import module names from its metadata (top_level.txt,
// RECORD, or directory listing). This is a last-resort fallback used when
// heuristic name guessing produces zero coupling matches.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// maxWheelSize is the maximum wheel file size we are willing to download (5 MB).
// Wheels larger than this are skipped to avoid excessive network and memory usage.
const maxWheelSize = 5 << 20

// maxEntrySize caps the decompressed size of a single ZIP entry (1 MB).
// This prevents zip-bomb attacks where a tiny compressed entry expands to
// gigabytes of data.
const maxEntrySize = 1 << 20

// importNameCacheEntry stores resolved import names with a timestamp for TTL.
type importNameCacheEntry struct {
	names []string
	ts    time.Time
}

// importNameCache is the mutex and map for import name caching on Client.
// These fields are added to Client via composition below.

// initImportCache lazily initialises the import name cache.
func (c *Client) initImportCache() {
	c.importOnce.Do(func() {
		c.importCache = make(map[string]importNameCacheEntry)
	})
}

// getImportCached returns cached import names if the TTL has not expired.
func (c *Client) getImportCached(name string) ([]string, bool) {
	if c.ttl <= 0 {
		return nil, false
	}
	c.initImportCache()
	c.importMu.RLock()
	ent, ok := c.importCache[name]
	c.importMu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Since(ent.ts) > c.ttl {
		return nil, false
	}
	return ent.names, true
}

// setImportCache stores resolved import names in the cache.
func (c *Client) setImportCache(name string, names []string) {
	if c.ttl <= 0 {
		return
	}
	c.initImportCache()
	c.importMu.Lock()
	c.importCache[name] = importNameCacheEntry{names: names, ts: time.Now()}
	c.importMu.Unlock()
}

// wheelURL describes a single distribution file from the PyPI JSON API urls array.
type wheelURL struct {
	Filename    string `json:"filename"`
	PackageType string `json:"packagetype"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`
}

// ResolveImportNames fetches the actual Python import module names for a PyPI
// package by downloading and inspecting the smallest wheel file. Returns nil
// if resolution fails (graceful degradation — the caller should fall back to
// heuristic names).
func (c *Client) ResolveImportNames(ctx context.Context, packageName string) ([]string, error) {
	n := strings.TrimSpace(packageName)
	if n == "" {
		return nil, nil
	}
	lower := strings.ToLower(n)

	// Check cache first.
	if names, ok := c.getImportCached(lower); ok {
		slog.Debug("pypi_wheel: import name cache hit", "package", lower)
		return names, nil
	}

	// Fetch the wheel URL.
	wURL, err := c.selectSmallestWheel(ctx, n)
	if err != nil {
		return nil, fmt.Errorf("pypi wheel URL lookup failed for %s: %w", n, err)
	}
	if wURL == "" {
		slog.Debug("pypi_wheel: no suitable wheel found", "package", n)
		c.setImportCache(lower, nil) // cache negative result
		return nil, nil
	}

	// Download the wheel.
	wheelData, err := c.downloadWheel(ctx, wURL)
	if err != nil {
		return nil, fmt.Errorf("pypi wheel download failed for %s: %w", n, err)
	}

	// Extract import names from the ZIP.
	names := extractImportNamesFromWheel(wheelData)
	slog.Debug("pypi_wheel: resolved import names", "package", n, "names", names)

	c.setImportCache(lower, names)
	return names, nil
}

// selectSmallestWheel fetches the PyPI JSON API and returns the URL of the
// smallest bdist_wheel distribution that fits within maxWheelSize.
func (c *Client) selectSmallestWheel(ctx context.Context, packageName string) (string, error) {
	apiURL := fmt.Sprintf("%s/pypi/%s/json", c.resolvedBaseURL(), url.PathEscape(packageName))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", pypiUserAgent)

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return "", fmt.Errorf("http failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		slog.Debug("pypi_wheel: package not found on PyPI", "package", packageName)
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http status %d", resp.StatusCode)
	}

	var raw struct {
		URLs []wheelURL `json:"urls"`
	}
	// Cap the JSON response to 10 MB to prevent memory exhaustion.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&raw); err != nil {
		return "", fmt.Errorf("decode failed: %w", err)
	}

	// Filter to wheels only, respecting size limit.
	var candidates []wheelURL
	for _, u := range raw.URLs {
		if u.PackageType != "bdist_wheel" {
			continue
		}
		if u.Size <= 0 || u.Size > maxWheelSize {
			continue
		}
		if u.URL == "" {
			continue
		}
		candidates = append(candidates, u)
	}
	if len(candidates) == 0 {
		return "", nil
	}

	// Pick the smallest.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Size < candidates[j].Size
	})
	return candidates[0].URL, nil
}

// downloadWheel fetches the wheel file content, capped at maxWheelSize.
func (c *Client) downloadWheel(ctx context.Context, dlURL string) ([]byte, error) {
	parsed, err := url.Parse(dlURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, fmt.Errorf("invalid wheel URL scheme: %s", dlURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", pypiUserAgent)

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("http failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	// Read with a safety cap to prevent memory exhaustion.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxWheelSize+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(data)) > maxWheelSize {
		return nil, fmt.Errorf("wheel exceeds %d bytes", maxWheelSize)
	}
	return data, nil
}

// extractImportNamesFromWheel parses a wheel ZIP and extracts Python import
// module names. Resolution priority:
//  1. top_level.txt in .dist-info
//  2. RECORD in .dist-info
//  3. Directory listing (__init__.py at top level)
func extractImportNamesFromWheel(data []byte) []string {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		slog.Debug("pypi_wheel: invalid zip", "error", err)
		return nil
	}

	// 1. Try top_level.txt
	if names := parseTopLevelTxt(r); len(names) > 0 {
		return names
	}

	// 2. Try RECORD
	if names := parseRECORD(r); len(names) > 0 {
		return names
	}

	// 3. Fallback: directories containing __init__.py at depth 1
	return parseInitPyDirs(r)
}

// parseTopLevelTxt looks for a top_level.txt file inside any .dist-info
// directory and extracts one module name per line.
func parseTopLevelTxt(r *zip.Reader) []string {
	for _, f := range r.File {
		dir, base := path.Split(f.Name)
		if base != "top_level.txt" {
			continue
		}
		if !strings.HasSuffix(dir, ".dist-info/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxEntrySize))
		_ = rc.Close()
		if err != nil {
			continue
		}
		var names []string
		seen := make(map[string]struct{})
		for _, line := range strings.Split(string(data), "\n") {
			name := strings.TrimSpace(line)
			if name == "" {
				continue
			}
			if !isPyIdentifierSafe(name) {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
		return names
	}
	return nil
}

// parseRECORD extracts top-level package directories from a RECORD file
// inside .dist-info. Each line is "path,hash,size". We look for entries
// whose first path component contains __init__.py at depth 1 only.
func parseRECORD(r *zip.Reader) []string {
	for _, f := range r.File {
		dir, base := path.Split(f.Name)
		if base != "RECORD" {
			continue
		}
		if !strings.HasSuffix(dir, ".dist-info/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxEntrySize))
		_ = rc.Close()
		if err != nil {
			continue
		}

		pkgDirs := make(map[string]struct{})
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// RECORD format: path,hash,size — extract the path field only.
			fields := strings.SplitN(line, ",", 2)
			recPath := fields[0]

			parts := strings.SplitN(recPath, "/", 2)
			if len(parts) < 2 {
				continue
			}
			topDir := parts[0]
			rest := parts[1]

			// Skip dist-info/data directories
			if strings.HasSuffix(topDir, ".dist-info") || strings.HasSuffix(topDir, ".data") {
				continue
			}
			// Only include top-level directories that have __init__.py (depth 1).
			if rest == "__init__.py" {
				if isPyIdentifierSafe(topDir) {
					pkgDirs[topDir] = struct{}{}
				}
			}
		}

		if len(pkgDirs) == 0 {
			return nil
		}
		names := make([]string, 0, len(pkgDirs))
		for d := range pkgDirs {
			names = append(names, d)
		}
		sort.Strings(names) // deterministic output
		return names
	}
	return nil
}

// parseInitPyDirs scans the ZIP for directories at depth 1 that contain
// __init__.py (the classic Python package indicator).
func parseInitPyDirs(r *zip.Reader) []string {
	dirs := make(map[string]struct{})
	for _, f := range r.File {
		parts := strings.SplitN(f.Name, "/", 3)
		if len(parts) < 2 {
			continue
		}
		topDir := parts[0]
		rest := parts[1]

		// Skip metadata directories
		if strings.HasSuffix(topDir, ".dist-info") || strings.HasSuffix(topDir, ".data") {
			continue
		}
		if strings.HasPrefix(topDir, "_") {
			continue
		}
		if rest == "__init__.py" {
			if isPyIdentifierSafe(topDir) {
				dirs[topDir] = struct{}{}
			}
		}
	}
	if len(dirs) == 0 {
		return nil
	}
	names := make([]string, 0, len(dirs))
	for d := range dirs {
		names = append(names, d)
	}
	sort.Strings(names)
	return names
}

// isPyIdentifierSafe reports whether s is a valid Python identifier segment.
// Duplicated from application/diet/service.go to avoid cross-layer import.
func isPyIdentifierSafe(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// importCacheFields groups the fields added to Client for import name caching.
// They are initialised lazily via initImportCache.
type importCacheFields struct {
	importOnce  sync.Once
	importMu    sync.RWMutex
	importCache map[string]importNameCacheEntry
}
