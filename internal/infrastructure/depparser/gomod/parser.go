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

	// Build tool set: require module paths that correspond to tool directives
	// (Go 1.24+). Tool directives reference package import paths (e.g.,
	// github.com/foo/bar/cmd/baz) while require entries use module paths
	// (e.g., github.com/foo/bar). We match by checking if the tool path
	// equals or is a sub-path of the require module path.
	toolPaths := resolveToolModulePaths(f.Tool, f.Require)

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

		// Mark dependencies that correspond to tool directives.
		// Tool deps are dev/CI executables (e.g., linters, code generators)
		// that appear in the require block but are not imported in source code.
		var scope string
		if _, ok := toolPaths[req.Mod.Path]; ok {
			scope = "tool"
		}

		deps = append(deps, depparser.ParsedDependency{
			PURL:      fmt.Sprintf("pkg:golang/%s@%s", modPath, version),
			Ecosystem: "golang",
			Name:      modPath,
			Version:   version,
			Relation:  depparser.RelationDirect,
			Scope:     scope,
		})
	}

	return deps, nil
}

// ParseToolPaths extracts the require module paths that correspond to tool
// directives from a go.mod file. Tool directives reference package import
// paths (e.g., github.com/foo/bar/cmd/baz) while require entries use module
// paths (e.g., github.com/foo/bar). This function resolves tool paths to
// their matching require module paths using longest-prefix matching.
// Returns an empty set if the file has no tool directives.
// This is used by the diet pipeline to identify tool deps that intentionally
// have zero source imports.
func ParseToolPaths(data []byte) (map[string]struct{}, error) {
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse go.mod: %w", err)
	}
	return resolveToolModulePaths(f.Tool, f.Require), nil
}

// resolveToolModulePaths resolves tool directive package paths to their
// corresponding require module paths. A tool path matches a require module
// path if the tool path equals the module path or has it as a prefix with
// a "/" boundary. When multiple require modules match, the longest module
// path wins.
func resolveToolModulePaths(tools []*modfile.Tool, requires []*modfile.Require) map[string]struct{} {
	if len(tools) == 0 {
		return make(map[string]struct{})
	}
	result := make(map[string]struct{}, len(tools))
	for _, t := range tools {
		bestModule := ""
		for _, req := range requires {
			modPath := req.Mod.Path
			if t.Path == modPath || strings.HasPrefix(t.Path, modPath+"/") {
				if len(modPath) > len(bestModule) {
					bestModule = modPath
				}
			}
		}
		if bestModule != "" {
			result[bestModule] = struct{}{}
		}
	}
	return result
}
