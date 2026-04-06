// Package sbomgraph provides shared CycloneDX SBOM types and utility functions
// used by both the dependency parser and the dependency graph analyzer.
package sbomgraph

import (
	"log/slog"

	"github.com/package-url/packageurl-go"
)

// MaxNestingDepth limits recursive component traversal to prevent stack overflow
// from maliciously crafted SBOMs.
const MaxNestingDepth = 100

// BOMEnvelope is the minimal CycloneDX structure needed for PURL extraction
// and dependency relation resolution.
type BOMEnvelope struct {
	Metadata     *BOMMetadata `json:"metadata"`
	Components   []Component  `json:"components"`
	Dependencies []Dependency `json:"dependencies"`
}

// BOMMetadata holds the root component identity for dependency relation resolution.
type BOMMetadata struct {
	Component *Component `json:"component"`
}

// Component represents a CycloneDX component with optional nested sub-components.
type Component struct {
	BOMRef     string      `json:"bom-ref"`
	PURL       string      `json:"purl"`
	Components []Component `json:"components"`
}

// Dependency represents a single entry in the CycloneDX dependencies array.
// Each entry maps a component ref to its direct dependsOn refs.
type Dependency struct {
	Ref       string   `json:"ref"`
	DependsOn []string `json:"dependsOn"`
}

// BuildRefMap walks all components recursively and builds a mapping from
// bom-ref and raw PURL to normalized PURL. This allows resolving dependency
// references regardless of whether the tool uses bom-ref or PURL as the ref key.
func BuildRefMap(components []Component) map[string]string {
	m := make(map[string]string)
	buildRefMapRecursive(components, m, 0)
	return m
}

// buildRefMapRecursive populates m with bom-ref/raw-PURL → normalized-PURL mappings.
func buildRefMapRecursive(components []Component, m map[string]string, depth int) {
	if depth > MaxNestingDepth {
		slog.Warn(
			"max CycloneDX SBOM component nesting depth exceeded; ref map construction truncated",
			"maxDepth", MaxNestingDepth,
			"depth", depth,
		)
		return
	}
	for _, c := range components {
		if c.PURL != "" {
			normalized := NormalizePURL(c.PURL)
			if normalized == "" {
				continue
			}
			if c.BOMRef != "" {
				m[c.BOMRef] = normalized
			}
			m[c.PURL] = normalized
		}
		if len(c.Components) > 0 {
			buildRefMapRecursive(c.Components, m, depth+1)
		}
	}
}

// ResolveDirectPURLs identifies which normalized PURLs are direct dependencies
// of the root component by inspecting the CycloneDX dependencies section.
// Returns nil when the SBOM lacks metadata.component or the dependencies section,
// which causes all dependencies to be classified as RelationUnknown.
func ResolveDirectPURLs(bom *BOMEnvelope, refMap map[string]string) map[string]struct{} {
	if bom.Metadata == nil || bom.Metadata.Component == nil || len(bom.Dependencies) == 0 {
		return nil
	}

	rootRef := bom.Metadata.Component.BOMRef
	if rootRef == "" {
		rootRef = bom.Metadata.Component.PURL
	}
	if rootRef == "" {
		slog.Debug("CycloneDX metadata.component has no bom-ref or PURL; skipping relation resolution")
		return nil
	}

	var rootDeps []string
	for _, d := range bom.Dependencies {
		if d.Ref == rootRef {
			rootDeps = d.DependsOn
			break
		}
	}
	if rootDeps == nil {
		slog.Debug("root component not found in dependencies section", "ref", rootRef)
		return nil
	}

	directPURLs := make(map[string]struct{}, len(rootDeps))
	for _, ref := range rootDeps {
		if purl, ok := refMap[ref]; ok {
			directPURLs[purl] = struct{}{}
		} else {
			slog.Debug("dependency ref not found in component map", "ref", ref)
		}
	}
	return directPURLs
}

// NormalizePURL parses and rebuilds a PURL, stripping qualifiers and subpath.
// Qualifiers (e.g., syft's ?package-id=, ?vcs_url=) and subpaths are removed
// so that identity comparison uses only type/namespace/name/version.
// Returns an empty string on parse error.
func NormalizePURL(raw string) string {
	parsed, err := packageurl.FromString(raw)
	if err != nil {
		slog.Debug("invalid PURL", "purl", raw, "error", err)
		return ""
	}
	return packageurl.NewPackageURL(
		parsed.Type, parsed.Namespace, parsed.Name, parsed.Version, nil, "",
	).ToString()
}

// BuildAdjacencyList builds a normalizedPURL → []normalizedPURL adjacency list
// from CycloneDX dependency entries and a ref-to-PURL map.
func BuildAdjacencyList(deps []Dependency, refMap map[string]string) map[string][]string {
	adj := make(map[string][]string)
	for _, d := range deps {
		fromPURL, ok := refMap[d.Ref]
		if !ok {
			continue
		}
		for _, toRef := range d.DependsOn {
			toPURL, ok := refMap[toRef]
			if !ok {
				continue
			}
			adj[fromPURL] = append(adj[fromPURL], toPURL)
		}
	}
	return adj
}
