// Package purl provides type definitions for PURL parsing
package purl

import (
	"strings"
)

// SupportedEcosystems returns a list of supported package ecosystems
func SupportedEcosystems() []string {
	return []string{
		"npm",
		"pypi",
		"maven",
		"nuget",
		"cargo",
		"golang",
		"gem",
		"composer",
		"github",
	}
}

// IsEcosystemSupported checks if an ecosystem is supported
func IsEcosystemSupported(ecosystem string) bool {
	supported := SupportedEcosystems()
	for _, e := range supported {
		if e == ecosystem {
			return true
		}
	}
	return false
}

// PackageManagerMapping provides mapping from GitHub package managers to PURL ecosystems
var PackageManagerMapping = map[string]string{
	"NPM":      "npm",
	"PIP":      "pypi",
	"PYPI":     "pypi",
	"MAVEN":    "maven",
	"NUGET":    "nuget",
	"CARGO":    "cargo",
	"RUST":     "cargo",
	"GO":       "golang",
	"GOLANG":   "golang",
	"RUBYGEMS": "gem",
	"GEM":      "gem",
	"GITHUB":   "github",
}

// MapPackageManagerToEcosystem converts GitHub package manager names to PURL ecosystems
func MapPackageManagerToEcosystem(packageManager string) string {
	if ecosystem, exists := PackageManagerMapping[strings.ToUpper(packageManager)]; exists {
		return ecosystem
	}

	// Fallback to lowercase if not in mapping
	ecosystem := strings.ToLower(packageManager)
	if IsEcosystemSupported(ecosystem) {
		return ecosystem
	}

	return "" // Unsupported ecosystem
}
