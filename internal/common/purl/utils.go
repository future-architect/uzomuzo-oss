package purl

import (
	"github.com/package-url/packageurl-go"
)

// WithVersion returns a PURL string where the version component is replaced with newVersion.
// It preserves namespace, name, qualifiers, and subpath from the original PURL.
//
// Args:
//   - basePURL: original PURL string (with or without version)
//   - newVersion: version string to set (e.g., "4.18.2", "v1.8.0")
//
// Returns:
//   - string: rebuilt PURL containing the new version
//   - error: parse errors wrapped with context
func WithVersion(basePURL, newVersion string) (string, error) {
	p, err := packageurl.FromString(basePURL)
	if err != nil {
		return "", NewParseError("failed to parse PURL for version update", basePURL)
	}
	p.Version = newVersion
	return p.ToString(), nil
}
