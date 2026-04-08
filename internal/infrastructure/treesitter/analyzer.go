//go:build cgo

// Package treesitter implements source code coupling analysis using tree-sitter.
package treesitter

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	domaindiet "github.com/future-architect/uzomuzo-oss/internal/domain/diet"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// skipDirs contains directory names that should be skipped during walking.
var skipDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	".git":         true,
	"testdata":     true,
	"__pycache__":  true,
	"build":        true,
	"dist":         true,
	"target":       true,
}

// langID identifies a programming language for parsing.
type langID int

const (
	langGo langID = iota
	langPython
	langJavaScript
	langTypeScript
	langTSX
	langJava
)

// dotImportAlias is a sentinel alias for Go dot imports (import . "pkg").
// Dot-imported symbols are called without a package prefix, so selector_expression
// queries cannot track them. We mark them as "used but uncountable."
const dotImportAlias = "\x00dot"

// blankImportAlias is a sentinel alias for Go blank imports (import _ "pkg").
// Blank imports are side-effect-only (init registration) with no callable API,
// so we record the import file but skip call-site counting.
const blankImportAlias = "\x00blank"

// wildcardImportAlias is a sentinel alias for Python wildcard imports (from x import *).
// Wildcard-imported symbols are called without a package prefix, so bare identifier
// queries cannot attribute them. We mark them as "used but uncountable."
const wildcardImportAlias = "\x00wildcard"

// langConfig holds the tree-sitter language and query patterns.
type langConfig struct {
	language       *sitter.Language
	importQuery    string
	callQuery      string
	compiledImport *sitter.Query // compiled once in NewAnalyzer
	compiledCall   *sitter.Query // compiled once in NewAnalyzer
	stripQuotes    bool
	aliasFromPkg   func(importPath string) string
}

// Analyzer implements SourceAnalyzer using tree-sitter for multi-language parsing.
type Analyzer struct {
	configs map[langID]*langConfig
}

// newJSLikeConfig creates a langConfig for JS-family languages (JS, TS, TSX).
// When includeJSX is true, the call query also matches JSX element syntax
// (e.g., <Camera /> and <Icon size={24}>) so that component usage is counted
// as call sites. Pass true for languages whose grammar supports JSX nodes
// (JavaScript and TSX).
func newJSLikeConfig(lang *sitter.Language, includeJSX bool) *langConfig {
	importQ := strings.Join([]string{
		`(import_statement source: (string) @import)`,
		`(call_expression function: (identifier) @func (#eq? @func "require") arguments: (arguments (string) @import))`,
	}, "\n")
	callPatterns := []string{
		`(member_expression object: (identifier) @obj property: (property_identifier) @prop)`,
		`(call_expression function: (identifier) @func)`,
	}
	if includeJSX {
		callPatterns = append(callPatterns,
			`(jsx_self_closing_element name: (identifier) @func)`,
			`(jsx_opening_element name: (identifier) @func)`,
		)
	}
	callQ := strings.Join(callPatterns, "\n")

	cfg := &langConfig{
		language:    lang,
		importQuery: importQ,
		callQuery:   callQ,
		stripQuotes: true,
		aliasFromPkg: func(importPath string) string {
			return importPath
		},
	}
	compileQueries(cfg)

	return cfg
}

// compileQueries compiles import and call queries for a langConfig.
// Call this after setting importQuery and callQuery.
func compileQueries(cfg *langConfig) {
	q, err := sitter.NewQuery([]byte(cfg.importQuery), cfg.language)
	if err != nil {
		slog.Warn("failed to compile import query", "error", err)
	}
	cfg.compiledImport = q

	q2, err := sitter.NewQuery([]byte(cfg.callQuery), cfg.language)
	if err != nil {
		slog.Warn("failed to compile call query", "error", err)
	}
	cfg.compiledCall = q2
}

