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
func newJSLikeConfig(lang *sitter.Language) *langConfig {
	importQ := strings.Join([]string{
		`(import_statement source: (string) @import)`,
		`(call_expression function: (identifier) @func arguments: (arguments (string) @import))`,
	}, "\n")
	callQ := `(member_expression object: (identifier) @obj property: (property_identifier) @prop)`

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
		callQuery:   `(selector_expression operand: (identifier) @pkg field: (field_identifier) @field)`,
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
			`(import_from_statement module_name: (dotted_name) @import)`,
		}, "\n"),
		callQuery:   `(attribute object: (identifier) @pkg attribute: (identifier) @attr)`,
		stripQuotes: false,
		aliasFromPkg: func(importPath string) string {
			parts := strings.Split(importPath, ".")
			return parts[0]
		},
	}
	compileQueries(a.configs[langPython])

	a.configs[langJavaScript] = newJSLikeConfig(javascript.GetLanguage())
	a.configs[langTypeScript] = newJSLikeConfig(typescript.GetLanguage())
	a.configs[langTSX] = newJSLikeConfig(tsx.GetLanguage())

	a.configs[langJava] = &langConfig{
		language:    java.GetLanguage(),
		importQuery: `(import_declaration (scoped_identifier) @import)`,
		callQuery:   `(method_invocation object: (identifier) @obj name: (identifier) @method)`,
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
	// Build reverse map: importPath -> PURL
	importToPURL := make(map[string]string, len(importPaths))
	for purl, paths := range importPaths {
		for _, p := range paths {
			importToPURL[p] = purl
		}
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
		defer tree.Close()

		root := tree.RootNode()

		// Reuse a single QueryCursor across extractImports and countCallSites.
		cursor := sitter.NewQueryCursor()

		// Phase 1: Extract imports and build alias->PURL map for this file.
		fileAliases := a.extractImports(cfg, root, src, importToPURL, lid, cursor)

		relPath, _ := filepath.Rel(sourceRoot, path)
		if relPath == "" {
			relPath = path
		}

		// Record import files
		for alias, purl := range fileAliases {
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
			if alias == dotImportAlias {
				acc.hasDotImport = true
			}
		}

		// Phase 2: Count call sites using alias->PURL mapping.
		a.countCallSites(cfg, root, src, fileAliases, accum, cursor)
		cursor.Close()

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking source tree: %w", err)
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

		results[purl] = &domaindiet.CouplingAnalysis{
			ImportFileCount: len(acc.importFiles),
			CallSiteCount:   callSites,
			APIBreadth:      len(acc.symbols),
			ImportFiles:     files,
			IsUnused:        isUnused,
		}
	}

	return results, nil
}

// extractImports finds import statements in the AST and returns alias->PURL mapping.
func (a *Analyzer) extractImports(
	cfg *langConfig,
	root *sitter.Node,
	src []byte,
	importToPURL map[string]string,
	lid langID,
	cursor *sitter.QueryCursor,
) map[string]string {
	aliasMap := make(map[string]string) // alias -> PURL

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
				a.handlePythonImport(value, importToPURL, aliasMap, cfg)
				continue
			}

			// For Java: match as prefix of the full import path.
			if lid == langJava {
				a.handleJavaImport(value, importToPURL, aliasMap)
				continue
			}

			// Skip TypeScript `import type` statements (no runtime coupling).
			if lid == langTypeScript || lid == langTSX {
				if isTypeOnlyImport(capture.Node) {
					continue
				}
			}
			// For JS/TS: exact match or subpath prefix match (e.g., "lodash/fp" → "lodash").
			a.handleJSImport(value, importToPURL, aliasMap, cfg)
		}
	}

	return aliasMap
}

// handleGoImport processes a Go import spec node.
func (a *Analyzer) handleGoImport(
	node *sitter.Node,
	src []byte,
	importPath string,
	importToPURL map[string]string,
	aliasMap map[string]string,
) {
	purl, ok := importToPURL[importPath]
	if !ok {
		// Also try prefix matching for subpackages.
		for ip, p := range importToPURL {
			if strings.HasPrefix(importPath, ip+"/") || importPath == ip {
				purl = p
				ok = true
				break
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

	// Note: Blank imports (import _ "pkg") are side-effect-only, so skip them entirely.
	if alias == "_" {
		return
	}

	// Special case: Dot imports (import . "pkg") make symbols callable without a package prefix.
	// Selector-expression-based tracking cannot attribute those call sites, so mark them as used but uncountable.
	if alias == "." {
		aliasMap[dotImportAlias] = purl
		return
	}

	if alias == "" {
		// Default: last path component
		parts := strings.Split(importPath, "/")
		alias = parts[len(parts)-1]
	}

	aliasMap[alias] = purl
}

// handlePythonImport handles matching Python imports.
func (a *Analyzer) handlePythonImport(
	importPath string,
	importToPURL map[string]string,
	aliasMap map[string]string,
	cfg *langConfig,
) {
	// Try exact match first.
	if purl, ok := importToPURL[importPath]; ok {
		alias := cfg.aliasFromPkg(importPath)
		aliasMap[alias] = purl
		return
	}

	// Try top-level module name.
	topLevel := strings.Split(importPath, ".")[0]
	if purl, ok := importToPURL[topLevel]; ok {
		aliasMap[topLevel] = purl
		return
	}

	// Try prefix matching.
	for ip, purl := range importToPURL {
		if importPath == ip || strings.HasPrefix(importPath, ip+".") {
			alias := cfg.aliasFromPkg(ip)
			aliasMap[alias] = purl
			return
		}
	}
}

// handleJavaImport handles matching Java fully-qualified imports.
func (a *Analyzer) handleJavaImport(
	importPath string,
	importToPURL map[string]string,
	aliasMap map[string]string,
) {
	for ip, purl := range importToPURL {
		if importPath == ip || strings.HasPrefix(importPath, ip+".") {
			// Use the last component of the import as alias (class name).
			parts := strings.Split(importPath, ".")
			alias := parts[len(parts)-1]
			aliasMap[alias] = purl
			// Also register lowercase for variable-style calls (e.g., Gson gson = ...; gson.toJson()).
			lower := strings.ToLower(alias[:1]) + alias[1:]
			if lower != alias {
				aliasMap[lower] = purl
			}
			return
		}
	}
}

// handleJSImport processes a JS/TS module specifier with subpath prefix matching.
func (a *Analyzer) handleJSImport(
	importPath string,
	importToPURL map[string]string,
	aliasMap map[string]string,
	cfg *langConfig,
) {
	purl, ok := importToPURL[importPath]
	if !ok {
		for ip, p := range importToPURL {
			if strings.HasPrefix(importPath, ip+"/") {
				purl = p
				ok = true
				break
			}
		}
	}
	if !ok {
		return
	}

	alias := cfg.aliasFromPkg(importPath)
	aliasMap[alias] = purl
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
	aliasMap map[string]string,
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

		if len(match.Captures) < 2 {
			continue
		}

		pkg := match.Captures[0].Node.Content(src)
		field := match.Captures[1].Node.Content(src)

		purl, ok := aliasMap[pkg]
		if !ok {
			continue
		}

		acc := accum[purl]
		if acc == nil {
			continue
		}

		acc.callSites++
		acc.symbols[field] = true
	}
}

// accumulator is used internally; re-declared here for the countCallSites method receiver.
type accumulator struct {
	importFiles  map[string]bool
	callSites    int
	symbols      map[string]bool
	hasDotImport bool // true if any file uses dot import for this PURL
}
