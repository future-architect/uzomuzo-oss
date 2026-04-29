package maven

import (
	"context"
	"fmt"
	"strings"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/licenses"
)

// FetchLicenses fetches the POM for the given coordinates and extracts <licenses>
// entries, returning each as a domain.ResolvedLicense. It performs a single POM
// fetch with no parent traversal: license declarations in Maven are per-artifact
// metadata, and parent inheritance for <licenses> is rare enough that the extra
// HTTP cost is not justified. Property placeholders (${license.name}) inside
// <name>/<url> are expanded using the same merge rules as GetRepoURL.
//
// Each <license> entry is normalized via the following decision tree:
//  1. If <name> normalizes to a canonical SPDX identifier, return SPDX.
//  2. Else if <url> matches a known license URL (apache.org, opensource.org,
//     gnu.org, etc.), return SPDX.
//  3. Else return non-standard with the raw value preserved.
//
// DDD Layer: Infrastructure
// Responsibility: Read Maven POM <licenses> for ecosystem-native license fallback.
//
// Returns:
//   - ([]ResolvedLicense, true, nil) when at least one <license> is found.
//   - (nil, false, nil) when the POM is missing, has no <licenses>, or all
//     entries are blank after expansion.
//   - (nil, false, err) on transport / decode errors.
func (c *Client) FetchLicenses(ctx context.Context, groupID, artifactID, version string) ([]domain.ResolvedLicense, bool, error) {
	g := strings.TrimSpace(groupID)
	a := strings.TrimSpace(artifactID)
	v := strings.TrimSpace(version)
	if g == "" || a == "" || v == "" {
		return nil, false, fmt.Errorf("groupId, artifactId and version are required")
	}
	pom, found, err := c.fetchPOM(ctx, g, a, v)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	if len(pom.Licenses.License) == 0 {
		return nil, false, nil
	}
	props := c.mergeProps(pom, nil, g, a, v)
	out := make([]domain.ResolvedLicense, 0, len(pom.Licenses.License))
	// Track keys to deduplicate same-license-twice declarations. Identifier is
	// the natural key for SPDX entries; non-standard entries fall back to Raw.
	seen := make(map[string]struct{}, len(pom.Licenses.License))
	for _, lic := range pom.Licenses.License {
		name := strings.TrimSpace(expand(props, lic.Name))
		urlStr := strings.TrimSpace(expand(props, lic.URL))
		rl := resolvePOMLicense(name, urlStr)
		if rl.IsZero() {
			continue
		}
		key := rl.Identifier
		if key == "" {
			key = "raw:" + rl.Raw
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, rl)
	}
	if len(out) == 0 {
		return nil, false, nil
	}
	return out, true, nil
}

// resolvePOMLicense applies the per-<license> decision tree.
// name and urlStr must already be trimmed and have property placeholders expanded.
//
// Raw precedence: when <name> is present the human-readable name is preserved
// in Raw (even when SPDX resolution succeeded via the URL fallback path); the
// URL is only stored as Raw for url-only entries.
func resolvePOMLicense(name, urlStr string) domain.ResolvedLicense {
	raw := name
	if raw == "" {
		raw = urlStr
	}
	if raw == "" {
		return domain.ResolvedLicense{}
	}
	if name != "" {
		if id, isSPDX := domain.NormalizeLicenseIdentifier(name); isSPDX {
			return domain.ResolvedLicense{
				Identifier: id,
				Source:     domain.LicenseSourceMavenPOMSPDX,
				Raw:        raw,
				IsSPDX:     true,
			}
		}
	}
	if urlStr != "" {
		if id := licenses.LookupLicenseURL(urlStr); id != "" {
			return domain.ResolvedLicense{
				Identifier: id,
				Source:     domain.LicenseSourceMavenPOMSPDX,
				Raw:        urlStr,
				IsSPDX:     true,
			}
		}
	}
	return domain.ResolvedLicense{
		Source: domain.LicenseSourceMavenPOMNonStandard,
		Raw:    raw,
	}
}
