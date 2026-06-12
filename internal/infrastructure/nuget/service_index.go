package nuget

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ServiceIndex represents the minimal shape of the NuGet v3 service index.
type ServiceIndex struct {
	Resources []struct {
		ID   string `json:"@id"`
		Type string `json:"@type"`
	} `json:"resources"`
}

// getRegistrationCandidates returns a list of Registration Base URLs to try.
// Order of precedence:
//  1. Discovered bases from the service index (cached with TTL)
//  2. Configured baseURL
//  3. Sibling variant on nuget.org (semver2 <-> gz-semver2)
//  4. Fallback to known nuget.org gz-semver2 base
func (c *Client) getRegistrationCandidates(ctx context.Context) []string {
	base := strings.TrimRight(c.baseURL, "/")
	// If baseURL is overridden (not the default), honor it and skip network discovery
	if base != strings.TrimRight(defaultRegistrationBase, "/") {
		candidates := []string{base}
		if strings.Contains(base, "registration5-semver2") && !strings.Contains(base, "registration5-gz-semver2") {
			candidates = append(candidates, strings.Replace(base, "registration5-semver2", "registration5-gz-semver2", 1))
		} else if strings.Contains(base, "registration5-gz-semver2") {
			candidates = append(candidates, strings.Replace(base, "registration5-gz-semver2", "registration5-semver2", 1))
		} else {
			candidates = append(candidates, "https://api.nuget.org/v3/registration5-gz-semver2")
		}
		return candidates
	}

	// Otherwise, try discovery first
	discovered := c.discoverRegistrationBases(ctx)
	if len(discovered) > 0 {
		return discovered
	}
	candidates := []string{base}
	if strings.Contains(base, "registration5-semver2") && !strings.Contains(base, "registration5-gz-semver2") {
		candidates = append(candidates, strings.Replace(base, "registration5-semver2", "registration5-gz-semver2", 1))
	} else if strings.Contains(base, "registration5-gz-semver2") {
		candidates = append(candidates, strings.Replace(base, "registration5-gz-semver2", "registration5-semver2", 1))
	} else {
		candidates = append(candidates, "https://api.nuget.org/v3/registration5-gz-semver2")
	}
	return candidates
}

// discoverRegistrationBases loads the service index (if TTL expired) and extracts Registration Base URLs.
// It caches the discovered list and returns it for use by callers.
func (c *Client) discoverRegistrationBases(ctx context.Context) []string {
	c.mu.Lock()
	idxURL := c.serviceIndexURL
	ttl := c.serviceIndexTTL
	// If cache is fresh, return it immediately.
	if len(c.discoveredBases) > 0 && time.Since(c.discoveredAt) < ttl {
		bases := append([]string(nil), c.discoveredBases...)
		c.mu.Unlock()
		return bases
	}
	c.mu.Unlock()

	// No fresh cache, attempt discovery.
	if strings.TrimSpace(idxURL) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, idxURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "uzomuzo-nuget-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var idx ServiceIndex
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return nil
	}

	// Collect registration base URLs. Prefer semver2, then gz-semver2, then any RegistrationsBaseUrl.
	var semver2 []string
	var gzSemver2 []string
	var generic []string
	for _, r := range idx.Resources {
		t := strings.ToLower(r.Type)
		if r.ID == "" || t == "" {
			continue
		}
		switch {
		case strings.Contains(t, "registrationsbaseurl/3.6.0") || strings.Contains(t, "registrationsbaseurl/3.5.0") || strings.Contains(t, "registrationsbaseurl/3.0.0"):
			// Generic match; we will still sort by content of the URL for semver2 preference later
			if strings.Contains(strings.ToLower(r.ID), "registration5-semver2") {
				semver2 = append(semver2, strings.TrimRight(r.ID, "/"))
			} else if strings.Contains(strings.ToLower(r.ID), "registration5-gz-semver2") {
				gzSemver2 = append(gzSemver2, strings.TrimRight(r.ID, "/"))
			} else {
				generic = append(generic, strings.TrimRight(r.ID, "/"))
			}
		case strings.Contains(t, "registrationsbaseurl"):
			// Fallback broad match
			if strings.Contains(strings.ToLower(r.ID), "registration5-semver2") {
				semver2 = append(semver2, strings.TrimRight(r.ID, "/"))
			} else if strings.Contains(strings.ToLower(r.ID), "registration5-gz-semver2") {
				gzSemver2 = append(gzSemver2, strings.TrimRight(r.ID, "/"))
			} else {
				generic = append(generic, strings.TrimRight(r.ID, "/"))
			}
		}
	}
	// Merge with precedence: semver2 first, then gz-semver2, then generic.
	dedup := make(map[string]bool)
	var bases []string
	for _, list := range [][]string{semver2, gzSemver2, generic} {
		for _, b := range list {
			if !dedup[b] {
				dedup[b] = true
				bases = append(bases, b)
			}
		}
	}
	if len(bases) == 0 {
		return nil
	}
	c.mu.Lock()
	c.discoveredBases = bases
	c.discoveredAt = time.Now()
	c.mu.Unlock()
	slog.Debug("nuget: discovered registration bases", "count", len(bases))
	return append([]string(nil), bases...)
}