// NewAnalyzer creates a new tree-sitter based Analyzer.
func NewAnalyzer() *Analyzer {
	a := &Analyzer{
		configs: make(map[langID]*langConfig),
	}

	a.configs[langGo] = &langConfig{
		language:    golang.GetLanguage(),
		importQuery: `(import_spec path: (interpreted_string_literal) @import)`,
		callQuery: strings.Join([]string{
			`(selector_expression operand: (identifier) @pkg field: (field_identifier) @field)`,
			`(qualified_type package: (package_identifier) @pkg name: (type_identifier) @field)`,
		}, "\n"),
		stripQuotes: true,
		aliasFromPkg: func(importPath string) string {
			parts := strings.Split(importPath, "/")
			return parts[len(parts)-1]
		},
	}
	compileQueries(a.configs[langGo])

	a.configs[langPython] = &langConfig{
		language: python.GetLanguage(),
		importQuery: strings.Join([]string{
			`(import_statement name: (dotted_name) @import)`,
			`(import_statement name: (aliased_import name: (dotted_name) @import))`,
			`(import_from_statement module_name: (dotted_name) @import)`,
		}, "\n"),
		callQuery: strings.Join([]string{
			`(attribute object: (identifier) @pkg attribute: (identifier) @attr)`,
			`(call function: (identifier) @func)`,
		}, "\n"),
		stripQuotes: false,
		aliasFromPkg: func(importPath string) string {
			parts := strings.Split(importPath, ".")
			return parts[0]
		},
	}
	compileQueries(a.configs[langPython])

	a.configs[langJavaScript] = newJSLikeConfig(javascript.GetLanguage(), true)
	a.configs[langTypeScript] = newJSLikeConfig(typescript.GetLanguage(), false)
	a.configs[langTSX] = newJSLikeConfig(tsx.GetLanguage(), true)

	a.configs[langJava] = &langConfig{
		language:    java.GetLanguage(),
		importQuery: `(import_declaration (scoped_identifier) @import)`,
		callQuery: strings.Join([]string{
			`(method_invocation object: (identifier) @obj name: (identifier) @method)`,
			`(method_invocation !object name: (identifier) @func)`,
			`(marker_annotation name: (identifier) @func)`,
			`(annotation name: (identifier) @func)`,
		}, "\n"),
		stripQuotes: false,
		aliasFromPkg: func(importPath string) string {
			return importPath
		},
	}
	compileQueries(a.configs[langJava])

	return a
}

// extToLang maps file extensions to language IDs.
func extToLang(ext string) (langID, bool) {
	switch ext {
	case ".go":
		return langGo, true
	case ".py":
		return langPython, true
	case ".js", ".jsx", ".mjs":
		return langJavaScript, true
	case ".ts":
		return langTypeScript, true
	case ".tsx":
		return langTSX, true
	case ".java":
		return langJava, true
	default:
		return 0, false
	}
}

