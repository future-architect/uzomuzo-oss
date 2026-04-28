// Package links provides shared URL builders for external package registries.
// DDD Layer: Common (shared by Infrastructure and Interfaces)
package links

import (
	"errors"
	"net/url"
	"strings"
)

// ErrUnsupportedEcosystem is the sentinel callers wrap when [EncodeDepsDevPath]
// rejects an ecosystem outside deps.dev's documented allowlist (Go, RubyGems,
// npm, Cargo, Maven, PyPI, NuGet — see https://docs.deps.dev/api/v3/). Use
// errors.Is to detect it and skip the request, since deps.dev would otherwise
// 404 for these inputs.
var ErrUnsupportedEcosystem = errors.New("ecosystem not supported by deps.dev")

// EncodeDepsDevPath returns the deps.dev system identifier and a path-escaped
// single-segment package name for `<system>/<encoded>`-style URLs and API
// paths. Both values are "" when the ecosystem is outside deps.dev's
// allowlist or when name is empty; callers that want a typed error should
// wrap [ErrUnsupportedEcosystem] as the cause via
// `fmt.Errorf("%w: ...", ErrUnsupportedEcosystem, ...)` (see
// `internal/infrastructure/depsdev/normalize.go` for an example adapter).
//
// `name` MUST be the canonical, unescaped package identifier for the
// ecosystem. Use [JoinMavenName] / [JoinNpmName] to build it from PURL
// components:
//   - Go modules: full module path, e.g. "github.com/gorilla/mux"
//   - npm scoped: "@scope/name" (use [JoinNpmName])
//   - Maven:      "groupId:artifactId" (use [JoinMavenName])
//
// Internally this uses [url.PathEscape] so multi-segment names collapse into
// a single URL path segment (deps.dev's React Router pattern matches `:name`
// against `[^/]+` only) and so spaces encode as %20 rather than `+`.
func EncodeDepsDevPath(ecosystem, name string) (system, encoded string) {
	sys := normalizeDepsDevEcosystem(ecosystem)
	if sys == "" || name == "" {
		return "", ""
	}
	return sys, url.PathEscape(name)
}

// JoinMavenName builds the canonical Maven coordinate `groupId:artifactId`
// that deps.dev expects as the package name. Returns just `artifactId` when
// group is empty, and "" when artifact is empty. Group and artifact are
// trimmed of surrounding whitespace.
func JoinMavenName(group, artifact string) string {
	artifact = strings.TrimSpace(artifact)
	if artifact == "" {
		return ""
	}
	group = strings.TrimSpace(group)
	if group == "" {
		return artifact
	}
	return group + ":" + artifact
}

// JoinNpmName builds the canonical npm package name "@scope/name" (or just
// "name" when scope is empty or made up only of "@" characters). The scope
// may be passed with or without the leading "@" — leading "@" runs are
// collapsed to exactly one, so callers passing PURL namespaces verbatim
// (which always include "@") and callers passing bare scope names both
// produce the same canonical form. Returns "" when name is empty.
func JoinNpmName(scope, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	// Normalize to exactly one leading "@". Handles "scope" → "@scope",
	// "@scope" → "@scope", and accidental "@@scope" / "@" → "@scope" / "".
	scope = "@" + strings.TrimLeft(strings.TrimSpace(scope), "@")
	if scope == "@" {
		return name
	}
	return scope + "/" + name
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
	system, encoded := EncodeDepsDevPath(ecosystem, name)
	if system == "" {
		return ""
	}
	return "https://deps.dev/" + system + "/" + encoded
}

// BuildDepsDevVersionURL returns the deps.dev version-specific page URL.
// Same `name` conventions and unsupported-ecosystem handling as
// [BuildDepsDevURL]; additionally returns "" when version is empty.
func BuildDepsDevVersionURL(ecosystem, name, version string) string {
	if version == "" {
		return ""
	}
	system, encoded := EncodeDepsDevPath(ecosystem, name)
	if system == "" {
		return ""
	}
	return "https://deps.dev/" + system + "/" + encoded + "/" + url.PathEscape(version)
}

// normalizeDepsDevEcosystem maps a PURL ecosystem name to deps.dev's system
// name, returning "" for ecosystems deps.dev does not host. The supported
// list (Go, RubyGems, npm, Cargo, Maven, PyPI, NuGet) is documented at
// https://docs.deps.dev/api/v3/.
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
