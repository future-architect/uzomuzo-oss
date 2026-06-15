package depsdev

import (
	"context"
	"fmt"
	"sort"
	"strings"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// GetPackageVersionLicenses fetches license identifiers (SPDX preferred) for a specific versioned PURL.
// The input must include an explicit version (e.g., pkg:npm/%40babel/core@7.25.0).
// Returns a normalized, deduplicated, sorted slice of canonical SPDX identifiers (casing preserved, e.g.
// Apache-2.0, GPL-3.0-only) or an empty slice if none found. Non-SPDX strings may appear only as
// fallback-normalized tokens (spaces collapsed to dashes). Empty values and NOASSERTION are excluded.
// Errors are wrapped with context for upstream diagnostics.
func (c *DepsDevClient) GetPackageVersionLicenses(ctx context.Context, versionedPURL string) ([]string, error) {
	vp := strings.TrimSpace(versionedPURL)
	if vp == "" {
		return nil, fmt.Errorf("empty versioned PURL")
	}
	resp, err := c.fetchPackageInfo(ctx, vp)
	if err != nil {
		return nil, fmt.Errorf("fetch package info for license (purl=%s): %w", vp, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("nil package response (purl=%s)", vp)
	}
	licenses := collectVersionLicenses(&resp.Version)
	return licenses, nil
}

// collectVersionLicenses extracts SPDX (preferred) or raw license identifiers from a Version.
// Normalization steps:
//  1. Collect non-empty SPDX strings (LicenseDetails[].Spdx) and normalize via domain.NormalizeLicenseIdentifier
//     preserving canonical SPDX casing (e.g., Apache-2.0, GPL-3.0-only, LGPL-2.1-or-later).
//  2. If none collected, fallback to Version.Licenses values with the same normalization.
//  3. Deduplicate (normalization makes comparison case-insensitive) and return a sorted slice (canonical casing preserved).
//  4. Excludes empty strings and NOASSERTION.
func collectVersionLicenses(v *Version) []string {
	if v == nil {
		return nil
	}
	set := make(map[string]struct{})
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if norm, _ := domain.NormalizeLicenseIdentifier(raw); norm != "" && !strings.EqualFold(norm, "NOASSERTION") {
			set[norm] = struct{}{}
		}
	}
	for _, d := range v.LicenseDetails {
		if d.Spdx != "" {
			add(d.Spdx)
		}
	}
	if len(set) == 0 {
		for _, l := range v.Licenses {
			add(l)
		}
	}
	if len(set) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
