// Package purl provides unified PURL (Package URL) parsing functionality
// This eliminates code duplication across the infrastructure layer
package purl

import (
	"net/url"
	"strings"

	"github.com/package-url/packageurl-go"
)

// Parser provides unified PURL parsing functionality using standard packageurl-go library
type Parser struct{}

// NewParser creates a new PURL parser instance
func NewParser() *Parser {
	return &Parser{}
}

// ParsedPURL represents the components of a parsed PURL using the standard library
type ParsedPURL struct {
	// packageURL is the underlying packageurl-go instance
	packageURL packageurl.PackageURL
	// Raw is the original PURL string
	Raw string
}

// Parse parses a PURL string into its components using the standard packageurl-go library
func (p *Parser) Parse(purl string) (*ParsedPURL, error) {
	packageURL, err := packageurl.FromString(purl)
	if err != nil {
		return &ParsedPURL{
			Raw: purl,
		}, NewParseError("failed to parse PURL", purl)
	}

	return &ParsedPURL{
		packageURL: packageURL,
		Raw:        purl,
	}, nil
}

// GetEcosystem returns the ecosystem (type) component
func (p *ParsedPURL) GetEcosystem() string {
	return p.packageURL.Type
}

// GetPackageName returns the package name with URL encoding if needed (for API compatibility)
func (p *ParsedPURL) GetPackageName() string {
	// For Golang packages, include the namespace in the package name
	if p.packageURL.Type == "golang" && p.packageURL.Namespace != "" {
		fullName := p.packageURL.Namespace + "/" + p.packageURL.Name
		return url.QueryEscape(fullName)
	}

	// Apply URL encoding for package names containing special characters
	// This maintains compatibility with the original depsdev implementation
	if strings.Contains(p.packageURL.Name, ":") {
		return url.QueryEscape(p.packageURL.Name)
	}
	return p.packageURL.Name
}

// Namespace returns the package namespace
func (p *ParsedPURL) Namespace() string {
	// For Golang packages, return empty since the full path is in Name()
	if p.packageURL.Type == "golang" {
		return ""
	}
	return p.packageURL.Namespace
}

// Name returns the package name
func (p *ParsedPURL) Name() string {
	// For Golang packages, return the full path (namespace + name)
	if p.packageURL.Type == "golang" && p.packageURL.Namespace != "" {
		return p.packageURL.Namespace + "/" + p.packageURL.Name
	}
	return p.packageURL.Name
}

// Version returns the package version
func (p *ParsedPURL) Version() string {
	return p.packageURL.Version
}

// IsStableVersion determines whether a version is stable
func IsStableVersion(version string) bool {
	if version == "" {
		return false
	}

	version = strings.ToLower(version)
	unstableKeywords := []string{"alpha", "beta", "rc", "dev", "snapshot", "pre", "preview"}

	for _, keyword := range unstableKeywords {
		if strings.Contains(version, keyword) {
			return false
		}
	}

	return true
}

// ParseError represents a PURL parsing error
type ParseError struct {
	Message string
	PURL    string
}

// NewParseError creates a new parse error
func NewParseError(message, purl string) *ParseError {
	return &ParseError{
		Message: message,
		PURL:    purl,
	}
}

// Error implements the error interface
func (e *ParseError) Error() string {
	return e.Message + ": " + e.PURL
}
