// Package links provides helpers to build canonical package & version specific URLs.
// DDD Layer: Infrastructure (derives external URLs; domain holds only data)
package links

import (
	"fmt"
	"strings"
)

// BuildPackageRegistryURL returns the ecosystem's canonical registry landing page (package-wide).
// Expects unscoped/normalized names where possible. Namespace/group should be included in 'name'
// for ecosystems that require it (e.g., packagist vendor/package, maven group:artifact).
func BuildPackageRegistryURL(ecosystem, name string) string {
	switch strings.ToLower(strings.TrimSpace(ecosystem)) {
	case "npm":
		return fmt.Sprintf("https://www.npmjs.com/package/%s", name)
	case "pypi":
		return fmt.Sprintf("https://pypi.org/project/%s/", name)
	case "rubygems", "gem":
		return fmt.Sprintf("https://rubygems.org/gems/%s", name)
	case "packagist", "composer":
		return fmt.Sprintf("https://packagist.org/packages/%s", name)
	case "golang":
		return fmt.Sprintf("https://pkg.go.dev/%s", name)
	case "maven":
		parts := strings.Split(name, ":")
		if len(parts) == 2 {
			return fmt.Sprintf("https://central.sonatype.com/artifact/%s/%s", parts[0], parts[1])
		}
	case "cargo":
		return fmt.Sprintf("https://crates.io/crates/%s", name)
	case "nuget":
		return fmt.Sprintf("https://www.nuget.org/packages/%s", name)
	}
	return ""
}

// BuildVersionRegistryURL returns a version-specific registry URL (if ecosystem supports one).
func BuildVersionRegistryURL(ecosystem, name, version string) string {
	if version == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(ecosystem)) {
	case "npm":
		return fmt.Sprintf("https://www.npmjs.com/package/%s/v/%s", name, version)
	case "pypi":
		return fmt.Sprintf("https://pypi.org/project/%s/%s/", name, version)
	case "rubygems", "gem":
		return fmt.Sprintf("https://rubygems.org/gems/%s/versions/%s", name, version)
	case "cargo":
		return fmt.Sprintf("https://crates.io/crates/%s/%s", name, version)
	case "nuget":
		return fmt.Sprintf("https://www.nuget.org/packages/%s/%s", name, version)
	case "maven":
		parts := strings.Split(name, ":")
		if len(parts) == 2 {
			return fmt.Sprintf("https://central.sonatype.com/artifact/%s/%s/%s", parts[0], parts[1], version)
		}
	case "golang":
		return fmt.Sprintf("https://pkg.go.dev/%s@%s", name, version)
	}
	return ""
}
