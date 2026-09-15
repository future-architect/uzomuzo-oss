// Package osv provides a minimal OSV.dev query client for the one question the
// analysis pipeline asks it: which advisories exist for a package, in a shape
// the domain can classify.
//
// The client decodes and sanitises; it decides nothing. Which curator is
// trusted, which marker counts and whether an advisory covers the whole package
// are business rules and live in analysis.ClassifyUnmaintained. See ADR-0025.
//
// DDD Layer: Infrastructure
// Responsibility: External HTTP calls to https://api.osv.dev/v1/query,
// mapping the OSV wire format onto analysis.AdvisoryRecord.
package osv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/future-architect/uzomuzo-oss/internal/common/ttlcache"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// osvUserAgent identifies uzomuzo to OSV.dev.
const osvUserAgent = "uzomuzo-osv-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)"

// maxJSONResponseSize caps one OSV query response page (4 MB). A widely-used
// package's page of advisories is typically well under 1 MB.
const maxJSONResponseSize = 4 << 20

// maxPages bounds the next_page_token walk. Exhausting it is an error, never an
// empty answer: a package whose advisories did not fit is unknown, not clean.
const maxPages = 10

// advisoryReferenceType is the OSV reference type naming the advisory's own
// page. RustSec records it as the rustsec.org advisory URL; the type is read
// rather than the host pattern assumed, so a different curator still resolves.
const advisoryReferenceType = "ADVISORY"

// Client queries OSV.dev for a package's advisories.
type Client struct {
	http    *httpclient.Client
	baseURL string
	cache   ttlcache.Cache[[]domain.AdvisoryRecord]
}

// NewClient returns an OSV.dev client with sensible HTTP defaults.
func NewClient() *Client {
	hc := &http.Client{Timeout: 5 * time.Second}
	c := &Client{
		http:    httpclient.NewClient(hc, httpclient.RegistryRetryConfig()),
		baseURL: "https://api.osv.dev",
	}
	c.cache.SetTTL(10 * time.Minute)
	return c
}

// SetHTTPClient overrides the underlying http.Client (tests).
func (c *Client) SetHTTPClient(h *http.Client) {
	if h == nil {
		return
	}
	c.http = httpclient.NewClient(h, httpclient.RegistryRetryConfig())
}

// SetBaseURL overrides the base host (tests).
func (c *Client) SetBaseURL(u string) { c.baseURL = strings.TrimRight(u, "/") }

// SetCacheTTL sets the in-memory cache TTL (<=0 disables caching).
func (c *Client) SetCacheTTL(d time.Duration) { c.cache.SetTTL(d) }

// QueryPackage returns every advisory OSV.dev holds for one package, reduced to
// the fields the domain classifies.
//
// An error means unknown, never "no advisories": callers must not read a failed
// lookup as a clean package. A successful lookup that found nothing returns an
// empty slice and a nil error, and is cached as such — most packages have no
// advisories, and re-asking for each of them would dominate a batch.
func (c *Client) QueryPackage(ctx context.Context, ecosystem, name string) ([]domain.AdvisoryRecord, error) {
	eco := strings.TrimSpace(ecosystem)
	n := strings.TrimSpace(name)
	if eco == "" || n == "" {
		return nil, nil
	}
	key := strings.ToLower(eco) + "/" + strings.ToLower(n)
	if recs, ok := c.cache.Get(key); ok {
		slog.Debug("osv_cache_hit", "ecosystem", eco, "name", n)
		return recs, nil
	}

	var out []domain.AdvisoryRecord
	seenTokens := map[string]bool{}
	pageToken := ""
	for page := 0; page < maxPages; page++ {
		resp, err := c.queryPage(ctx, eco, n, pageToken)
		if err != nil {
			return nil, err
		}
		for i := range resp.Vulns {
			rec, ok := toRecord(&resp.Vulns[i])
			if !ok {
				// A vuln carrying no ID is not an advisory we can attribute.
				continue
			}
			out = append(out, rec)
		}
		next := strings.TrimSpace(resp.NextPageToken)
		if next == "" {
			c.cache.Set(key, out)
			return out, nil
		}
		// A repeated token means the server is not advancing. Continuing would
		// loop until the page cap and then report a partial walk anyway.
		if seenTokens[next] {
			return nil, fmt.Errorf("osv query for %s/%s repeated a page token", eco, n)
		}
		seenTokens[next] = true
		pageToken = next
	}
	return nil, fmt.Errorf("osv query for %s/%s exceeded %d pages", eco, n, maxPages)
}

