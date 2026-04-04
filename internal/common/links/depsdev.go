// Package links provides shared URL builders for external package registries.
// DDD Layer: Common (shared by Infrastructure and Interfaces)
package links

import (
	"fmt"
	"strings"
)

// normalizeDepsDevEcosystem maps PURL ecosystem names to deps.dev system names.
// See internal/infrastructure/depsdev/normalize.go for the canonical mapping.
func normalizeDepsDevEcosystem(ecosystem string) string {
	eco := strings.ToLower(strings.TrimSpace(ecosystem))
	switch eco {
	case "golang":
		return "go"
	case "gem":
		return "rubygems"
	case "composer":
		return "packagist"
	default:
		return eco
	}
}

// BuildDepsDevURL returns the deps.dev package overview page URL (no version).
func BuildDepsDevURL(ecosystem, name string) string {
	eco := normalizeDepsDevEcosystem(ecosystem)
	if eco == "" || name == "" {
		return ""
	}
	return fmt.Sprintf("https://deps.dev/%s/%s", eco, name)
}

// BuildDepsDevVersionURL returns the deps.dev version-specific page URL.
func BuildDepsDevVersionURL(ecosystem, name, version string) string {
	eco := normalizeDepsDevEcosystem(ecosystem)
	if eco == "" || name == "" || version == "" {
		return ""
	}
	return fmt.Sprintf("https://deps.dev/%s/%s/%s", eco, name, version)
}
