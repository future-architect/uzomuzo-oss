// Package links provides shared URL builders for external package registries.
// DDD Layer: Common (shared by Infrastructure and Interfaces)
package links

import (
	"net/url"
	"strings"
)

// normalizeDepsDevEcosystem maps a PURL ecosystem name to deps.dev's system
// name, returning "" for ecosystems deps.dev does not host. The supported
// list (Go, RubyGems, npm, Cargo, Maven, PyPI, NuGet) is documented at
// https://docs.deps.dev/api/v3/.
//
// Note: this is the user-facing URL allowlist. The API client in
// internal/infrastructure/depsdev/normalize.go currently keeps a wider
// mapping (composer→packagist) that produces 404s the client handles
// gracefully; consolidation is tracked as follow-up work.
func normalizeDepsDevEcosystem(ecosystem string) string {
	eco := strings.ToLower(strings.TrimSpace(ecosystem))
	switch eco {
	case "go", "golang":
		return "go"
	case "rubygems", "gem":
		return "rubygems"
	case "npm", "cargo", "maven", "pypi", "nuget":
		return eco
	default:
		return ""
	}
}

// BuildDepsDevURL returns the deps.dev package overview page URL.
//
// `name` must already be the canonical single-segment package identifier
// for the ecosystem (`groupId:artifactId` for Maven, `<scope>/<name>` for
// npm scoped packages, full module path for Go modules); slashes inside
// `name` are percent-encoded so the URL survives deps.dev's React Router
// pattern `/:system/:name/:version?`, which matches `:name` against `[^/]+`
// only.
//
// Returns "" for an empty ecosystem/name or an ecosystem deps.dev does not
// host (e.g., Composer / Hex / Swift) so callers can skip rendering.
func BuildDepsDevURL(ecosystem, name string) string {
	eco := normalizeDepsDevEcosystem(ecosystem)
	if eco == "" || name == "" {
		return ""
	}
	return "https://deps.dev/" + eco + "/" + url.PathEscape(name)
}

// BuildDepsDevVersionURL returns the deps.dev version-specific page URL.
// Same `name` conventions and unsupported-ecosystem handling as
// [BuildDepsDevURL]; additionally returns "" when version is empty.
func BuildDepsDevVersionURL(ecosystem, name, version string) string {
	eco := normalizeDepsDevEcosystem(ecosystem)
	if eco == "" || name == "" || version == "" {
		return ""
	}
	return "https://deps.dev/" + eco + "/" + url.PathEscape(name) + "/" + url.PathEscape(version)
}
