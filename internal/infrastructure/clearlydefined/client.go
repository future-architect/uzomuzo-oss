// Package clearlydefined integrates with the ClearlyDefined.io public API to
// supplement uzomuzo's license-resolution chain with curated, scancode-toolkit-
// backed license metadata.
//
// CD covers npm, maven, nuget, pypi, gem, cargo (and a few others), keyed by a
// stable URL shape:
//
//	GET https://api.clearlydefined.io/definitions/<type>/<provider>/<namespace>/<name>/<version>
//
// In the layered chain documented in ADR-0017 / ADR-0018, this package is the
// fourth and final tier — invoked only when deps.dev, GitHub `licenseInfo`,
// and the package's own ecosystem manifest (e.g. Maven POM) have all failed
// to yield a canonical SPDX identifier. The client never overwrites a prior
// canonical SPDX result; CD's value can only fill an empty or non-standard
// slot.
//
// DDD Layer: Infrastructure.
package clearlydefined

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/licenses"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

const (
	// defaultBaseURL is the public ClearlyDefined.io API endpoint.
	defaultBaseURL = "https://api.clearlydefined.io"

	// minDeclaredScore is the minimum `licensed.score.declared` required to
	// accept a CD response. CD scores >= 30 correspond to entries with at
	// least basic declared-license curation; lower scores indicate stale or
	// uncurated entries. User-confirmed default per umbrella tracker #327.
	minDeclaredScore = 30

	// userAgent identifies the client to CD's operators for debugging /
	// rate-limit attribution.
	userAgent = "uzomuzo-clearlydefined-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)"

	// positiveCacheTTL caps how long a successful (or below-threshold)
	// response stays cached. CD definitions are stable curation artifacts
	// — version-pinned coordinates do not change unless a curator edits
	// them — so a long TTL is safe for batch scans.
	positiveCacheTTL = 24 * time.Hour
	// negativeCacheTTL caps 404s. CD lazily curates new releases; a fresh
	// release may appear in a later run, but within a single batch we do
	// not want to retry the same 404 on every analysis sharing the
	// coordinate.
	negativeCacheTTL = 1 * time.Hour

	// maxJSONResponseSize caps the CD JSON API response body (1 MB).
	// CD definition responses are typically <50 KB; this guards against
	// malformed or abusive responses from the external service.
	maxJSONResponseSize = 1 << 20
)

// providerByEcosystem maps uzomuzo's normalized ecosystem name to CD's
// "<type>/<provider>" path components. Ecosystems not in this map have no
// CD coverage and FetchLicenses returns found=false without HTTP.
var providerByEcosystem = map[string]struct{ cdType, provider string }{
	"maven":    {cdType: "maven", provider: "mavencentral"},
	"npm":      {cdType: "npm", provider: "npmjs"},
	"nuget":    {cdType: "nuget", provider: "nuget"},
	"pypi":     {cdType: "pypi", provider: "pypi"},
	"gem":      {cdType: "gem", provider: "rubygems"},
	"cargo":    {cdType: "crate", provider: "cratesio"},
	"composer": {cdType: "composer", provider: "packagist"},
}

// SupportsEcosystem reports whether the given ecosystem name has CD coverage.
// Callers (typically the dispatcher in package integration) use it as an
// up-front gate so analyses for unsupported ecosystems do not reach the
// fan-out and waste a goroutine slot only to short-circuit inside FetchLicenses.
// The accepted set matches FetchLicenses' lookup; pass the same lower-cased
// ecosystem string both places.
func SupportsEcosystem(ecosystem string) bool {
	_, ok := providerByEcosystem[strings.ToLower(strings.TrimSpace(ecosystem))]
	return ok
}

// defaultRetryConfig returns the retry policy used by both NewClient and
// SetHTTPClient. Centralizing here ensures tests with injected transports
// exercise the same retry behavior as production, avoiding test/production
// drift where a mock'd transport would silently see a different MaxRetries
// or backoff curve.
func defaultRetryConfig() httpclient.RetryConfig {
	return httpclient.RetryConfig{
		MaxRetries:        2,
		BaseBackoff:       500 * time.Millisecond,
		MaxBackoff:        3 * time.Second,
		RetryOn5xx:        true,
		RetryOnNetworkErr: true,
	}
}

// Client fetches curated license information from ClearlyDefined.io.
//
// DDD Layer: Infrastructure
// Responsibility: External HTTP to ClearlyDefined.io to retrieve curated
// `licensed.declared` data and translate it into domain.ResolvedLicense values.
type Client struct {
	baseURL string
	http    *httpclient.Client

	cacheMu sync.RWMutex
	cache   map[cacheKey]cacheEntry
}

// cacheKey identifies a CD coordinate in the in-memory cache. namespace
// stores the *original* (untransformed) input — empty stays empty. The URL
// builder substitutes "-" for empty when constructing the request, but the
// cache key never sees that substitution, so a package whose namespace is
// genuinely "-" (rare but technically valid) does not collide with packages
// whose namespace is empty.
type cacheKey struct {
	cdType, provider, namespace, name, version string
}

