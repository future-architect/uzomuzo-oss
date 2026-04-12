//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// registerJavaScriptConfig registers the JavaScript language configuration on the Analyzer.
func registerJavaScriptConfig(a *Analyzer) {
	a.configs[langJavaScript] = newJSLikeConfig(javascript.GetLanguage(), true)
}

// registerTypeScriptConfig registers the TypeScript language configuration on the Analyzer.
func registerTypeScriptConfig(a *Analyzer) {
	a.configs[langTypeScript] = newJSLikeConfig(typescript.GetLanguage(), false)
}

// registerTSXConfig registers the TSX language configuration on the Analyzer.
func registerTSXConfig(a *Analyzer) {
	a.configs[langTSX] = newJSLikeConfig(tsx.GetLanguage(), true)
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
		// obj.alias.method() — nested member access where alias was assigned via
		// `obj.alias = require('pkg')`. Matches the outer member_expression with
		// the inner's property as @obj and the outer's property as @prop.
		`(member_expression object: (member_expression property: (property_identifier) @obj) property: (property_identifier) @prop)`,
		// foo() — bare function call on an imported named binding
		`(call_expression function: (identifier) @func)`,
		// obj.alias() — direct call on a property-assigned require binding.
		// Captures just the property name so it can match aliases registered from
		// `obj.alias = require('pkg')`.
		`(call_expression function: (member_expression property: (property_identifier) @func))`,
		// new Foo() — constructor call on an imported named binding
		`(new_expression constructor: (identifier) @func)`,
		// { [ATTR]: val } — imported constant used as computed property key
		`(computed_property_name (identifier) @func)`,
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

	// Side-effect imports (`import 'reflect-metadata'`) have no import_clause,
	// meaning no bindings are introduced. Treat them like Go blank imports:
	// record the import file so the dep is not misclassified as "unused", but
	// skip call-site counting.
	if isSideEffectImport(node) {
		key := blankImportAlias + importPath
		aliasMap[key] = appendUniquePURLs(aliasMap[key], purls)
		return
	}

	// Extract the JS/TS binding names from the AST (e.g., "cloud" from
	// `import cloud from '@strapi/plugin-cloud'`). Falls back to aliasFromPkg
	// for patterns we cannot resolve from the AST.
	aliases := extractJSBindings(node, src)
	if len(aliases) == 0 {
		// No variable binding — check for inline require patterns:
		//   require('x')()          — immediate invocation
		//   require('x').method()   — chained property access
		//   require('x')('arg')     — factory pattern
		//   require('x');           — bare side-effect require
		// These are used without binding to a variable, so call-site queries
		// cannot match them. Register with the blank-import sentinel to treat
		// the require itself as proof of usage.
		if isInlineRequireUsage(node) {
			key := blankImportAlias + importPath
			aliasMap[key] = appendUniquePURLs(aliasMap[key], purls)
			return
		}
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
			// Check for variable declarator: const x = require('pkg')
			declarator := findAncestorVariableDeclarator(callExpr)
			if declarator != nil {
				nameNode := declarator.ChildByFieldName("name")
				if nameNode == nil {
					return nil
				}
				switch nameNode.Type() {
				case "identifier":
					return []string{nameNode.Content(src)}
				case "object_pattern":
					// Destructured: const { X, Y } = require('pkg')
					return extractCJSDestructuredBindings(nameNode, src)
				}
			}

			// Check for property assignment: obj.prop = require('pkg')
			// The property name becomes the binding alias so that call sites
			// like obj.prop.method() or obj.prop() can be tracked.
			if binding := extractPropertyAssignBinding(callExpr, src); binding != "" {
				return []string{binding}
			}
		}
	}

	return nil
}