// AnalyzeCoupling walks sourceRoot, parses source files, and returns coupling analysis per PURL.
func (a *Analyzer) AnalyzeCoupling(
	ctx context.Context,
	sourceRoot string,
	importPaths map[string][]string,
) (map[string]*domaindiet.CouplingAnalysis, error) {
	// Build reverse map: importPath -> []PURL.
	// Keys are lowercased because PURL namespace is case-insensitive (PURL spec)
	// while Go import paths are case-sensitive. SBOM generators may produce
	// lowercased PURLs (e.g., "github.com/masterminds/semver") for imports that
	// use mixed case (e.g., "github.com/Masterminds/semver").
	//
	// Multiple PURLs can map to the same import path (e.g., two versions of gson).
	// We store all of them so coupling data is attributed to every matching PURL
	// rather than silently dropping all but the last-written entry.
	importToPURL := make(map[string][]string, len(importPaths))
	for purl, paths := range importPaths {
		for _, p := range paths {
			key := strings.ToLower(p)
			importToPURL[key] = append(importToPURL[key], purl)
		}
	}
	// Sort and deduplicate each PURL slice for deterministic behavior when iterating.
	for key, purls := range importToPURL {
		slices.Sort(purls)
		importToPURL[key] = slices.Compact(purls)
	}

	accum := make(map[string]*accumulator)

	parser := sitter.NewParser()

	err := filepath.WalkDir(sourceRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		info, err := d.Info()
		if err != nil {
			return nil // skip
		}
		if info.Size() > 1<<20 { // 1 MB
			slog.Debug("skipping large file for coupling analysis", "path", path, "size", info.Size())
			return nil
		}

		ext := filepath.Ext(path)
		lid, ok := extToLang(ext)
		if !ok {
			return nil
		}

		cfg, ok := a.configs[lid]
		if !ok {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			slog.Debug("failed to read file", "path", path, "error", err)
			return nil
		}

		parser.SetLanguage(cfg.language)
		tree, err := parser.ParseCtx(ctx, nil, src)
		if err != nil {
			slog.Debug("failed to parse file", "path", path, "error", err)
			return nil
		}

		root := tree.RootNode()
		cursor := sitter.NewQueryCursor()

		// Phase 1: Extract imports and build alias->PURL map for this file.
		fileAliases := a.extractImports(cfg, root, src, importToPURL, lid, cursor)

		relPath, _ := filepath.Rel(sourceRoot, path)
		if relPath == "" {
			relPath = path
		}

		// Record import files
		for alias, purls := range fileAliases {
			for _, purl := range purls {
				acc, ok := accum[purl]
				if !ok {
					acc = &accumulator{
						importFiles: make(map[string]bool),
						symbols:     make(map[string]bool),
					}
					accum[purl] = acc
				}
				if !acc.importFiles[relPath] {
					acc.importFiles[relPath] = true
				}
				if strings.HasPrefix(alias, dotImportAlias) {
					acc.hasDotImport = true
				}
				if strings.HasPrefix(alias, blankImportAlias) {
					acc.hasBlankImport = true
				}
				if strings.HasPrefix(alias, wildcardImportAlias) {
					acc.hasWildcardImport = true
				}
			}
		}

		// Phase 2: Count call sites using alias->PURL mapping.
		a.countCallSites(cfg, root, src, fileAliases, accum, cursor)

		// Close immediately — defer in WalkDir callback accumulates across all files.
		cursor.Close()
		tree.Close()

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking source tree: %w", err)
	}

	// No coupling data collected — return nil so callers treat coupling as unavailable
	// rather than misclassifying every dependency as unused.
	if len(accum) == 0 {
		return nil, nil
	}

	// Build result map
	results := make(map[string]*domaindiet.CouplingAnalysis, len(accum))
	for purl, acc := range accum {
		files := make([]string, 0, len(acc.importFiles))
		for f := range acc.importFiles {
			files = append(files, f)
		}
		slices.Sort(files)
		callSites := acc.callSites
		isUnused := len(acc.importFiles) == 0

		// Dot imports are used but uncountable via selector queries.
		// Mark as used with a baseline call site count.
		if acc.hasDotImport {
			isUnused = false
			if callSites == 0 {
				callSites = 1
			}
		}

		symbols := make([]string, 0, len(acc.symbols))
		for s := range acc.symbols {
			symbols = append(symbols, s)
		}
		slices.Sort(symbols)

		results[purl] = &domaindiet.CouplingAnalysis{
			ImportFileCount:   len(acc.importFiles),
			CallSiteCount:     callSites,
			APIBreadth:        len(acc.symbols),
			ImportFiles:       files,
			Symbols:           symbols,
			IsUnused:          isUnused,
			HasBlankImport:    acc.hasBlankImport,
			HasDotImport:      acc.hasDotImport,
			HasWildcardImport: acc.hasWildcardImport,
		}
	}

	return results, nil
}

