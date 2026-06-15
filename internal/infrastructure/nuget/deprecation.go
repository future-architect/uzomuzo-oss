package nuget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// DeprecationInfo carries NuGet deprecation metadata (if any).
type DeprecationInfo struct {
	Reasons            []string
	Message            string
	AlternatePackageID string
}

// Minimal JSON shapes for Registration API

type registrationIndex struct {
	Items []registrationPageRef `json:"items"`
}

type registrationPageRef struct {
	ID    string             `json:"@id"`
	Items []registrationLeaf `json:"items"`
}

type registrationPage struct {
	Items []registrationLeaf `json:"items"`
}

type registrationLeaf struct {
	ID string `json:"@id"`
	// CatalogEntry can be either an object or a string URL depending on embedding.
	// We don't need its contents for deprecation checks, so accept any JSON.
	CatalogEntry json.RawMessage          `json:"catalogEntry"`
	Deprecation  *registrationDeprecation `json:"deprecation"`
}

type registrationDeprecation struct {
	Reasons          []string                `json:"reasons"`
	Message          string                  `json:"message"`
	AlternatePackage *registrationAltPackage `json:"alternatePackage"`
}

type registrationAltPackage struct {
	ID    string `json:"id"`
	Range string `json:"range"`
}

// catalogEntryDoc represents the minimal shape of a catalog entry when embedded or fetched.
type catalogEntryDoc struct {
	Deprecation *registrationDeprecation `json:"deprecation"`
}

// GetDeprecation fetches deprecation information for a package id.
// Returns (info, found, error). When found=false, info is nil.
func (c *Client) GetDeprecation(ctx context.Context, packageID string) (*DeprecationInfo, bool, error) {
	id := strings.TrimSpace(packageID)
	if id == "" {
		return nil, false, fmt.Errorf("package id is required")
	}

	// Cache
	if c.cacheTTL > 0 {
		c.mu.Lock()
		if ce, ok := c.cache[id]; ok {
			if time.Since(ce.fetchedAt) < c.cacheTTL {
				info := ce.info
				found := ce.found
				c.mu.Unlock()
				slog.Debug("nuget: cache hit for deprecation", "id", id, "found", found)
				return info, found, nil
			}
		}
		c.mu.Unlock()
	}

	// Registration index endpoint (with fallback base variants)
	idLower := strings.ToLower(id) // NuGet registration path requires lowercase package ID
	candidates := c.getRegistrationCandidates(ctx)

	var last404 bool
	var anyOK bool
	var decodeErrors int
	for idx, b := range candidates {
		endpoint := fmt.Sprintf("%s/%s/index.json", b, url.PathEscape(idLower))
		slog.Debug("nuget: request registration index", "id", id, "endpoint", endpoint, "attempt", idx+1)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, false, fmt.Errorf("failed to build NuGet request: %w", err)
		}
		// Be a good citizen: set a descriptive User-Agent
		req.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
		resp, err := c.http.Do(ctx, req)
		if err != nil {
			return nil, false, fmt.Errorf("NuGet HTTP error: %w", err)
		}
		if resp.StatusCode == http.StatusNotFound {
			slog.Debug("nuget: registration index not found", "id", id, "endpoint", endpoint)
			last404 = true
			_ = resp.Body.Close() // best-effort cleanup
			continue
		}
		if resp.StatusCode != http.StatusOK {
			slog.Debug("nuget: non-OK status from registration index", "id", id, "status", resp.StatusCode, "endpoint", endpoint)
			_ = resp.Body.Close() // best-effort cleanup
			// Non-OK other than 404: treat as error for now
			return nil, false, fmt.Errorf("NuGet HTTP %d", resp.StatusCode)
		}

		anyOK = true
		var reg registrationIndex
		if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
			// Do not abort on decode errors (e.g., slow network/timeouts while reading body).
			// Try next candidate (gz/non-gz) or fallback scraper.
			slog.Debug("nuget: decode failed for registration index", "id", id, "error", err)
			_ = resp.Body.Close() // best-effort cleanup
			decodeErrors++
			continue
		}
		_ = resp.Body.Close() // best-effort cleanup

		if info, found, err := c.deprecationFromIndex(ctx, reg, id); err != nil {
			return nil, false, fmt.Errorf("deprecation index lookup for %q: %w", id, err)
		} else if found {
			c.remember(id, info)
			return info, true, nil
		}
		// No deprecation in this variant, try next
		slog.Debug("nuget: no deprecation in this index variant", "id", id, "endpoint", endpoint)
	}

	// No deprecation found across all attempts
	if !anyOK && last404 {
		slog.Debug("nuget: no deprecation found (all variants 404)", "id", id)
	} else {
		slog.Debug("nuget: no deprecation found (checked variants)", "id", id)
	}
	// Suppress unused warning: decodeErrors is intentional (tracked for future metrics).
	_ = decodeErrors
	// Best-effort HTML fallback for nuget.org: scrape the package page for a deprecation banner
	if info, ok := c.scrapeDeprecationFromNuGetHTML(ctx, id); ok {
		slog.Debug("nuget: deprecation found via HTML fallback", "id", id, "alt", info.AlternatePackageID)
		c.remember(id, info)
		return info, true, nil
	}
	c.remember(id, nil)
	return nil, false, nil
}