// isInlineRequireUsage checks whether a CJS require() call is used inline without
// a variable binding. The node is the string literal inside require('pkg').
//
// Inline patterns detected:
//   - require('x')()        — immediate invocation (parent call_expression is the function of an outer call)
//   - require('x').method() — chained property access (parent call_expression is the object of a member_expression)
//   - require('x')('arg')   — factory pattern (same AST shape as immediate invocation)
//   - require('x');          — bare side-effect require (parent call_expression is inside expression_statement)
//   - require('x').prop      — member access without call (parent call_expression is object of member_expression)
//
// AST walk: string → arguments → call_expression(require) → check parent type.
func isInlineRequireUsage(node *sitter.Node) bool {
	if node == nil {
		return false
	}
	// node is the string literal; its parent should be arguments.
	args := node.Parent()
	if args == nil || args.Type() != "arguments" {
		return false
	}
	// arguments' parent should be the require() call_expression.
	requireCall := args.Parent()
	if requireCall == nil || requireCall.Type() != "call_expression" {
		return false
	}
	// Check what uses the require() call_expression.
	parent := requireCall.Parent()
	if parent == nil {
		return false
	}
	switch parent.Type() {
	case "call_expression":
		// require('x')() or require('x')('arg') — the require call is the function of an outer call.
		return true
	case "member_expression":
		// require('x').method or require('x').prop — chained property access.
		return true
	case "expression_statement":
		// require('x'); — bare side-effect require.
		return true
	case "arguments":
		// f(require('x')) — passed as argument to another function.
		return true
	default:
		return false
	}
}

// jsExpressionTypes contains AST node types that can sit between a call_expression
// and a variable_declarator in compound require() patterns.
var jsExpressionTypes = map[string]bool{
	"binary_expression":        true,
	"ternary_expression":       true,
	"parenthesized_expression": true,
	"assignment_expression":    true,
}

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

// extractCJSDestructuredBindings extracts binding names from a CJS destructured
// require pattern. For `const { foo, bar: baz } = require("pkg")`, the AST has
// an object_pattern with children:
//   - shorthand_property_identifier_pattern for `{ foo }` (name == foo, local binding == foo)
//   - pair_pattern for `{ bar: baz }` (key == bar, value == baz, local binding == baz)
//
// This mirrors extractNamedImportBindings for ESM named imports.
func extractCJSDestructuredBindings(objectPattern *sitter.Node, src []byte) []string {
	var bindings []string
	for i := 0; i < int(objectPattern.ChildCount()); i++ {
		child := objectPattern.Child(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "shorthand_property_identifier_pattern":
			// { X } — the identifier itself is the local binding.
			bindings = append(bindings, child.Content(src))
		case "pair_pattern":
			// { X: alias } — the value side is the local binding.
			valueNode := child.ChildByFieldName("value")
			if valueNode != nil && valueNode.Type() == "identifier" {
				bindings = append(bindings, valueNode.Content(src))
			}
		}
	}
	return bindings
}

// extractPropertyAssignBinding extracts the property name from a CJS require()
// assigned to an object property. For `obj.prop = require('pkg')`, the AST is:
//
//	assignment_expression
//	  left: member_expression
//	    object: identifier (obj)
//	    property: property_identifier (prop)
//	  right: call_expression (require('pkg'))
//
// This function walks up from the call_expression through intermediate
// expression nodes (e.g., binary_expression) to find the assignment_expression,
// then extracts the property name from the member_expression on the left side.
// Returns empty string if the pattern does not match.
func extractPropertyAssignBinding(callExpr *sitter.Node, src []byte) string {
	current := callExpr.Parent()
	for depth := 0; current != nil && depth < maxAncestorWalkDepth; depth++ {
		if current.Type() == "assignment_expression" {
			left := current.ChildByFieldName("left")
			if left != nil && left.Type() == "member_expression" {
				propNode := left.ChildByFieldName("property")
				if propNode != nil {
					return propNode.Content(src)
				}
			}
			return ""
		}
		if !jsExpressionTypes[current.Type()] {
			return ""
		}
		current = current.Parent()
	}
	return ""
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

// isSideEffectImport checks if a JS/TS import is a bare side-effect import
// with no bindings (e.g., `import 'reflect-metadata'`). These imports have an
// import_statement parent but no import_clause child — only the source string.
// This helper only classifies bare ESM `import "x"` statements as side-effect
// imports; it does not attempt to classify CommonJS `require()` usage.
func isSideEffectImport(node *sitter.Node) bool {
	parent := node.Parent()
	if parent == nil || parent.Type() != "import_statement" {
		return false
	}
	for i := 0; i < int(parent.ChildCount()); i++ {
		if parent.Child(i).Type() == "import_clause" {
			return false
		}
	}
	return true
}