// extractImports finds import statements in the AST and returns alias->[]PURL mapping.
// Multiple PURLs per alias can occur when two versions of the same library are present.
func (a *Analyzer) extractImports(
	cfg *langConfig,
	root *sitter.Node,
	src []byte,
	importToPURL map[string][]string,
	lid langID,
	cursor *sitter.QueryCursor,
) map[string][]string {
	aliasMap := make(map[string][]string) // alias -> []PURL

	query := cfg.compiledImport
	if query == nil {
		slog.Debug("import query not compiled")
		return aliasMap
	}

	cursor.Exec(query, root)

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}
		// Apply tree-sitter predicates (e.g., #eq? @func "require") to filter matches.
		match = cursor.FilterPredicates(match, src)
		for _, capture := range match.Captures {
			// Only process @import captures; skip auxiliary captures like @func.
			if query.CaptureNameForId(capture.Index) != "import" {
				continue
			}
			value := capture.Node.Content(src)

			if cfg.stripQuotes {
				value = strings.Trim(value, `"'`+"`")
			}

			if value == "" {
				continue
			}

			// For Go, check for explicit alias.
			if lid == langGo {
				a.handleGoImport(capture.Node, src, value, importToPURL, aliasMap)
				continue
			}

			// For Python: match against top-level module or full dotted name.
			if lid == langPython {
				a.handlePythonImport(capture.Node, src, value, importToPURL, aliasMap, cfg)
				continue
			}

			// For Java: match as prefix of the full import path.
			if lid == langJava {
				a.handleJavaImport(capture.Node, value, importToPURL, aliasMap)
				continue
			}

			// Skip TypeScript `import type` statements (no runtime coupling).
			if lid == langTypeScript || lid == langTSX {
				if isTypeOnlyImport(capture.Node) {
					continue
				}
			}
			// For JS/TS: exact match or subpath prefix match (e.g., "lodash/fp" → "lodash").
			a.handleJSImport(capture.Node, src, value, importToPURL, aliasMap, cfg)
		}
	}

	return aliasMap
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
		// Default: last path component, with heuristics for Go conventions.
		parts := strings.Split(importPath, "/")
		alias = parts[len(parts)-1]

		// Handle major-version suffixes (e.g., "example.com/foo/v2" → "foo").
		if len(parts) >= 2 && len(alias) >= 2 && alias[0] == 'v' && alias[1] >= '0' && alias[1] <= '9' {
			alias = parts[len(parts)-2]
		}

		// Handle gopkg.in version suffixes (e.g., "yaml.v3" → "yaml").
		if idx := strings.LastIndex(alias, ".v"); idx > 0 && idx+2 < len(alias) && alias[idx+2] >= '0' && alias[idx+2] <= '9' {
			alias = alias[:idx]
		}

		// Handle hyphenated package names. Go identifiers cannot contain hyphens,
		// so the actual package name differs from the directory name.
		// Common conventions: "opentracing-go" → "opentracing", "go-loser" → "loser",
		// "go-spew" → "spew", "mmap-go" → "mmap".
		alias = goPackageFromHyphenated(alias)
	}

	aliasMap[alias] = appendUniquePURLs(aliasMap[alias], purls)
}

// goPackageFromHyphenated derives the Go package name from a hyphenated directory name.
// Go identifiers cannot contain hyphens, so directory names like "opentracing-go",
// "go-loser", or "mmap-go" map to package names "opentracing", "loser", "mmap".
//
// Heuristics (applied in order, short-circuiting when no hyphens remain):
//  1. Strip "-go" suffix (e.g., "opentracing-go" → "opentracing", "mmap-go" → "mmap")
//  2. Strip "go-" prefix (e.g., "go-loser" → "loser", "go-spew" → "spew")
//     Only reached if hyphens remain after step 1.
//  3. Remove remaining hyphens (e.g., "some-pkg" → "somepkg")
//     Only reached if hyphens remain after steps 1-2.
//
// If the input contains no hyphens, it is returned unchanged.
// If a step produces an empty string (e.g., input is "-go"), the original name
// is returned to avoid creating an invalid alias.
func goPackageFromHyphenated(name string) string {
	if !strings.Contains(name, "-") {
		return name
	}

	// Strip "-go" suffix first (more specific).
	result := strings.TrimSuffix(name, "-go")
	if result == "" {
		return name
	}
	if !strings.Contains(result, "-") {
		return result
	}

	// Strip "go-" prefix (only reached if hyphens remain after step 1).
	trimmed := strings.TrimPrefix(result, "go-")
	if trimmed == "" {
		return result
	}
	result = trimmed
	if !strings.Contains(result, "-") {
		return result
	}

	// Remove remaining hyphens as a fallback.
	return strings.ReplaceAll(result, "-", "")
}

