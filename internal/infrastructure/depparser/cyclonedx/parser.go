// Package cyclonedx implements a CycloneDX SBOM parser for dependency extraction.
//
// DDD Layer: Infrastructure (external format parsing)
package cyclonedx

import (
	"encoding/json"
	"fmt"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"github.com/package-url/packageurl-go"
)

// Parser implements depparser.DependencyParser for CycloneDX SBOM JSON.
type Parser struct{}

// FormatName returns the parser's display name.
func (p *Parser) FormatName() string { return "CycloneDX SBOM" }

// Parse extracts PURLs from a CycloneDX JSON SBOM.
// It recursively walks nested components and deduplicates by PURL string.
// Components without a purl field are silently skipped.
// Qualifiers (e.g., syft's ?package-id=) are stripped for clean PURLs.
func (p *Parser) Parse(data []byte) ([]depparser.ParsedDependency, error) {
	var bom bomEnvelope
	if err := json.Unmarshal(data, &bom); err != nil {
		return nil, fmt.Errorf("failed to parse CycloneDX JSON: %w", err)
	}

	seen := make(map[string]struct{})
	var deps []depparser.ParsedDependency
	extractPURLs(bom.Components, seen, &deps, 0)
	return deps, nil
}

// bomEnvelope is the minimal CycloneDX structure needed for PURL extraction.
type bomEnvelope struct {
	Components []component `json:"components"`
}

type component struct {
	PURL       string      `json:"purl"`
	Components []component `json:"components"`
}

// maxNestingDepth limits recursive component traversal to prevent stack overflow
// from maliciously crafted SBOMs.
const maxNestingDepth = 100

// extractPURLs recursively walks components and extracts deduplicated dependencies.
// Recursion stops at maxNestingDepth to guard against malicious input.
func extractPURLs(components []component, seen map[string]struct{}, deps *[]depparser.ParsedDependency, depth int) {
	if depth > maxNestingDepth {
		return
	}
	for _, c := range components {
		if c.PURL != "" {
			dep, err := normalizePURL(c.PURL)
			if err == nil {
				if _, exists := seen[dep.PURL]; !exists {
					seen[dep.PURL] = struct{}{}
					*deps = append(*deps, dep)
				}
			}
		}
		if len(c.Components) > 0 {
			extractPURLs(c.Components, seen, deps, depth+1)
		}
	}
}

// normalizePURL parses and rebuilds a PURL, stripping qualifiers added by tools
// (e.g., syft's ?package-id=, ?vcs_url=).
func normalizePURL(raw string) (depparser.ParsedDependency, error) {
	parsed, err := packageurl.FromString(raw)
	if err != nil {
		return depparser.ParsedDependency{}, fmt.Errorf("invalid PURL %q: %w", raw, err)
	}

	// Rebuild clean PURL without qualifiers and subpath
	clean := packageurl.NewPackageURL(
		parsed.Type,
		parsed.Namespace,
		parsed.Name,
		parsed.Version,
		nil, // strip qualifiers
		"",  // strip subpath
	).ToString()

	// Build name: namespace/name for Go and Maven, just name for others
	name := parsed.Name
	if parsed.Namespace != "" {
		name = parsed.Namespace + "/" + parsed.Name
	}

	return depparser.ParsedDependency{
		PURL:      clean,
		Ecosystem: parsed.Type,
		Name:      name,
		Version:   parsed.Version,
	}, nil
}
