package rdap

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// ianaBootstrapURL is the IANA RDAP bootstrap registry for DNS (RFC 9224).
const ianaBootstrapURL = "https://data.iana.org/rdap/dns.json"

// bootstrapCacheSchema versions the on-disk bootstrap cache layout.
const bootstrapCacheSchema = 1

// maxBootstrapLifetime caps how long a cached registry is trusted, whatever
// max-age or Expires says.
const maxBootstrapLifetime = 365 * 24 * time.Hour

//go:embed bootstrap.json
var bootstrapFS embed.FS

type bootstrap struct {
	Publication string       `json:"publication"`
	Services    [][][]string `json:"services"`
}

// bootstrapList is a parsed bootstrap registry: TLD to https RDAP base URL.
type bootstrapList struct {
	bases       map[string]string
	publication string
}

// bootstrapCacheEntry is the on-disk copy of the last fetched registry.
type bootstrapCacheEntry struct {
	Schema  int             `json:"schema"`
	ETag    string          `json:"etag,omitempty"`
	Expires time.Time       `json:"expires"`
	Body    json.RawMessage `json:"body"`
}

// embeddedBootstrap is the registry built into the binary. It panics if the
// embedded file cannot be decoded, which is a build defect.
var embeddedBootstrap = sync.OnceValue(func() bootstrapList {
	data, err := bootstrapFS.ReadFile("bootstrap.json")
	if err != nil {
		panic(fmt.Sprintf("rdap: read embedded bootstrap: %v", err))
	}
	list, err := parseBootstrap(data)
	if err != nil {
		panic(fmt.Sprintf("rdap: embedded bootstrap: %v", err))
	}
	return list
})

// DefaultBootstrapCachePath returns where scan keeps the fetched RDAP bootstrap
// registry, under the user cache directory. It returns "" when the platform
// has no user cache directory; the registry is then fetched every run.
func DefaultBootstrapCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "uzomuzo", "rdap", "dns.json")
}

// SetBootstrapCache sets the file that keeps the fetched bootstrap registry
// between runs. It is separate from SetCacheDir, which keeps domain records.
func (c *Client) SetBootstrapCache(path string) { c.bootstrapCache = path }

// SetBootstrapURL overrides the bootstrap registry URL.
func (c *Client) SetBootstrapURL(u string) { c.bootstrapURL = u }

// SetBootstrapClock replaces the clock used for bootstrap cache expiry.
func (c *Client) SetBootstrapClock(now func() time.Time) { c.now = now }

// RefreshBootstrap replaces the TLD service list with the current IANA
// registry, following its HTTP cache headers (RFC 9224 section 4). A cached
// copy is used without a request until it expires, then revalidated with
// If-None-Match when it has an ETag. Any failure resets the list to the
// embedded registry. The fetch is not retried. Call it before SetBaseURL and
// the first lookup: it replaces the whole list. See ADR-0030.
func (c *Client) RefreshBootstrap(ctx context.Context) analysis.RDAPBootstrapInfo {
	list, source, err := c.loadBootstrap(ctx)
	if err != nil {
		slog.Warn("rdap_bootstrap_fallback", "url", c.bootstrapURL, "error", err)
		list, source = embeddedBootstrap(), analysis.RDAPBootstrapEmbedded
	}
	c.mu.Lock()
	c.base = maps.Clone(list.bases)
	c.mu.Unlock()
	return analysis.RDAPBootstrapInfo{Source: source, Publication: list.publication}
}

