// Package cyclonedx implements a CycloneDX SBOM parser for dependency extraction.
//
// DDD Layer: Infrastructure (external format parsing)
package cyclonedx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/sbomgraph"
	"github.com/package-url/packageurl-go"
)

// SniffPrefixLen is the number of bytes inspected when sniffing file format.
const SniffPrefixLen = 512

// IsCycloneDXJSON performs a quick sniff to detect CycloneDX JSON format
// by checking for "bomFormat" with value "CycloneDX" in the first 512 bytes.
// It handles both compact (`"bomFormat":"CycloneDX"`) and pretty-printed
// (`"bomFormat": "CycloneDX"`) JSON.
func IsCycloneDXJSON(data []byte) bool {
	prefix := data[:min(len(data), SniffPrefixLen)]
	if !bytes.Contains(prefix, []byte(`"bomFormat"`)) {
		return false
	}
	// Decode just the bomFormat field to validate the value.
	var header struct {
		BOMFormat string `json:"bomFormat"`
	}
	if err := json.Unmarshal(prefix, &header); err == nil {
		return header.BOMFormat == "CycloneDX"
	}

	// prefix may be truncated; fall back to a stricter search that
	// validates bomFormat's value is exactly "CycloneDX".
	bomIdx := bytes.Index(prefix, []byte(`"bomFormat"`))
	if bomIdx == -1 {
		return false
	}
	rest := prefix[bomIdx+len(`"bomFormat"`):]
	colonIdx := bytes.IndexByte(rest, ':')
	if colonIdx == -1 {
		return false
	}
	rest = bytes.TrimLeft(rest[colonIdx+1:], " \t\r\n")
	if len(rest) == 0 || rest[0] != '"' {
		return false
	}
	endIdx := bytes.IndexByte(rest[1:], '"')
	if endIdx == -1 {
		return false
	}
	return bytes.Equal(rest[1:1+endIdx], []byte("CycloneDX"))
}

// Parser implements depparser.DependencyParser for CycloneDX SBOM JSON.
type Parser struct{}

// FormatName returns the parser's display name.
func (p *Parser) FormatName() string { return "CycloneDX SBOM" }

// Parse extracts PURLs from a CycloneDX JSON SBOM.
// It recursively walks nested components and deduplicates by PURL string.
// Components without a purl field are silently skipped.
// Qualifiers (e.g., syft's ?package-id=) are stripped for clean PURLs.
//
// When the SBOM contains a dependencies section and a metadata.component,
// each dependency is classified as direct or transitive based on the root
// component's dependsOn list. If these sections are absent, all dependencies
// are marked as RelationUnknown.
func (p *Parser) Parse(_ context.Context, data []byte) ([]depparser.ParsedDependency, error) {
	var bom sbomgraph.BOMEnvelope
	if err := json.Unmarshal(data, &bom); err != nil {
		return nil, fmt.Errorf("failed to parse CycloneDX JSON: %w", err)
	}

	// Build ref-to-normalizedPURL map for dependency resolution.
	refMap := sbomgraph.BuildRefMap(bom.Components)
	// Determine which PURLs are direct dependencies of the root component.
	directPURLs := sbomgraph.ResolveDirectPURLs(&bom, refMap)
	// For transitive deps, determine which direct deps they are pulled in through.
	viaParents := resolveViaParents(&bom, refMap, directPURLs)

	seen := make(map[string]struct{})
	var deps []depparser.ParsedDependency
	extractPURLs(bom.Components, seen, &deps, 0, directPURLs, viaParents)
	return deps, nil
}

// extractPURLs recursively walks components and extracts deduplicated dependencies.
// directPURLs maps normalized PURLs that are direct dependencies of the root component.
// When directPURLs is nil, all dependencies are marked RelationUnknown.
// viaParents maps transitive dep PURLs to the short names of their direct ancestors.
// Recursion stops at MaxNestingDepth to guard against malicious input.
func extractPURLs(components []sbomgraph.Component, seen map[string]struct{}, deps *[]depparser.ParsedDependency, depth int, directPURLs map[string]struct{}, viaParents map[string][]string) {
	if depth > sbomgraph.MaxNestingDepth {
		slog.Warn(
			"max CycloneDX SBOM component nesting depth exceeded; dependency extraction truncated",
			"maxDepth", sbomgraph.MaxNestingDepth,
			"depth", depth,
		)
		return
	}
	for _, c := range components {
		if c.PURL != "" {
			dep, err := normalizePURL(c.PURL)
			if err != nil {
				slog.Debug("skipping invalid PURL in SBOM component", "purl", c.PURL, "error", err)
				continue
			}
			if _, exists := seen[dep.PURL]; !exists {
				seen[dep.PURL] = struct{}{}
				dep.Relation = classifyRelation(dep.PURL, directPURLs)
				if viaParents != nil {
					dep.ViaParents = viaParents[dep.PURL]
				}
				*deps = append(*deps, dep)
			}
		}
		if len(c.Components) > 0 {
			extractPURLs(c.Components, seen, deps, depth+1, directPURLs, viaParents)
		}
	}
}

// classifyRelation determines the dependency relation for a given normalized PURL.
// Returns RelationUnknown when directPURLs is nil (no dependency info available),
// RelationDirect when the PURL is in the set, and RelationTransitive otherwise.
func classifyRelation(normalizedPURL string, directPURLs map[string]struct{}) depparser.DependencyRelation {
	if directPURLs == nil {
		return depparser.RelationUnknown
	}
	if _, ok := directPURLs[normalizedPURL]; ok {
		return depparser.RelationDirect
	}
	return depparser.RelationTransitive
}

// resolveViaParents determines which direct dependencies each transitive dep
// is pulled in through by performing BFS from each direct dependency.
// Returns nil when dependency graph info is unavailable.
func resolveViaParents(bom *sbomgraph.BOMEnvelope, refMap map[string]string, directPURLs map[string]struct{}) map[string][]string {
	if directPURLs == nil || len(bom.Dependencies) == 0 {
		return nil
	}

	// Build adjacency list: normalizedPURL → []normalizedPURL.
	adj := sbomgraph.BuildAdjacencyList(bom.Dependencies, refMap)

	// For each direct dep, BFS to find all reachable transitive deps.
	viaMap := make(map[string][]string)
	for directPURL := range directPURLs {
		directDep, err := normalizePURL(directPURL)
		if err != nil {
			continue
		}
		shortName := directDep.Name

		visited := map[string]bool{directPURL: true}
		queue := []string{directPURL}
		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			for _, next := range adj[curr] {
				if visited[next] {
					continue
				}
				visited[next] = true
				if _, isDirect := directPURLs[next]; !isDirect {
					viaMap[next] = append(viaMap[next], shortName)
				}
				queue = append(queue, next)
			}
		}
	}

	// Sort each via list for deterministic output.
	for k := range viaMap {
		sort.Strings(viaMap[k])
	}

	return viaMap
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