type cacheEntry struct {
	licenses []domain.ResolvedLicense
	found    bool
	expires  time.Time
}

// NewClient returns a Client targeting the public ClearlyDefined.io API
// with retry-friendly defaults appropriate for batch enrichment.
func NewClient() *Client {
	return &Client{
		baseURL: defaultBaseURL,
		http: httpclient.NewClient(
			&http.Client{Timeout: 10 * time.Second},
			defaultRetryConfig(),
		),
		cache: make(map[cacheKey]cacheEntry),
	}
}

// SetBaseURL overrides the API base URL (useful for tests or mirrors).
//
// The base URL is treated as origin-only — its path component, if any, is
// overwritten when each definition URL is constructed. Callers wiring a
// mirror that sits behind a path prefix (e.g. https://internal/cd/v1) need
// a wrapper proxy or a future enhancement to JoinPath. Trailing slashes
// are tolerated and stripped.
func (c *Client) SetBaseURL(u string) {
	c.baseURL = strings.TrimRight(strings.TrimSpace(u), "/")
}

// SetHTTPClient lets tests inject a custom *http.Client (typically pointing
// at an httptest.Server). The retry policy stays at the package default
// (defaultRetryConfig), so test-injected transports exercise the same retry
// behavior as production.
func (c *Client) SetHTTPClient(h *http.Client) {
	c.http = httpclient.NewClient(h, defaultRetryConfig())
}

// FetchLicenses queries CD for the given coordinate and translates the
// response's `licensed.declared` field into domain.ResolvedLicense values.
// Returns:
//   - ([]ResolvedLicense, true, nil) when CD has at least one parseable
//     license entry that meets the score threshold;
//   - (nil, false, nil) when the ecosystem has no CD coverage, the
//     coordinate is missing inputs, the response is 404, the response is
//     200 but has no/empty `licensed.declared`, or the declared score is
//     below the threshold;
//   - (nil, false, err) on transport / decode / non-404 HTTP errors.
//
// The (nil, false, nil) and (nil, false, err) shapes mirror Maven's
// FetchLicenses for dispatcher symmetry.
func (c *Client) FetchLicenses(ctx context.Context, ecosystem, namespace, name, version string) ([]domain.ResolvedLicense, bool, error) {
	eco := strings.ToLower(strings.TrimSpace(ecosystem))
	mapping, ok := providerByEcosystem[eco]
	if !ok {
		return nil, false, nil
	}
	ns := strings.TrimSpace(namespace)
	n := strings.TrimSpace(name)
	v := strings.TrimSpace(version)
	if n == "" || v == "" {
		return nil, false, nil
	}

	// Cache key uses the original (untransformed) namespace — never the URL
	// builder's "-" placeholder — so empty and literal "-" stay distinct.
	key := cacheKey{cdType: mapping.cdType, provider: mapping.provider, namespace: ns, name: n, version: v}
	if cached, ok := c.lookupCache(key); ok {
		return cached.licenses, cached.found, nil
	}

	// CD's path requires a namespace segment — packages without a
	// namespace (e.g. some npm/pypi entries) use "-" as a placeholder.
	urlNS := ns
	if urlNS == "" {
		urlNS = "-"
	}
	defURL, err := c.buildDefinitionURL(mapping.cdType, mapping.provider, urlNS, n, v)
	if err != nil {
		return nil, false, fmt.Errorf("clearlydefined build url: %w", err)
	}
	def, status, err := c.fetchDefinition(ctx, defURL)
	if err != nil {
		return nil, false, err
	}
	if status == http.StatusNotFound || def == nil {
		c.storeCache(key, nil, false, negativeCacheTTL)
		return nil, false, nil
	}

	lics, found := translateDefinition(def)
	if !found {
		c.storeCache(key, nil, false, positiveCacheTTL)
		return nil, false, nil
	}
	c.storeCache(key, lics, true, positiveCacheTTL)
	return lics, true, nil
}

// buildDefinitionURL constructs the canonical CD definition URL. Each path
// segment is escaped via url.PathEscape so namespaces with special characters
// (e.g. npm @scope, "@types/node") survive intact in the wire format. The
// URL package treats Path as the unescaped form and RawPath as the encoded
// form; setting both keeps EscapedPath() honoring the explicit escaping
// rather than re-escaping a pre-encoded Path.
func (c *Client) buildDefinitionURL(cdType, provider, namespace, name, version string) (string, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base url: %w", err)
	}
	rawSegments := []string{"definitions", cdType, provider, namespace, name, version}
	escSegments := make([]string, len(rawSegments))
	for i, s := range rawSegments {
		escSegments[i] = url.PathEscape(s)
	}
	base.Path = "/" + strings.Join(rawSegments, "/")
	base.RawPath = "/" + strings.Join(escSegments, "/")
	return base.String(), nil
}