// handlePythonImport handles matching Python imports.
// For from-imports (e.g., "from requests import get, post"), it also registers
// each imported name as an alias so that bare calls like get() are counted.
func (a *Analyzer) handlePythonImport(
	node *sitter.Node,
	src []byte,
	importPath string,
	importToPURL map[string][]string,
	aliasMap map[string][]string,
	cfg *langConfig,
) {
	// Resolve the module's PURLs via exact match, top-level name, or prefix matching.
	purls := a.resolvePythonPURLs(importPath, importToPURL)
	if len(purls) == 0 {
		return
	}

	parent := node.Parent()
	if parent == nil {
		return
	}

	switch parent.Type() {
	case "import_statement":
		// Regular import (e.g., "import requests") — register module name as alias.
		alias := cfg.aliasFromPkg(importPath)
		aliasMap[alias] = appendUniquePURLs(aliasMap[alias], purls)
	case "aliased_import":
		// Aliased import (e.g., "import requests as r") — the captured dotted_name's
		// parent is aliased_import. Register the explicit alias, not the module name.
		grandparent := parent.Parent()
		if grandparent != nil && grandparent.Type() == "import_statement" {
			aliasNode := parent.ChildByFieldName("alias")
			if aliasNode != nil {
				key := aliasNode.Content(src)
				aliasMap[key] = appendUniquePURLs(aliasMap[key], purls)
			} else {
				// Fallback: no alias found, use module name.
				alias := cfg.aliasFromPkg(importPath)
				aliasMap[alias] = appendUniquePURLs(aliasMap[alias], purls)
			}
		}
	case "import_from_statement":
		// For from-imports, register each imported name as an alias.
		// "from requests import get" does NOT bind "requests" in scope,
		// so we only register the individual imported names.
		a.registerFromImportNames(node, src, purls, aliasMap)
	}
}

// resolvePythonPURLs resolves a Python import path to its PURLs.
// Matching is case-insensitive because importToPURL keys are already lowercased.
// Multiple PURLs can be returned when different versions of the same library are present.
func (a *Analyzer) resolvePythonPURLs(
	importPath string,
	importToPURL map[string][]string,
) []string {
	lowerPath := strings.ToLower(importPath)

	// Try exact match first.
	if purls, ok := importToPURL[lowerPath]; ok {
		return purls
	}

	// Try top-level module name.
	topLevel := strings.Split(lowerPath, ".")[0]
	if purls, ok := importToPURL[topLevel]; ok {
		return purls
	}

	// Try prefix matching — pick the longest matching prefix to handle
	// overlapping package names (e.g., prefer "google.cloud" over "google").
	bestIP := ""
	var bestPURLs []string
	for ip, purls := range importToPURL {
		if (lowerPath == ip || strings.HasPrefix(lowerPath, ip+".")) && len(ip) > len(bestIP) {
			bestIP = ip
			bestPURLs = purls
		}
	}
	return bestPURLs
}

// registerFromImportNames registers imported names from "from x import y, z" statements.
// The node must be a dotted_name captured from the module_name field of an import_from_statement.
func (a *Analyzer) registerFromImportNames(
	node *sitter.Node,
	src []byte,
	purls []string,
	aliasMap map[string][]string,
) {
	parent := node.Parent()
	if parent == nil || parent.Type() != "import_from_statement" {
		return
	}

	for i := 0; i < int(parent.ChildCount()); i++ {
		child := parent.Child(i)
		switch child.Type() {
		case "dotted_name":
			// Only process named imports (field "name"), not the module_name field.
			if parent.FieldNameForChild(i) != "name" {
				continue
			}
			// from x import y → register "y" -> purls
			name := child.Content(src)
			aliasMap[name] = appendUniquePURLs(aliasMap[name], purls)
		case "aliased_import":
			// from x import y as z → register "z" -> purls
			aliasNode := child.ChildByFieldName("alias")
			if aliasNode != nil {
				key := aliasNode.Content(src)
				aliasMap[key] = appendUniquePURLs(aliasMap[key], purls)
			}
		case "wildcard_import":
			// from x import * — cannot track individual names.
			// Register a unique sentinel per PURL so ImportFileCount is correct,
			// but bare calls will be undercounted.
			for _, purl := range purls {
				key := wildcardImportAlias + purl
				aliasMap[key] = appendUniquePURLs(aliasMap[key], []string{purl})
			}
		}
	}
}