// deprecationFromIndex iterates registration index pages for a deprecation entry.
// It handles both embedded leaves and remote page documents.
// Returns (info, true, nil) when deprecation is found, (nil, false, nil) when not found,
// or (nil, false, err) on a non-recoverable error.
func (c *Client) deprecationFromIndex(ctx context.Context, reg registrationIndex, id string) (*DeprecationInfo, bool, error) {
	for _, page := range reg.Items {
		// Embedded leaves
		if len(page.Items) > 0 {
			if info, ok := extractFirstDeprecation(page.Items); ok {
				slog.Debug("nuget: deprecation found (embedded)", "id", id, "reasons", info.Reasons, "alt", info.AlternatePackageID)
				return info, true, nil
			}
			continue
		}
		// Need to fetch page
		if page.ID == "" {
			continue
		}
		slog.Debug("nuget: fetching registration page", "id", id, "page", page.ID)
		piReq, err := http.NewRequestWithContext(ctx, http.MethodGet, page.ID, nil)
		if err != nil {
			return nil, false, fmt.Errorf("NuGet request (page) failed: %w", err)
		}
		piReq.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
		piResp, err := c.http.Do(ctx, piReq)
		if err != nil {
			return nil, false, fmt.Errorf("NuGet HTTP (page) error: %w", err)
		}
		if piResp.StatusCode != http.StatusOK {
			_ = piResp.Body.Close() // best-effort cleanup
			slog.Debug("nuget: non-OK page status", "id", id, "status", piResp.StatusCode, "page", page.ID)
			continue
		}
		var leaf registrationPage
		if err := json.NewDecoder(piResp.Body).Decode(&leaf); err != nil {
			_ = piResp.Body.Close() // best-effort cleanup
			slog.Debug("nuget: page decode failed", "id", id, "error", err)
			return nil, false, fmt.Errorf("NuGet page decode failed: %w", err)
		}
		_ = piResp.Body.Close() // best-effort cleanup
		if info, ok := extractFirstDeprecation(leaf.Items); ok {
			slog.Debug("nuget: deprecation found (page)", "id", id, "reasons", info.Reasons, "alt", info.AlternatePackageID)
			return info, true, nil
		}
	}
	return nil, false, nil
}

// extractFirstDeprecation scans registration leaves and returns the first deprecation entry found.
// Returns (info, true) when found, (nil, false) when no deprecation is present.
func extractFirstDeprecation(items []registrationLeaf) (*DeprecationInfo, bool) {
	for _, it := range items {
		// 1) Leaf-level deprecation
		if it.Deprecation != nil {
			altID := ""
			if it.Deprecation.AlternatePackage != nil {
				altID = strings.TrimSpace(it.Deprecation.AlternatePackage.ID)
			}
			info := &DeprecationInfo{
				Reasons:            append([]string(nil), it.Deprecation.Reasons...),
				Message:            strings.TrimSpace(it.Deprecation.Message),
				AlternatePackageID: altID,
			}
			return info, true
		}
		// 2) catalogEntry may be embedded as an object and include deprecation
		if len(it.CatalogEntry) > 0 && len(it.CatalogEntry) >= 1 && it.CatalogEntry[0] == '{' {
			var ce catalogEntryDoc
			if err := json.Unmarshal(it.CatalogEntry, &ce); err == nil && ce.Deprecation != nil {
				altID := ""
				if ce.Deprecation.AlternatePackage != nil {
					altID = strings.TrimSpace(ce.Deprecation.AlternatePackage.ID)
				}
				info := &DeprecationInfo{
					Reasons:            append([]string(nil), ce.Deprecation.Reasons...),
					Message:            strings.TrimSpace(ce.Deprecation.Message),
					AlternatePackageID: altID,
				}
				return info, true
			}
		}
	}
	return nil, false
}

// scrapeDeprecationFromNuGetHTML performs a best-effort parse of the nuget.org package page
// to determine if the package is deprecated and to extract a suggested successor.
// It returns (info, true) when a deprecation banner is detected. Message and successor are optional.
func (c *Client) scrapeDeprecationFromNuGetHTML(ctx context.Context, id string) (*DeprecationInfo, bool) {
	base := c.htmlBase
	if strings.TrimSpace(base) == "" {
		base = "https://www.nuget.org"
	}
	// Use the provided ID as-is; nuget.org routing is case-insensitive.
	pageURL := fmt.Sprintf("%s/packages/%s", strings.TrimRight(base, "/"), url.PathEscape(id))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, false
	}
	// Read up to 512KB
	const maxRead = 512 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRead))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false
	}
	lower := strings.ToLower(string(body))
	if !strings.Contains(lower, "this package has been deprecated") && !strings.Contains(lower, "deprecated") {
		return nil, false
	}
	// Try to extract a successor from a Suggested Alternatives section link
	// Example snippet:
	//   Suggested Alternatives
	//   <a href="/packages/Azure.Messaging.EventHubs">Azure.Messaging.EventHubs</a>
	reAlt := regexp.MustCompile(`(?is)suggested\s+alternatives?.{0,400}?<a[^>]*href="/packages/([A-Za-z0-9_.\-]+)/?"[^>]*>\s*([^<]+)\s*</a>`)
	var altID string
	if m := reAlt.FindSubmatch(body); len(m) >= 2 {
		altID = strings.TrimSpace(string(m[1]))
	}
	// Extract a brief message from the deprecation area if present
	// Heuristic: grab up to 200 chars around the first occurrence of "deprecated"
	msg := ""
	if idx := strings.Index(lower, "deprecated"); idx >= 0 {
		start := idx - 80
		if start < 0 {
			start = 0
		}
		end := idx + 200
		if end > len(body) {
			end = len(body)
		}
		snippet := string(body[start:end])
		// Collapse whitespace
		snippet = regexp.MustCompile(`\s+`).ReplaceAllString(snippet, " ")
		msg = strings.TrimSpace(snippet)
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
	}
	// We do not know the NuGet machine reason (Legacy/CriticalBugs/Other) from HTML alone.
	// Provide a neutral reason to indicate deprecation was detected via HTML.
	reasons := []string{"Other"}
	return &DeprecationInfo{Reasons: reasons, Message: msg, AlternatePackageID: altID}, true
}
