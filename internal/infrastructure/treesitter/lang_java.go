//go:build cgo

package treesitter

import (
	"strings"
	"unicode"
	"unicode/utf8"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/java"
)

// registerJavaConfig registers the Java language configuration on the Analyzer.
func registerJavaConfig(a *Analyzer) {
	a.configs[langJava] = &langConfig{
		language:    java.GetLanguage(),
		importQuery: `(import_declaration (scoped_identifier) @import)`,
		callQuery: strings.Join([]string{
			`(method_invocation object: (identifier) @obj name: (identifier) @method)`,
			`(method_invocation !object name: (identifier) @func)`,
			`(marker_annotation name: (identifier) @func)`,
			`(annotation name: (identifier) @func)`,
			// Bare constructors: new Foo()
			`(object_creation_expression type: (type_identifier) @func)`,
			// Generic constructors: new Foo<T>(), new Foo<>()
			`(object_creation_expression type: (generic_type (type_identifier) @func))`,
			// Qualified constructors: new Outer.Inner()
			// Single @func capture; countCallSites matches each captured
			// type_identifier against aliasMap as a bare identifier.
			`(object_creation_expression type: (scoped_type_identifier (type_identifier) @func))`,
			// Qualified generic constructors: new Outer.Inner<T>()
			`(object_creation_expression type: (generic_type (scoped_type_identifier (type_identifier) @func)))`,
			// Bare implements: implements Foo
			`(super_interfaces (type_list (type_identifier) @func))`,
			// Generic implements: implements Foo<T>
			`(super_interfaces (type_list (generic_type (type_identifier) @func)))`,
			// Bare extends: extends Foo
			`(superclass (type_identifier) @func)`,
			// Generic extends: extends Foo<T>
			`(superclass (generic_type (type_identifier) @func))`,
			// Method reference: Foo::bar — dual capture so alias lookup uses
			// the qualifier expression while symbol recording uses the method
			// name, consistent with method_invocation handling.
			`(method_reference . (_) @obj . (identifier) @method)`,
			// Constructor reference: Foo::new — tree-sitter-java represents
			// "new" as a token, not an identifier, so match it explicitly.
			`(method_reference . (_) @obj . "new" @method)`,
		}, "\n"),
		stripQuotes: false,
		aliasFromPkg: func(importPath string) string {
			return importPath
		},
	}
	compileQueries(a.configs[langJava])
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
	firstRune, size := utf8.DecodeRuneInString(alias)
	lower := string(unicode.ToLower(firstRune)) + alias[size:]
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