// handleJavaImport handles matching Java fully-qualified imports, including static imports.
// For static imports (import static org.junit.Assert.assertEquals), the method/field name
// is registered as the alias so bare calls like assertEquals() are matched.
// For regular imports, the class name (and its lowercase variant) are registered.
func (a *Analyzer) handleJavaImport(
	node *sitter.Node,
	importPath string,
	importToPURL map[string][]string,
	aliasMap map[string][]string,
) {
	// Lowercase the import path for matching — importToPURL keys are already lowercased
	// to handle PURL case-insensitivity (see AnalyzeCoupling).
	lowerPath := strings.ToLower(importPath)

	// Pick the longest matching prefix to handle overlapping groupIds
	// (e.g., prefer "org.apache.commons" over "org.apache").
	bestIP := ""
	var bestPURLs []string
	for ip, purls := range importToPURL {
		if (lowerPath == ip || strings.HasPrefix(lowerPath, ip+".")) && len(ip) > len(bestIP) {
			bestIP = ip
			bestPURLs = purls
		}
	}
	if bestIP == "" {
		return
	}

	// Check if this is a static import by looking for a "static" child
	// in the parent import_declaration node.
	isStatic := isJavaStaticImport(node)

	parts := strings.Split(importPath, ".")
	alias := parts[len(parts)-1]

	if isStatic {
		if alias == "*" {
			// Wildcard static import (import static org.junit.Assert.*) — cannot
			// track individual names. Register a sentinel so ImportFileCount is
			// correct, but bare calls will be undercounted.
			key := wildcardImportAlias + importPath
			aliasMap[key] = appendUniquePURLs(aliasMap[key], bestPURLs)
			return
		}
		// Static import: the last component is a method/field name (e.g., assertEquals).
		// Register it directly so bare calls like assertEquals() are matched
		// via the single-capture call query pattern.
		aliasMap[alias] = appendUniquePURLs(aliasMap[alias], bestPURLs)
		return
	}

	// Regular import: last component is a class name (e.g., Gson, StringUtils).
	aliasMap[alias] = appendUniquePURLs(aliasMap[alias], bestPURLs)
	// Also register lowercase for variable-style calls (e.g., Gson gson = ...; gson.toJson()).
	lower := strings.ToLower(alias[:1]) + alias[1:]
	if lower != alias {
		aliasMap[lower] = appendUniquePURLs(aliasMap[lower], bestPURLs)
	}
}

// isJavaStaticImport checks whether a scoped_identifier node is part of a static import.
// The AST structure is: import_declaration -> ["import", "static", scoped_identifier, ";"]
func isJavaStaticImport(node *sitter.Node) bool {
	parent := node.Parent()
	if parent == nil || parent.Type() != "import_declaration" {
		return false
	}
	for i := 0; i < int(parent.ChildCount()); i++ {
		if parent.Child(i).Type() == "static" {
			return true
		}
	}
	return false
}

// handleJSImport processes a JS/TS module specifier with subpath prefix matching.
// node is the @import capture node (the source string literal); src is the file source.
func (a *Analyzer) handleJSImport(
	node *sitter.Node,
	src []byte,
	importPath string,
	importToPURL map[string][]string,
	aliasMap map[string][]string,
	cfg *langConfig,
) {
	// Lowercase the import path for matching — importToPURL keys are already lowercased
	// to handle PURL case-insensitivity (see AnalyzeCoupling).
	lowerPath := strings.ToLower(importPath)
	purls, ok := importToPURL[lowerPath]
	if !ok {
		// Pick the longest matching prefix to handle scoped packages
		// (e.g., prefer "@scope/pkg/sub" over "@scope/pkg").
		bestLen := 0
		for ip, p := range importToPURL {
			if strings.HasPrefix(lowerPath, ip+"/") && len(ip) > bestLen {
				purls = p
				ok = true
				bestLen = len(ip)
			}
		}
	}
	if !ok {
		return
	}

	// Extract the JS/TS binding names from the AST (e.g., "cloud" from
	// `import cloud from '@strapi/plugin-cloud'`). Falls back to aliasFromPkg
	// for side-effect imports or patterns we cannot resolve.
	aliases := extractJSBindings(node, src)
	if len(aliases) == 0 {
		aliases = []string{cfg.aliasFromPkg(importPath)}
	}
	for _, alias := range aliases {
		aliasMap[alias] = appendUniquePURLs(aliasMap[alias], purls)
	}
}