// queryPage issues one POST against the query endpoint.
func (c *Client) queryPage(ctx context.Context, ecosystem, name, pageToken string) (*queryResponse, error) {
	body, err := json.Marshal(queryRequest{
		Package:   queryPackageRef{Name: name, Ecosystem: ecosystem},
		PageToken: pageToken,
	})
	if err != nil {
		return nil, fmt.Errorf("osv query body encode failed: %w", err)
	}
	apiURL := c.resolvedBaseURL() + "/v1/query"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("osv query request build failed: %w", err)
	}
	req.Header.Set("User-Agent", osvUserAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("osv query http failed: %w", err)
	}
	// best-effort cleanup; the body is fully read below and any close error
	// would only mask the decode error this function returns.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("osv query http status %d", resp.StatusCode)
	}

	// Read one byte past the cap so an oversized body is detected rather than
	// silently truncated: a truncated body can still decode as a valid JSON
	// prefix, which would under-report advisories without any error.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("osv query read failed: %w", err)
	}
	if len(raw) > maxJSONResponseSize {
		return nil, fmt.Errorf("osv query response exceeded %d bytes", maxJSONResponseSize)
	}
	var out queryResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("osv query decode failed: %w", err)
	}
	return &out, nil
}

func (c *Client) resolvedBaseURL() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return "https://api.osv.dev"
}

// --- OSV wire format. Every JSON tag in this package lives here. ---

type queryPackageRef struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type queryRequest struct {
	Package   queryPackageRef `json:"package"`
	PageToken string          `json:"page_token,omitempty"`
}

type queryResponse struct {
	Vulns         []wireVuln `json:"vulns"`
	NextPageToken string     `json:"next_page_token"`
}

type wireVuln struct {
	ID      string   `json:"id"`
	Aliases []string `json:"aliases"`
	Summary string   `json:"summary"`
	// Published is RFC3339. An unparseable or absent value leaves the record's
	// Published zero, which the domain treats as a non-match.
	Published string `json:"published"`
	// Withdrawn is a timestamp, not a flag: its presence is the retraction.
	Withdrawn  string          `json:"withdrawn"`
	References []wireReference `json:"references"`
	Affected   []wireAffected  `json:"affected"`
}

type wireReference struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type wireAffected struct {
	Package          wirePackage `json:"package"`
	Ranges           []wireRange `json:"ranges"`
	Versions         []string    `json:"versions"`
	DatabaseSpecific struct {
		Informational string `json:"informational"`
	} `json:"database_specific"`
}

type wirePackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type wireRange struct {
	Type   string      `json:"type"`
	Events []wireEvent `json:"events"`
}

type wireEvent struct {
	Introduced   string `json:"introduced"`
	Fixed        string `json:"fixed"`
	LastAffected string `json:"last_affected"`
	Limit        string `json:"limit"`
	// unrecognized is set when the event object carries a key outside the four
	// above. Such an event is passed on with every field empty, which the
	// domain rejects, rather than decoded as whatever subset it happens to share.
	unrecognized bool
}

// UnmarshalJSON decodes an OSV range event and records whether it carried any
// key this client does not know.
func (e *wireEvent) UnmarshalJSON(data []byte) error {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return fmt.Errorf("decode osv range event: %w", err)
	}
	type plain wireEvent
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("decode osv range event: %w", err)
	}
	*e = wireEvent(p)
	for k := range keys {
		switch k {
		case "introduced", "fixed", "last_affected", "limit":
		default:
			e.unrecognized = true
		}
	}
	return nil
}

