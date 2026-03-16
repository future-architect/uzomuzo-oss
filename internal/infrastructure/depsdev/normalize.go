package depsdev

import (
	"net/url"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

// toDepsDevSystemAndName normalizes a parsed PURL into deps.dev system and package name
// components suitable for the systems/{system}/packages/{name} endpoint.
//
// Rules:
//   - system mapping:
//     golang -> go
//     gem    -> rubygems
//     others -> lowercased ecosystem
//   - name construction:
//     npm:   include scope when present ("@scope/name"), URL-escape when scoped; unscoped uses raw name
//     maven: use "groupId:artifactId" URL-escaped (group may be empty)
//     golang: use full path (namespace/name) URL-escaped via ParsedPURL.GetPackageName()
//     others: use ParsedPURL.GetPackageName()
func toDepsDevSystemAndName(p *purl.ParsedPURL) (system string, name string) {
	ec := strings.ToLower(strings.TrimSpace(p.GetEcosystem()))
	switch ec {
	case "golang":
		system = "go"
		// GetPackageName() already returns URL-escaped namespace/name for golang
		name = p.GetPackageName()
		return
	case "gem":
		system = "rubygems"
	case "composer":
		// deps.dev uses "packagist" for composer ecosystem
		system = "packagist"
	default:
		system = ec
	}

	switch ec {
	case "npm":
		if ns := strings.TrimSpace(p.Namespace()); ns != "" {
			name = url.QueryEscape(ns + "/" + p.Name())
		} else {
			name = p.Name()
		}
	case "maven":
		if g := strings.TrimSpace(p.Namespace()); g != "" {
			name = url.QueryEscape(g + ":" + p.Name())
		} else {
			name = url.QueryEscape(p.Name())
		}
	default:
		name = p.GetPackageName()
	}
	return
}
