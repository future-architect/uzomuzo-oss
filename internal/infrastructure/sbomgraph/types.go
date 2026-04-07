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
	Name       string      `json:"name"`
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
//
// When the root component has an entry in the dependencies array, its dependsOn
// list is used directly. Otherwise (common with syft directory scans where the
// root is a "file" type without a PURL), direct deps are inferred as components
// that appear as dependency refs but are never listed in any dependsOn array.
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

	// Build a dependency lookup for traversal.
	depIndex := make(map[string][]string, len(bom.Dependencies))
	for _, d := range bom.Dependencies {
		depIndex[d.Ref] = d.DependsOn
	}

	// Identify PURLs that represent the project itself (not external deps).
	selfPURLs := make(map[string]struct{})
	if bom.Metadata.Component.PURL != "" {
		if p := NormalizePURL(bom.Metadata.Component.PURL); p != "" {
			selfPURLs[p] = struct{}{}
		}
	}

	// Try explicit root entry first.
	if rootDeps, ok := depIndex[rootRef]; ok {
		directPURLs := resolveDirectRefs(rootDeps, refMap, depIndex, selfPURLs)
		if len(directPURLs) == 0 {
			return nil
		}
		return directPURLs
	}

	// Root not in dependencies array — infer direct deps as refs that are never
	// listed in any dependsOn (i.e., no other component depends on them).
	slog.Debug("root component not in dependencies array, inferring direct deps", "ref", rootRef)
	dependedOn := make(map[string]struct{})
	for _, d := range bom.Dependencies {
		for _, ref := range d.DependsOn {
			dependedOn[ref] = struct{}{}
		}
	}
	directPURLs := make(map[string]struct{})
	for _, d := range bom.Dependencies {
		if _, isDep := dependedOn[d.Ref]; !isDep {
			if purl, ok := refMap[d.Ref]; ok {
				directPURLs[purl] = struct{}{}
			}
		}
	}
	return directPURLs
}

// resolveDirectRefs resolves a list of dependency refs to normalized PURLs,
// walking through "transparent" refs that represent intermediate nodes rather
// than actual dependencies. A ref is transparent if:
//   - It has no PURL (e.g., Trivy's go.mod file nodes with UUID bom-refs)
//   - Its PURL matches the project's own module (selfPURLs)
//   - It is the sole child of a no-PURL parent AND has its own children
//     (heuristic for Trivy's module-self pattern where root has no PURL)
//
// This handles Trivy's Go SBOM structure:
//
//	root (UUID) → go.mod (no PURL) → module-self (own PURL, has children) → actual deps
func resolveDirectRefs(refs []string, refMap map[string]string, depIndex map[string][]string, selfPURLs map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{})
	var walk func(ref string, parentWasTransparent bool)
	walk = func(ref string, parentWasTransparent bool) {
		purl, hasPURL := refMap[ref]
		children := depIndex[ref]

		if hasPURL {
			if _, isSelf := selfPURLs[purl]; isSelf {
				// Explicitly known self module — walk through.
				for _, child := range children {
					walk(child, true)
				}
				return
			}
			// If parent was a transparent node (no PURL) and this node is the
			// sole child with its own children, it's likely the module-self
			// (e.g., Trivy: go.mod → github.com/foo/bar → actual deps).
			if parentWasTransparent && len(children) > 0 {
				slog.Debug("treating as module-self (sole child of transparent parent with own children)", "purl", purl)
				for _, child := range children {
					walk(child, false)
				}
				return
			}
			// Real dependency — collect it.
			result[purl] = struct{}{}
			return
		}
		// No PURL — transparent node, walk children.
		for _, child := range children {
			walk(child, true)
		}
	}
	for _, ref := range refs {
		walk(ref, false)
	}
	return result
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
