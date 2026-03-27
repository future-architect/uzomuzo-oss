// Package depparser defines the domain contract for dependency file parsing.
// Implementations (CycloneDX SBOM, go.mod, etc.) live in the Infrastructure layer.
package depparser

import "context"

// ParsedDependency represents a single dependency extracted from an SBOM or lockfile.
// This is a Value Object — immutable after creation, defined by its attributes.
type ParsedDependency struct {
	// PURL is the Package URL identifier (e.g., "pkg:npm/express@4.18.2").
	PURL string
	// Ecosystem is the package ecosystem (e.g., "npm", "golang", "maven").
	Ecosystem string
	// Name is the package name (may include namespace for Maven/Go).
	Name string
	// Version is the package version string.
	Version string
}

// DependencyParser extracts dependencies from raw input data.
// Implementations live in the Infrastructure layer.
// The context parameter supports future parsers that may require network access
// (e.g., `go list -m -json all`). In-memory parsers may ignore it.
type DependencyParser interface {
	// Parse reads raw input and returns extracted dependencies.
	// The input format depends on the implementation (SBOM JSON, go.mod text, etc.).
	Parse(ctx context.Context, data []byte) ([]ParsedDependency, error)
	// FormatName returns a human-readable name for the parser (e.g., "CycloneDX SBOM", "go.mod").
	FormatName() string
}
