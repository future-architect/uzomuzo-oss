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

// wildcardImportAlias is a sentinel alias for wildcard imports (Python: from x import *,
// Java: import com.example.*). Wildcard-imported symbols are called without a package
// prefix, so bare identifier queries cannot attribute them. We mark them as "used but
// uncountable."
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

	registerGoConfig(a)
	registerPythonConfig(a)
	registerJavaScriptConfig(a)
	registerTypeScriptConfig(a)
	registerTSXConfig(a)
	registerJavaConfig(a)

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

		// Dot imports and blank/side-effect imports (Go: import _ "pkg",
		// JS: import 'x', CJS: require('x')) are used but uncountable via
		// standard call-site queries. Mark as used with a baseline call
		// site count so scoring does not penalize them.
		if acc.hasDotImport || acc.hasBlankImport {
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

// maxAncestorWalkDepth limits how far ancestor-walking functions (e.g.,
// findAncestorVariableDeclarator, isPythonTryExceptImport,
// isPythonTypeCheckingImport) traverse the AST.
// Prevents false matches against distant, unrelated ancestors in pathological
// nesting.
const maxAncestorWalkDepth = 5

// callSiteCaptureNames lists the tree-sitter capture names used for call-site
// counting across all language configs. Captures not in this set (e.g., @decorator,
// @metaKey used only for predicate filtering) are excluded from counting logic.
var callSiteCaptureNames = map[string]bool{
	"func":   true, // JS/TS/Python/Java: bare function or constructor call
	"obj":    true, // JS: member_expression object
	"prop":   true, // JS: member_expression property
	"pkg":    true, // Go/Python: package or module identifier
	"attr":   true, // Python: attribute access
	"field":  true, // Go/Java: field or member access
	"method": true, // Java: method invocation or reference
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
		// Apply tree-sitter predicates (e.g., #eq?, #match?) to filter matches.
		match = cursor.FilterPredicates(match, src)

		// Extract only the captures relevant to call-site counting, skipping
		// auxiliary captures used solely for predicate filtering (e.g., @decorator, @metaKey).
		// Use a fixed array to avoid per-match heap allocation.
		var relevant [2]sitter.QueryCapture
		nRelevant := 0
		for _, c := range match.Captures {
			if callSiteCaptureNames[query.CaptureNameForId(c.Index)] {
				if nRelevant < len(relevant) {
					relevant[nRelevant] = c
				}
				nRelevant++
			}
		}

		if nRelevant >= 2 {
			// Two-capture match: pkg.field pattern (e.g., requests.get)
			pkg := relevant[0].Node.Content(src)
			field := relevant[1].Node.Content(src)

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
		} else if nRelevant == 1 {
			// Single-capture match: bare identifier call (e.g., get() from "from x import get")
			funcName := relevant[0].Node.Content(src)

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
