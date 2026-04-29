//go:build cgo

package treesitter

import (
	"strings"
	"unicode"
	"unicode/utf8"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
)

// registerJavaConfig registers the Java language configuration on the Analyzer.
func registerJavaConfig(a *Analyzer) {
	a.configs[langJava] = &langConfig{
		language:    loadLanguage(tree_sitter_java.Language()),
		importQuery: `(import_declaration (scoped_identifier) @import)`,
		callQuery: strings.Join([]string{
			// --- Method calls ---
			`(method_invocation object: (identifier) @obj name: (identifier) @method)`,
			`(method_invocation !object name: (identifier) @func)`,

			// --- Annotations ---
			`(marker_annotation name: (identifier) @func)`,
			`(annotation name: (identifier) @func)`,

			// --- Constructors ---
			// Bare constructors: new Foo()
			`(object_creation_expression type: (type_identifier) @func)`,
			// Generic constructors: new Foo<T>(), new Foo<>()
			`(object_creation_expression type: (generic_type (type_identifier) @func))`,
			// Type arguments in generic constructors: new Foo<Bar>() captures Bar
			`(object_creation_expression type: (generic_type (type_arguments (type_identifier) @func)))`,
			// Type arguments with qualified type: new Foo<Map.Entry>() captures Map, Entry
			`(object_creation_expression type: (generic_type (type_arguments (scoped_type_identifier (type_identifier) @func))))`,
			// Qualified constructors: new Outer.Inner()
			// Each type_identifier child of scoped_type_identifier produces a
			// separate single-capture match, so countCallSites treats each as a
			// bare identifier lookup in aliasMap.
			`(object_creation_expression type: (scoped_type_identifier (type_identifier) @func))`,
			// Qualified generic constructors: new Outer.Inner<T>()
			`(object_creation_expression type: (generic_type (scoped_type_identifier (type_identifier) @func)))`,

			// --- Inheritance ---
			// Bare implements: implements Foo
			`(super_interfaces (type_list (type_identifier) @func))`,
			// Generic implements: implements Foo<T> — outer type
			`(super_interfaces (type_list (generic_type (type_identifier) @func)))`,
			// Generic implements type argument: implements Foo<Bar> — captures Bar
			`(super_interfaces (type_list (generic_type (type_arguments (type_identifier) @func))))`,
			// Generic implements type argument with qualified type: implements Foo<Map.Entry>
			`(super_interfaces (type_list (generic_type (type_arguments (scoped_type_identifier (type_identifier) @func)))))`,
			// Bare extends: extends Foo
			`(superclass (type_identifier) @func)`,
			// Generic extends: extends Foo<T> — outer type
			`(superclass (generic_type (type_identifier) @func))`,
			// Generic extends type argument: extends Foo<Bar> — captures Bar
			`(superclass (generic_type (type_arguments (type_identifier) @func)))`,
			// Generic extends type argument with qualified type: extends Foo<Map.Entry>
			`(superclass (generic_type (type_arguments (scoped_type_identifier (type_identifier) @func))))`,

			// --- Type checks and casts ---
			// instanceof: obj instanceof Foo
			`(instanceof_expression (type_identifier) @func)`,
			// instanceof: obj instanceof Map.Entry — capture bare identifiers for aliasMap lookup
			`(instanceof_expression (scoped_type_identifier (type_identifier) @func))`,
			// Cast: (Foo) obj
			`(cast_expression type: (type_identifier) @func)`,
			// Cast: (Map.Entry) obj — capture bare identifiers for aliasMap lookup
			`(cast_expression type: (scoped_type_identifier (type_identifier) @func))`,

			// --- Type declarations ---
			// Field declaration: private Foo field
			`(field_declaration type: (type_identifier) @func)`,
			// Field declaration: private Map.Entry field — capture bare identifiers for aliasMap lookup
			`(field_declaration type: (scoped_type_identifier (type_identifier) @func))`,
			// Field with generic type: private List<Foo> field — outer type
			`(field_declaration type: (generic_type (type_identifier) @func))`,
			// Field with generic type: private Outer.Inner<Foo> field — capture bare identifiers
			`(field_declaration type: (generic_type (scoped_type_identifier (type_identifier) @func)))`,
			// Field generic type argument: private List<Foo> field — captures Foo
			`(field_declaration type: (generic_type (type_arguments (type_identifier) @func)))`,
			// Field generic type argument with qualified type: private List<Map.Entry> field
			`(field_declaration type: (generic_type (type_arguments (scoped_type_identifier (type_identifier) @func))))`,
			// Method return type: public Foo method()
			`(method_declaration type: (type_identifier) @func)`,
			// Method return type: public Map.Entry method() — capture bare identifiers for aliasMap lookup
			`(method_declaration type: (scoped_type_identifier (type_identifier) @func))`,
			// Method return generic type: public List<Foo> method() — outer type
			`(method_declaration type: (generic_type (type_identifier) @func))`,
			// Method return generic type: public Outer.Inner<Foo> method() — capture bare identifiers
			`(method_declaration type: (generic_type (scoped_type_identifier (type_identifier) @func)))`,
			// Method return generic type argument: public List<Foo> method() — captures Foo
			`(method_declaration type: (generic_type (type_arguments (type_identifier) @func)))`,
			// Method return generic type argument with qualified type: public List<Map.Entry> method()
			`(method_declaration type: (generic_type (type_arguments (scoped_type_identifier (type_identifier) @func))))`,
			// Formal parameter: method(Foo param)
			`(formal_parameter type: (type_identifier) @func)`,
			// Formal parameter: method(Map.Entry param) — capture bare identifiers for aliasMap lookup
			`(formal_parameter type: (scoped_type_identifier (type_identifier) @func))`,
			// Formal parameter generic type: method(List<Foo> param) — outer type
			`(formal_parameter type: (generic_type (type_identifier) @func))`,
			// Formal parameter generic type: method(Outer.Inner<Foo> param) — capture bare identifiers
			`(formal_parameter type: (generic_type (scoped_type_identifier (type_identifier) @func)))`,
			// Formal parameter generic type argument: method(List<Foo> param) — captures Foo
			`(formal_parameter type: (generic_type (type_arguments (type_identifier) @func)))`,
			// Formal parameter generic type argument with qualified type: method(List<Map.Entry> param)
			`(formal_parameter type: (generic_type (type_arguments (scoped_type_identifier (type_identifier) @func))))`,
			// Local variable: Foo local = ...
			`(local_variable_declaration type: (type_identifier) @func)`,
			// Local variable: Map.Entry local = ... — capture bare identifiers for aliasMap lookup
			`(local_variable_declaration type: (scoped_type_identifier (type_identifier) @func))`,
			// Local variable generic type: List<Foo> local = ... — outer type
			`(local_variable_declaration type: (generic_type (type_identifier) @func))`,
			// Local variable generic type: Outer.Inner<Foo> local = ... — capture bare identifiers
			`(local_variable_declaration type: (generic_type (scoped_type_identifier (type_identifier) @func)))`,
			// Local variable generic type argument: List<Foo> local = ... — captures Foo
			`(local_variable_declaration type: (generic_type (type_arguments (type_identifier) @func)))`,
			// Local variable generic type argument with qualified type: List<Map.Entry> local = ...
			`(local_variable_declaration type: (generic_type (type_arguments (scoped_type_identifier (type_identifier) @func))))`,

			// --- Method references ---
			// Method reference: Foo::bar — capture simple qualifiers directly
			// so alias lookup remains consistent with method_invocation.
			`(method_reference . (identifier) @obj . (identifier) @method)`,
			// Scoped method reference: Outer.Inner::bar, Map.Entry::getKey.
			`(method_reference . (field_access (identifier) @obj) . (identifier) @method)`,
			// Constructor reference: Foo::new
			`(method_reference . (identifier) @obj @method . "new")`,
			// Scoped constructor reference: Outer.Inner::new.
			`(method_reference . (field_access (identifier) @obj (identifier) @method) . "new")`,
			// Field access: MyEnum.VALUE, Constants.PI, Locale.US.
			// NOTE: this pattern also matches the field_access node inside scoped
			// method references (e.g., ImmutableList.Builder::add), which is
			// intentional — the field access represents genuine type coupling
			// even when nested inside a method reference expression.
			`(field_access object: (identifier) @obj field: (identifier) @field)`,
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

	// Wildcard imports (import com.example.* or import static org.junit.Assert.*)
	// cannot track individual names. In tree-sitter-java, the asterisk is a
	// separate child of import_declaration (not part of scoped_identifier),
	// so the captured import path does not include "*".
	if isJavaWildcardImport(node) {
		key := wildcardImportAlias + importPath
		aliasMap[key] = appendUniquePURLs(aliasMap[key], bestPURLs)
		return
	}

	// Check if this is a static import by looking for a "static" child
	// in the parent import_declaration node.
	isStatic := isJavaStaticImport(node)

	parts := strings.Split(importPath, ".")
	alias := parts[len(parts)-1]

	if isStatic {
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

// isJavaWildcardImport checks whether a scoped_identifier node is part of a wildcard import
// (import com.example.* or import static org.junit.Assert.*).
// In tree-sitter-java, `import com.example.*;` parses as:
//
//	import_declaration -> ["import", scoped_identifier("com.example"), ".", asterisk, ";"]
//
// The asterisk is a separate child of import_declaration, not part of scoped_identifier.
func isJavaWildcardImport(node *sitter.Node) bool {
	parent := node.Parent()
	if parent == nil || parent.Kind() != "import_declaration" {
		return false
	}
	for i := uint(0); i < parent.ChildCount(); i++ {
		if parent.Child(i).Kind() == "asterisk" {
			return true
		}
	}
	return false
}

// isJavaStaticImport checks whether a scoped_identifier node is part of a static import.
// The AST structure is: import_declaration -> ["import", "static", scoped_identifier, ";"]
func isJavaStaticImport(node *sitter.Node) bool {
	parent := node.Parent()
	if parent == nil || parent.Kind() != "import_declaration" {
		return false
	}
	for i := uint(0); i < parent.ChildCount(); i++ {
		if parent.Child(i).Kind() == "static" {
			return true
		}
	}
	return false
}
