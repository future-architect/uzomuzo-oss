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

// BuildDepsDevURL returns the deps.dev package overview page URL (no version).
// Used in the Links section as a portal to the package summary.
func BuildDepsDevURL(ecosystem, name string) string {
	eco := strings.ToLower(strings.TrimSpace(ecosystem))
	if eco == "" || name == "" {
		return ""
	}
	return fmt.Sprintf("https://deps.dev/%s/%s", eco, name)
}

// BuildDepsDevVersionURL returns the deps.dev version-specific page URL.
// Used in advisory truncation to link to the full advisory list.
func BuildDepsDevVersionURL(ecosystem, name, version string) string {
	eco := strings.ToLower(strings.TrimSpace(ecosystem))
	if eco == "" || name == "" || version == "" {
		return ""
	}
	return fmt.Sprintf("https://deps.dev/%s/%s/%s", eco, name, version)
}

// BuildGitHubReleaseNotesURL attempts to build a GitHub release/tag URL if repoURL is a GitHub repo.
// version may or may not have a leading 'v'. We generate two candidate forms, preferring exact match semantics
// left to the caller if they wish to probe. Here we simply return one heuristic.
func BuildGitHubReleaseNotesURL(repoURL, version string) string {
	repoURL = strings.TrimSuffix(repoURL, "/")
	if !strings.Contains(repoURL, "github.com/") || version == "" {
		return ""
	}
	// Heuristic: prefer tag with same version; consumer could later enhance by probing with/without 'v'.
	return fmt.Sprintf("%s/releases/tag/%s", repoURL, version)
}