func (c *Client) loadBootstrap(ctx context.Context) (bootstrapList, analysis.RDAPBootstrapSource, error) {
	cached, cacheErr := c.readBootstrapCache()
	var cachedList bootstrapList
	if cacheErr == nil {
		// An unusable cached body is not revalidated: a 304 would keep it.
		cachedList, cacheErr = parseBootstrap(cached.Body)
	}
	if cacheErr == nil && c.now().Before(cached.Expires) {
		return cachedList, analysis.RDAPBootstrapCache, nil
	}
	if cacheErr != nil && !errors.Is(cacheErr, os.ErrNotExist) {
		slog.Debug("rdap_bootstrap_cache_unusable", "path", c.bootstrapCache, "error", cacheErr)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.bootstrapURL, nil)
	if err != nil {
		return bootstrapList{}, "", fmt.Errorf("build bootstrap request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	revalidate := cacheErr == nil && cached.ETag != ""
	if revalidate {
		req.Header.Set("If-None-Match", cached.ETag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return bootstrapList{}, "", fmt.Errorf("fetch bootstrap: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // read-only response body

	switch {
	case resp.StatusCode == http.StatusNotModified && revalidate:
		cached.Expires = c.expiry(resp.Header)
		if etag := resp.Header.Get("ETag"); etag != "" {
			cached.ETag = etag
		}
		if !noStore(resp.Header) {
			c.writeBootstrapCache(cached)
		}
		return cachedList, analysis.RDAPBootstrapCache, nil
	case resp.StatusCode == http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, rdapResponseLimit+1))
		if err != nil {
			return bootstrapList{}, "", fmt.Errorf("read bootstrap: %w", err)
		}
		if len(body) > rdapResponseLimit {
			return bootstrapList{}, "", fmt.Errorf("bootstrap larger than %d bytes", rdapResponseLimit)
		}
		list, err := parseBootstrap(body)
		if err != nil {
			return bootstrapList{}, "", fmt.Errorf("fetched bootstrap: %w", err)
		}
		if !noStore(resp.Header) {
			c.writeBootstrapCache(bootstrapCacheEntry{ETag: resp.Header.Get("ETag"), Expires: c.expiry(resp.Header), Body: body})
		}
		return list, analysis.RDAPBootstrapLive, nil
	default:
		return bootstrapList{}, "", fmt.Errorf("fetch bootstrap: HTTP %d", resp.StatusCode)
	}
}

// expiry derives the refresh time from Cache-Control max-age less Age, then
// from Expires, both capped at maxBootstrapLifetime. A no-store or no-cache
// directive anywhere in Cache-Control, or neither header, means the copy is
// stale at once and the next run revalidates it.
func (c *Client) expiry(h http.Header) time.Time {
	now := c.now()
	directives := cacheDirectives(h)
	if _, ok := directives["no-store"]; ok {
		return now
	}
	if _, ok := directives["no-cache"]; ok {
		return now
	}
	if maxAge, ok := headerSeconds(directives["max-age"]); ok {
		age, _ := headerSeconds(h.Get("Age")) // a missing or malformed Age counts as 0
		remaining := min(max(maxAge-age, 0), int64(maxBootstrapLifetime/time.Second))
		return now.Add(time.Duration(remaining) * time.Second)
	}
	if at, err := http.ParseTime(h.Get("Expires")); err == nil {
		if limit := now.Add(maxBootstrapLifetime); at.After(limit) {
			return limit
		}
		return at
	}
	return now
}

// noStore reports whether the response must not be written to disk.
func noStore(h http.Header) bool {
	_, ok := cacheDirectives(h)["no-store"]
	return ok
}

// cacheDirectives collects Cache-Control directives from every header line,
// lowercased, with their values unquoted.
func cacheDirectives(h http.Header) map[string]string {
	out := map[string]string{}
	for _, directive := range strings.Split(strings.Join(h.Values("Cache-Control"), ","), ",") {
		name, value, _ := strings.Cut(strings.TrimSpace(directive), "=")
		if name != "" {
			out[strings.ToLower(name)] = strings.Trim(value, `"`)
		}
	}
	return out
}

// headerSeconds parses a non-negative delta-seconds value. A value too large
// for int64 saturates (RFC 9111 section 1.2.2), so later arithmetic cannot
// overflow once the caller caps it.
func headerSeconds(v string) (int64, bool) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(strings.TrimSpace(v), "-") {
		return 1 << 62, true
	}
	if err != nil || seconds < 0 {
		return 0, false
	}
	return min(seconds, 1<<62), true
}

func (c *Client) readBootstrapCache() (bootstrapCacheEntry, error) {
	if c.bootstrapCache == "" {
		return bootstrapCacheEntry{}, os.ErrNotExist
	}
	f, err := os.Open(c.bootstrapCache)
	if err != nil {
		return bootstrapCacheEntry{}, fmt.Errorf("read bootstrap cache: %w", err)
	}
	defer func() { _ = f.Close() }() // read-only
	// The body is at most rdapResponseLimit; the rest is a small envelope.
	data, err := io.ReadAll(io.LimitReader(f, 2*rdapResponseLimit))
	if err != nil {
		return bootstrapCacheEntry{}, fmt.Errorf("read bootstrap cache: %w", err)
	}
	var entry bootstrapCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return bootstrapCacheEntry{}, fmt.Errorf("decode bootstrap cache: %w", err)
	}
	if entry.Schema != bootstrapCacheSchema {
		return bootstrapCacheEntry{}, fmt.Errorf("bootstrap cache schema %d, want %d", entry.Schema, bootstrapCacheSchema)
	}
	return entry, nil
}

// writeBootstrapCache stores the registry on a best-effort basis: the list is
// already in use, so a failed write only costs a request next run. The file is
// written through a temporary file and renamed, so a concurrent scan never
// reads half of it.
func (c *Client) writeBootstrapCache(entry bootstrapCacheEntry) {
	if c.bootstrapCache == "" {
		return
	}
	entry.Schema = bootstrapCacheSchema
	entry.Expires = entry.Expires.UTC()
	if err := writeJSONAtomic(c.bootstrapCache, entry); err != nil {
		slog.Debug("rdap_bootstrap_cache_write_failed", "path", c.bootstrapCache, "error", err)
	}
}

// writeJSONAtomic writes v as JSON to path through a temporary file in the
// same directory and a rename.
func writeJSONAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name) // best-effort cleanup; the write error is returned
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// parseBootstrap keeps, for each TLD, the first https RDAP base URL listed.
// Plain-http services are left out (see ADR-0030). A registry without an
// RFC 3339 publication time or without any https service is rejected.
func parseBootstrap(data []byte) (bootstrapList, error) {
	var b bootstrap
	if err := json.Unmarshal(data, &b); err != nil {
		return bootstrapList{}, fmt.Errorf("decode bootstrap: %w", err)
	}
	if _, err := time.Parse(time.RFC3339, b.Publication); err != nil {
		return bootstrapList{}, fmt.Errorf("bootstrap publication %q: %w", b.Publication, err)
	}
	bases := make(map[string]string)
	for _, service := range b.Services {
		if len(service) < 2 {
			continue
		}
		var https string
		for _, u := range service[1] {
			if isHTTPSBase(u) {
				https = u
				break
			}
		}
		if https == "" {
			continue
		}
		for _, tld := range service[0] {
			bases[strings.ToLower(tld)] = https
		}
	}
	if len(bases) == 0 {
		return bootstrapList{}, errors.New("bootstrap has no https services")
	}
	return bootstrapList{bases: bases, publication: b.Publication}, nil
}

// isHTTPSBase reports whether u is an https URL with a host and no userinfo.
func isHTTPSBase(u string) bool {
	parsed, err := url.Parse(u)
	return err == nil && strings.EqualFold(parsed.Scheme, "https") && parsed.Host != "" && parsed.User == nil
}
