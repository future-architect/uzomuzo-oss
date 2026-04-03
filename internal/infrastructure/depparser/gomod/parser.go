// Package gomod implements a go.mod parser for dependency extraction.
// This is a convenience fallback for users who don't have an SBOM tool installed.
//
// DDD Layer: Infrastructure (external format parsing)
package gomod

import (
	"context"
	"fmt"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"golang.org/x/mod/modfile"
)

// Parser implements depparser.DependencyParser for go.mod files.
type Parser struct{}

// FormatName returns the parser's display name.
func (p *Parser) FormatName() string { return "go.mod" }

// Parse reads a go.mod file and returns direct dependencies as PURLs.
// Indirect dependencies are skipped by default.
// Replace directives are applied: if a module has a replacement, the
// replacement path and version are used instead.
func (p *Parser) Parse(_ context.Context, data []byte) ([]depparser.ParsedDependency, error) {
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse go.mod: %w", err)
	}

	// Build replace map: old module path -> replacement
	replaces := make(map[string]*modfile.Replace, len(f.Replace))
	for _, rep := range f.Replace {
		replaces[rep.Old.Path] = rep
	}

	var deps []depparser.ParsedDependency
	for _, req := range f.Require {
		if req.Indirect {
			continue
		}

		modPath := req.Mod.Path
		version := req.Mod.Version

		// Apply replace directive if present.
		// Skip local-path replacements (empty version or relative path) as they
		// produce invalid PURLs like "pkg:golang/../foo@".
		if rep, ok := replaces[modPath]; ok {
			if rep.New.Version != "" && !strings.HasPrefix(rep.New.Path, ".") && !strings.HasPrefix(rep.New.Path, "/") {
				modPath = rep.New.Path
				version = rep.New.Version
			}
			// Local-path replacement: keep original module path and version
		}

		deps = append(deps, depparser.ParsedDependency{
			PURL:      fmt.Sprintf("pkg:golang/%s@%s", modPath, version),
			Ecosystem: "golang",
			Name:      modPath,
			Version:   version,
			Relation:  depparser.RelationDirect,
		})
	}

	return deps, nil
}