// extractJSBindings walks the AST from an import source node to find all JS binding names.
//
// ESM: import_statement → import_clause → identifier / namespace_import
// CJS: string → arguments → call_expression → variable_declarator → name
//
// Combined imports (e.g., `import def, * as ns from "pkg"`) produce multiple bindings.
func extractJSBindings(node *sitter.Node, src []byte) []string {
	if node == nil {
		return nil
	}
	parent := node.Parent()
	if parent == nil {
		return nil
	}

	// ESM: node is `source: (string)` inside `import_statement`
	if parent.Type() == "import_statement" {
		return extractESMBindings(parent, src)
	}

	// CJS: node is `(string)` inside `(arguments)` inside `(call_expression)`
	// Pattern: const pkg = require('@scope/pkg')
	// Also handles compound patterns like: var X = root.X || require('pkg')
	// where binary_expression sits between call_expression and variable_declarator.
	if parent.Type() == "arguments" {
		callExpr := parent.Parent()
		if callExpr != nil && callExpr.Type() == "call_expression" {
			declarator := findAncestorVariableDeclarator(callExpr)
			if declarator != nil {
				nameNode := declarator.ChildByFieldName("name")
				if nameNode != nil && nameNode.Type() == "identifier" {
					return []string{nameNode.Content(src)}
				}
			}
		}
	}

	return nil
}

// jsExpressionTypes contains AST node types that can sit between a call_expression
// and a variable_declarator in compound require() patterns.
var jsExpressionTypes = map[string]bool{
	"binary_expression":        true,
	"ternary_expression":       true,
	"parenthesized_expression": true,
	"assignment_expression":    true,
}

// maxAncestorWalkDepth limits how far findAncestorVariableDeclarator walks up
// the AST. Prevents false matches against distant, unrelated variable_declarators
// in pathological nesting (e.g., deeply nested ternary chains).
const maxAncestorWalkDepth = 5

// findAncestorVariableDeclarator walks up from node looking for a variable_declarator,
// traversing intermediate expression nodes (e.g., binary_expression for patterns like
// `var X = root.X || require('pkg')`). Stops at statement-level nodes or after
// maxAncestorWalkDepth hops to avoid false matches.
func findAncestorVariableDeclarator(node *sitter.Node) *sitter.Node {
	current := node.Parent()
	for depth := 0; current != nil && depth < maxAncestorWalkDepth; depth++ {
		if current.Type() == "variable_declarator" {
			return current
		}
		if !jsExpressionTypes[current.Type()] {
			// Reached a non-expression node without finding a variable_declarator.
			return nil
		}
		current = current.Parent()
	}
	return nil
}

// extractESMBindings extracts all binding identifiers from an ESM import_statement.
// Combined imports (e.g., `import def, * as ns from "pkg"`) return both bindings
// so that call sites via either identifier are counted.
func extractESMBindings(importStmt *sitter.Node, src []byte) []string {
	var bindings []string
	for i := 0; i < int(importStmt.ChildCount()); i++ {
		child := importStmt.Child(i)
		if child == nil || child.Type() != "import_clause" {
			continue
		}
		// import_clause children: identifier (default), namespace_import (* as foo), named_imports ({ foo })
		for j := 0; j < int(child.ChildCount()); j++ {
			gc := child.Child(j)
			if gc == nil {
				continue
			}
			switch gc.Type() {
			case "identifier":
				// Default import: import foo from "pkg" → "foo"
				bindings = append(bindings, gc.Content(src))
			case "namespace_import":
				// import * as foo from "pkg" → "foo"
				for k := 0; k < int(gc.ChildCount()); k++ {
					n := gc.Child(k)
					if n != nil && n.Type() == "identifier" {
						bindings = append(bindings, n.Content(src))
					}
				}
			case "named_imports":
				// import { foo, bar as baz } from "pkg" → ["foo", "baz"]
				// Each import_specifier has a "name" field and optional "alias" field.
				bindings = append(bindings, extractNamedImportBindings(gc, src)...)
			}
		}
	}
	return bindings
}

