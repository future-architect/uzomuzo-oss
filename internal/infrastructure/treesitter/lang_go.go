//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

// registerGoConfig registers the Go language configuration on the Analyzer.
func registerGoConfig(a *Analyzer) {
	a.configs[langGo] = &langConfig{
		language:    golang.GetLanguage(),
		importQuery: `(import_spec path: (interpreted_string_literal) @import)`,
		callQuery: strings.Join([]string{
			`(selector_expression operand: (identifier) @pkg field: (field_identifier) @field)`,
			`(qualified_type package: (package_identifier) @pkg name: (type_identifier) @field)`,
		}, "\n"),
		stripQuotes:  true,
		aliasFromPkg: goAliasFromImportPath,
	}
	compileQueries(a.configs[langGo])
}

// goAliasFromImportPath derives the default Go package alias from an import path.
// It applies Go-specific heuristics: major-version suffixes, gopkg.in version
// suffixes, ".go" module name suffixes, and hyphenated directory names.
func goAliasFromImportPath(importPath string) string {
	parts := strings.Split(importPath, "/")
	alias := parts[len(parts)-1]

	// Handle major-version suffixes (e.g., "example.com/foo/v2" → "foo").
	if len(parts) >= 2 && len(alias) >= 2 && alias[0] == 'v' && alias[1] >= '0' && alias[1] <= '9' {
		alias = parts[len(parts)-2]
	}

	// Handle gopkg.in version suffixes (e.g., "yaml.v3" → "yaml").
	if idx := strings.LastIndex(alias, ".v"); idx > 0 && idx+2 < len(alias) && alias[idx+2] >= '0' && alias[idx+2] <= '9' {
		alias = alias[:idx]
	}

	// Handle .go suffix in module names (e.g., "miscreant.go" → "miscreant").
	// Some Go modules use ".go" as a suffix in their repository/module name,
	// but the actual Go package name omits the suffix.
	if trimmed := strings.TrimSuffix(alias, ".go"); trimmed != "" && trimmed != alias {
		alias = trimmed
	}

	// Handle hyphenated package names. Go identifiers cannot contain hyphens,
	// so the actual package name differs from the directory name.
	alias = goPackageFromHyphenated(alias)

	return alias
}

// handleGoImport processes a Go import spec node.
func (a *Analyzer) handleGoImport(
	node *sitter.Node,
	src []byte,
	importPath string,
	importToPURL map[string][]string,
	aliasMap map[string][]string,
) {
	// Lowercase the import path for matching — importToPURL keys are already lowercased
	// to handle PURL case-insensitivity (see AnalyzeCoupling).
	lowerPath := strings.ToLower(importPath)
	purls, ok := importToPURL[lowerPath]
	if !ok {
		// Also try prefix matching for subpackages.
		// Pick the longest matching prefix to handle nested modules
		// (e.g., prefer "example.com/foo/bar" over "example.com/foo").
		bestLen := 0
		for ip, p := range importToPURL {
			if (strings.HasPrefix(lowerPath, ip+"/") || lowerPath == ip) && len(ip) > bestLen {
				purls = p
				ok = true
				bestLen = len(ip)
			}
		}
	}
	if !ok {
		return
	}

	// Check for explicit alias: parent node is import_spec with a name child.
	parent := node.Parent()
	alias := ""
	if parent != nil && parent.Type() == "import_spec" {
		nameNode := parent.ChildByFieldName("name")
		if nameNode != nil {
			alias = nameNode.Content(src)
		}
	}

	// Note: Blank imports (import _ "pkg") are side-effect-only (init registration).
	// Record the import file so the dep is not misclassified as "unused", but skip call-site counting.
	// Use a unique key per import path so blank imports of different packages in one file are preserved.
	if alias == "_" {
		key := blankImportAlias + importPath
		aliasMap[key] = appendUniquePURLs(aliasMap[key], purls)
		return
	}

	// Special case: Dot imports (import . "pkg") make symbols callable without a package prefix.
	// Selector-expression-based tracking cannot attribute those call sites, so mark them as used but uncountable.
	// Use a unique key per import path so multiple dot imports in one file are all preserved.
	if alias == "." {
		key := dotImportAlias + importPath
		aliasMap[key] = appendUniquePURLs(aliasMap[key], purls)
		return
	}

	if alias == "" {
		alias = goAliasFromImportPath(importPath)
	}

	aliasMap[alias] = appendUniquePURLs(aliasMap[alias], purls)
}

// goPackageFromHyphenated derives the Go package name from a hyphenated directory name.
// Go identifiers cannot contain hyphens, so directory names like "opentracing-go",
// "go-loser", or "mmap-go" map to package names "opentracing", "loser", "mmap".
//
// Heuristics (applied in order, short-circuiting when no hyphens remain):
//  1. Strip "-golang" suffix (e.g., "geoip2-golang" → "geoip2")
//  2. Strip "-go" suffix (e.g., "opentracing-go" → "opentracing", "mmap-go" → "mmap")
//  3. Strip compound suffixes "-sdk", "-api", "-client", "-lib"
//     (e.g., "onepassword-sdk-go" → "onepassword" after step 2 yields "onepassword-sdk")
//  4. Strip "go-" prefix (e.g., "go-loser" → "loser", "go-spew" → "spew")
//     Only reached if hyphens remain after steps 1-3.
//  5. Remove remaining hyphens (e.g., "some-pkg" → "somepkg")
//     Only reached if hyphens remain after steps 1-4.
//
// If the input contains no hyphens, it is returned unchanged.
// If a step produces an empty string (e.g., input is "-go"), the original name
// is returned to avoid creating an invalid alias.
func goPackageFromHyphenated(name string) string {
	if !strings.Contains(name, "-") {
		return name
	}

	// Strip "-golang" suffix first (most specific).
	result := strings.TrimSuffix(name, "-golang")
	if result == "" {
		return name
	}
	if !strings.Contains(result, "-") {
		return result
	}

	// Strip "-go" suffix, including compound suffixes like "-sdk-go"
	// (e.g., "opentracing-go" → "opentracing", "onepassword-sdk-go" → "onepassword").
	trimmed := strings.TrimSuffix(result, "-go")
	if trimmed == "" {
		return name
	}
	result = trimmed
	if !strings.Contains(result, "-") {
		return result
	}

	// After stripping "-go", try removing common compound suffixes
	// (e.g., "onepassword-sdk" → "onepassword").
	for _, suffix := range []string{"-sdk", "-api", "-client", "-lib"} {
		if t := strings.TrimSuffix(result, suffix); t != "" && t != result {
			result = t
			if !strings.Contains(result, "-") {
				return result
			}
		}
	}

	// Strip "go-" prefix (only reached if hyphens remain after steps above).
	trimmed = strings.TrimPrefix(result, "go-")
	if trimmed == "" {
		return name
	}
	result = trimmed
	if !strings.Contains(result, "-") {
		return result
	}

	// Remove remaining hyphens as a fallback.
	return strings.ReplaceAll(result, "-", "")
}