// isPlainAdvisoryID reports whether id is a non-empty run of ASCII letters,
// digits and hyphens, the shape every OSV database uses (RUSTSEC-2024-0375,
// GHSA-mc8h-8q98-g5hr).
//
// Why not strip the offending characters as sanitizeSummary does: the ID is
// printed as the name of the advisory, and a repaired ID would name one that
// does not exist. Rejecting the record fails safe — it can only cost a
// detection.
func isPlainAdvisoryID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

// toRecord maps one OSV vuln onto the domain's neutral record. Reports false
// when the response carried no usable advisory ID, which is not an advisory we
// could attribute a fact to.
func toRecord(v *wireVuln) (domain.AdvisoryRecord, bool) {
	id := strings.TrimSpace(v.ID)
	if !isPlainAdvisoryID(id) {
		return domain.AdvisoryRecord{}, false
	}
	rec := domain.AdvisoryRecord{
		ID:        id,
		Summary:   sanitizeSummary(v.Summary),
		Reference: advisoryReference(v.References),
		Withdrawn: strings.TrimSpace(v.Withdrawn) != "",
	}
	for _, alias := range v.Aliases {
		// A malformed alias is dropped like a malformed ID; losing one only
		// costs the assessor an exclusion.
		if alias = strings.TrimSpace(alias); isPlainAdvisoryID(alias) {
			rec.Aliases = append(rec.Aliases, alias)
		}
	}
	if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(v.Published)); err == nil {
		rec.Published = ts
	}
	for i := range v.Affected {
		af := &v.Affected[i]
		if strings.TrimSpace(af.Package.Name) == "" {
			// An affected entry naming no package cannot be matched against the
			// queried one; dropping it keeps the domain's universal honest.
			continue
		}
		out := domain.AdvisoryAffected{
			Ecosystem:     af.Package.Ecosystem,
			Name:          af.Package.Name,
			Versions:      af.Versions,
			Informational: strings.TrimSpace(af.DatabaseSpecific.Informational),
		}
		for _, r := range af.Ranges {
			dr := domain.AdvisoryRange{Type: r.Type}
			for _, ev := range r.Events {
				if ev.unrecognized {
					dr.Events = append(dr.Events, domain.AdvisoryRangeEvent{})
					continue
				}
				dr.Events = append(dr.Events, domain.AdvisoryRangeEvent{
					Introduced:   ev.Introduced,
					Fixed:        ev.Fixed,
					LastAffected: ev.LastAffected,
					Limit:        ev.Limit,
				})
			}
			out.Ranges = append(out.Ranges, dr)
		}
		rec.Affected = append(rec.Affected, out)
	}
	return rec, true
}

// advisoryReference returns the URL of the advisory's own page, preferring the
// ADVISORY-typed reference OSV defines for exactly this purpose. Falls back to
// the first reference carrying a URL so evidence is not silently empty.
func advisoryReference(refs []wireReference) string {
	fallback := ""
	for _, r := range refs {
		u := strings.TrimSpace(r.URL)
		if !isPlainWebURL(u) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(r.Type), advisoryReferenceType) {
			return u
		}
		if fallback == "" {
			fallback = u
		}
	}
	return fallback
}

// isPlainWebURL reports whether u is an absolute http(s) URL carrying no
// control or format runes.
//
// Why not pass the URL through as written: it is published as the evidence a
// reader opens, so a "javascript:" or "data:" URL, or one hiding an ANSI escape,
// would travel from an advisory database into a terminal or a consumer's UI.
// Dropping it costs a link, never a verdict.
func isPlainWebURL(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	for _, r := range u {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// sanitizeSummary makes an advisory summary safe to print, then collapses it to
// a single capped line.
//
// Control characters (Cc, carrying the ANSI escape introducer and the carriage
// return) and format characters (Cf, carrying the bidirectional overrides and
// zero-width characters) are dropped: the CLI prints the summary verbatim, and
// either class can repaint or visually reorder the surrounding output.
// Whitespace is exempt so ordinary line breaks survive to be collapsed.
func sanitizeSummary(raw string) string {
	return domain.SanitizeExternalSummary(raw)
}
