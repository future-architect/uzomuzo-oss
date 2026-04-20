package purl

import "github.com/package-url/packageurl-go"

// HasVersion reports whether the PURL string contains a non-empty version component.
// Unlike strings.Contains(p, "@"), this correctly handles namespaced PURLs
// such as pkg:npm/@scope/name where "@" appears in the namespace, not as a
// version delimiter.
func HasVersion(p string) bool {
	parsed, err := packageurl.FromString(p)
	if err != nil {
		return false
	}
	return parsed.Version != ""
}