// extractNamedImportBindings extracts binding names from a named_imports node.
// For `import { foo, bar as baz } from "pkg"`, it returns ["foo", "baz"].
// If an alias is present, the alias is used (that is the local binding name).
func extractNamedImportBindings(namedImports *sitter.Node, src []byte) []string {
	var bindings []string
	for k := 0; k < int(namedImports.ChildCount()); k++ {
		spec := namedImports.Child(k)
		if spec == nil || spec.Type() != "import_specifier" {
			continue
		}
		// Use alias if present (import { x as y } → "y"), otherwise name.
		aliasNode := spec.ChildByFieldName("alias")
		if aliasNode != nil {
			bindings = append(bindings, aliasNode.Content(src))
			continue
		}
		nameNode := spec.ChildByFieldName("name")
		if nameNode != nil {
			bindings = append(bindings, nameNode.Content(src))
		}
	}
	return bindings
}

// isTypeOnlyImport checks if a node's parent import_statement is a TypeScript
// `import type` (no runtime coupling).
func isTypeOnlyImport(node *sitter.Node) bool {
	parent := node.Parent()
	if parent == nil || parent.Type() != "import_statement" {
		return false
	}
	for i := 0; i < int(parent.ChildCount()); i++ {
		if parent.Child(i).Type() == "type" {
			return true
		}
	}
	return false
}

// countCallSites counts selector/member expressions matching known aliases.
func (a *Analyzer) countCallSites(
	cfg *langConfig,
	root *sitter.Node,
	src []byte,
	aliasMap map[string][]string,
	accum map[string]*accumulator,
	cursor *sitter.QueryCursor,
) {
	if len(aliasMap) == 0 {
		return
	}

	query := cfg.compiledCall
	if query == nil {
		slog.Debug("call query not compiled")
		return
	}

	cursor.Exec(query, root)

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		if len(match.Captures) >= 2 {
			// Two-capture match: pkg.field pattern (e.g., requests.get)
			pkg := match.Captures[0].Node.Content(src)
			field := match.Captures[1].Node.Content(src)

			purls, ok := aliasMap[pkg]
			if !ok {
				continue
			}
			for _, purl := range purls {
				acc := accum[purl]
				if acc == nil {
					continue
				}
				acc.callSites++
				acc.symbols[field] = true
			}
		} else if len(match.Captures) == 1 {
			// Single-capture match: bare identifier call (e.g., get() from "from x import get")
			funcName := match.Captures[0].Node.Content(src)

			purls, ok := aliasMap[funcName]
			if !ok {
				continue
			}
			for _, purl := range purls {
				acc := accum[purl]
				if acc == nil {
					continue
				}
				acc.callSites++
				acc.symbols[funcName] = true
			}
		}
	}
}

// appendUniquePURLs appends PURLs to existing, skipping duplicates.
func appendUniquePURLs(existing, newPURLs []string) []string {
	for _, p := range newPURLs {
		if !slices.Contains(existing, p) {
			existing = append(existing, p)
		}
	}
	return existing
}

// accumulator is used internally; re-declared here for the countCallSites method receiver.
type accumulator struct {
	importFiles       map[string]bool
	callSites         int
	symbols           map[string]bool
	hasDotImport      bool // true if any file uses dot import for this PURL
	hasBlankImport    bool // true if any file uses blank import for this PURL
	hasWildcardImport bool // true if any file uses wildcard import for this PURL
}
