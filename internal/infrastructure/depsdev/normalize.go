package depsdev

import (
	"fmt"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common/links"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

// toDepsDevSystemAndName normalizes a parsed PURL into deps.dev system and
// path-escaped package name components for the
// `systems/{system}/packages/{name}` endpoint.
//
// It is a thin adapter over [links.EncodeDepsDevPath]: build the canonical
// unescaped name from PURL components (Maven `groupId:artifactId`, npm
// `@scope/name`, Go full module path) and let the shared helper handle the
// allowlist + path encoding.
//
// Errors:
//   - [links.ErrUnsupportedEcosystem] (wrapped): the PURL ecosystem is
//     outside deps.dev's documented allowlist (composer, hex, swift, …);
//     callers should treat this as a graceful skip.
//   - Plain error (no sentinel): nil PURL or empty derived package name;
//     callers should propagate as a hard error.
func toDepsDevSystemAndName(p *purl.ParsedPURL) (system, name string, err error) {
	if p == nil {
		return "", "", fmt.Errorf("toDepsDevSystemAndName: nil PURL")
	}

	eco := strings.ToLower(strings.TrimSpace(p.GetEcosystem()))

	var raw string
	switch eco {
	case "maven":
		raw = links.JoinMavenName(p.Namespace(), p.Name())
	case "npm":
		raw = links.JoinNpmName(p.Namespace(), p.Name())
	default:
		// For golang, ParsedPURL.Name() already returns the full
		// "namespace/name" path unescaped; for other ecosystems Name()
		// is the bare package name.
		raw = p.Name()
	}

	if raw == "" {
		return "", "", fmt.Errorf("toDepsDevSystemAndName: empty package name (purl=%s)", p.Raw)
	}

	sys, encoded := links.EncodeDepsDevPath(eco, raw)
	if sys == "" {
		return "", "", fmt.Errorf("%w: %q (purl=%s)", links.ErrUnsupportedEcosystem, eco, p.Raw)
	}
	return sys, encoded, nil
}
