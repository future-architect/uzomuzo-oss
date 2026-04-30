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

	"github.com/future-architect/uzomuzo-oss/internal/common"
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
			httpclient.RetryConfig{
				MaxRetries:        2,
				BaseBackoff:       500 * time.Millisecond,
				MaxBackoff:        3 * time.Second,
				RetryOn5xx:        true,
				RetryOnNetworkErr: true,
			},
		),
		cache: make(map[cacheKey]cacheEntry),
	}
}

// SetBaseURL overrides the API base URL (useful for tests or mirrors).
func (c *Client) SetBaseURL(u string) {
	c.baseURL = strings.TrimRight(strings.TrimSpace(u), "/")
}

// SetHTTPClient lets tests inject a custom *http.Client (typically pointing
// at an httptest.Server). The retry policy is reset to httpclient.DefaultRetryConfig
// (MaxRetries=3, 1s base backoff), which differs from NewClient's batch-tuned
// settings — this matches the codebase-wide SetHTTPClient convention used by
// all other infra clients.
func (c *Client) SetHTTPClient(h *http.Client) {
	c.http = httpclient.NewClient(h, httpclient.DefaultRetryConfig())
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
	// CD's path requires a namespace segment — packages without a
	// namespace (e.g. some npm/pypi entries) use "-" as a placeholder.
	if ns == "" {
		ns = "-"
	}

	key := cacheKey{cdType: mapping.cdType, provider: mapping.provider, namespace: ns, name: n, version: v}
	if cached, ok := c.lookupCache(key); ok {
		return cached.licenses, cached.found, nil
	}

	defURL, err := c.buildDefinitionURL(mapping.cdType, mapping.provider, ns, n, v)
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
		// Fallback path: declared is non-empty but ParseExpression yielded
		// no usable operands (e.g., bare punctuation). Treat the whole
		// string as a single non-standard entry so callers still see the
		// raw value rather than dropping it.
		return []domain.ResolvedLicense{{
			Source: domain.LicenseSourceClearlyDefinedNonStandard,
			Raw:    declared,
		}}, true
	}

	out := make([]domain.ResolvedLicense, 0, len(leaves))
	for _, leaf := range leaves {
		out = append(out, leafToResolved(leaf, declared))
	}
	return out, true
}

// leafToResolved converts a single ExprLicense leaf to a ResolvedLicense.
// fullDeclared is preserved on each entry so downstream provenance can
// reconstruct the parent CD response. SPDX classification is taken from the
// leaf's Normalization result — never from substring matching on the
// identifier text.
func leafToResolved(leaf *licenses.ExprLicense, fullDeclared string) domain.ResolvedLicense {
	if leaf.Normalization.SPDX {
		return domain.ResolvedLicense{
			Identifier: leaf.Identifier,
			Source:     domain.LicenseSourceClearlyDefinedSPDX,
			Raw:        fullDeclared,
			IsSPDX:     true,
		}
	}
	return domain.ResolvedLicense{
		Source: domain.LicenseSourceClearlyDefinedNonStandard,
		Raw:    fullDeclared,
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

// SupportsEcosystem reports whether ClearlyDefined has coverage for the
// given ecosystem name. Callers can use this to skip enqueueing jobs for
// ecosystems that would always return found=false without issuing HTTP.
func SupportsEcosystem(ecosystem string) bool {
	_, ok := providerByEcosystem[strings.ToLower(strings.TrimSpace(ecosystem))]
	return ok
}

// IsRateLimitError reports whether an error from FetchLicenses indicates the
// underlying HTTP layer hit a rate limit. Re-exposed here so the dispatcher
// can branch on event names without depending on internal/common directly.
func IsRateLimitError(err error) bool {
	return common.IsRateLimitError(err)
}