// fetchDefinition issues a single GET, drains the body, decodes the JSON.
// Returns (def, status, nil) on 200, (nil, 404, nil) on 404, and
// (nil, status, err) for other failures (transport, 5xx after retries,
// decode errors, unexpected status codes).
func (c *Client) fetchDefinition(ctx context.Context, defURL string) (*definitionResponse, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, defURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("clearlydefined build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, 0, fmt.Errorf("clearlydefined http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.CopyN(io.Discard, resp.Body, 1024) // best-effort drain before close
		return nil, http.StatusNotFound, nil
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.CopyN(io.Discard, resp.Body, 1024)
		return nil, resp.StatusCode, fmt.Errorf("clearlydefined http status %d", resp.StatusCode)
	}
	var def definitionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONResponseSize)).Decode(&def); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("clearlydefined decode: %w", err)
	}
	return &def, resp.StatusCode, nil
}

// definitionResponse models the subset of CD's /definitions response that
// drives license attribution.
type definitionResponse struct {
	Licensed struct {
		Declared string `json:"declared"`
		Score    struct {
			Total    int `json:"total"`
			Declared int `json:"declared"`
		} `json:"score"`
	} `json:"licensed"`
}

// translateDefinition converts CD's `licensed.declared` into a slice of
// domain.ResolvedLicense values. Returns (nil, false) when the entry has no
// declared license, fails the score threshold, or yields no usable operands.
//
// SPDX expressions in `declared` (Apache-2.0 OR MIT, etc.) are parsed via
// licenses.ParseExpression so each operand becomes its own ResolvedLicense.
// LicenseRef-* and other non-SPDX strings emit a single non-standard entry
// with the raw value preserved.
func translateDefinition(def *definitionResponse) ([]domain.ResolvedLicense, bool) {
	declared := strings.TrimSpace(def.Licensed.Declared)
	if declared == "" {
		return nil, false
	}
	if def.Licensed.Score.Declared < minDeclaredScore {
		slog.Debug("clearlydefined: score below threshold",
			"declared", declared,
			"score", def.Licensed.Score.Declared,
			"threshold", minDeclaredScore)
		return nil, false
	}

	parsed := licenses.ParseExpression(declared)
	leaves := parsed.Leaves()
	if len(leaves) == 0 {
		// Ignore declared values that contain no usable license operands.
		// Reachable for malformed/operator-only inputs such as "OR" or "()",
		// and for parser-rejected oversized expressions. These should not be
		// treated as resolved licenses or reported as found.
		slog.Debug("clearlydefined: declared expression produced no license operands",
			"declared", declared)
		return nil, false
	}

	out := make([]domain.ResolvedLicense, 0, len(leaves))
	for _, leaf := range leaves {
		out = append(out, leafToResolved(leaf))
	}
	return out, true
}

// leafToResolved converts a single ExprLicense leaf to a ResolvedLicense.
// Raw is the leaf's per-operand substring (e.g. "Apache-2.0" rather than
// the full "Apache-2.0 OR MIT"), matching PR #345's per-<license> Raw
// semantics so downstream consumers can attribute each ResolvedLicense to
// its specific operand. SPDX classification is taken from the leaf's
// Normalization result — never from substring matching on the identifier.
func leafToResolved(leaf *licenses.ExprLicense) domain.ResolvedLicense {
	if leaf.Normalization.SPDX {
		return domain.ResolvedLicense{
			Identifier: leaf.Identifier,
			Source:     domain.LicenseSourceClearlyDefinedSPDX,
			Raw:        leaf.Raw,
			IsSPDX:     true,
		}
	}
	return domain.ResolvedLicense{
		Source: domain.LicenseSourceClearlyDefinedNonStandard,
		Raw:    leaf.Raw,
	}
}

// lookupCache returns a cached entry when present and unexpired. It uses a
// read lock on the fast path and evicts expired entries under a write lock to
// keep the cache from retaining stale keys indefinitely.
func (c *Client) lookupCache(key cacheKey) (cacheEntry, bool) {
	now := time.Now()

	c.cacheMu.RLock()
	entry, ok := c.cache[key]
	c.cacheMu.RUnlock()
	if !ok {
		return cacheEntry{}, false
	}
	if now.After(entry.expires) {
		c.cacheMu.Lock()
		current, stillPresent := c.cache[key]
		if stillPresent && now.After(current.expires) {
			delete(c.cache, key)
		}
		c.cacheMu.Unlock()
		return cacheEntry{}, false
	}
	return entry, true
}

// storeCache writes a cache entry with the given TTL.
func (c *Client) storeCache(key cacheKey, lics []domain.ResolvedLicense, found bool, ttl time.Duration) {
	c.cacheMu.Lock()
	c.cache[key] = cacheEntry{
		licenses: lics,
		found:    found,
		expires:  time.Now().Add(ttl),
	}
	c.cacheMu.Unlock()
}

