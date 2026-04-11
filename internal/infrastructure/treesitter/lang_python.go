//go:build cgo

package treesitter

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
)

// registerPythonConfig registers the Python language configuration on the Analyzer.
func registerPythonConfig(a *Analyzer) {
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

	// Skip imports inside `if TYPE_CHECKING:` blocks — they are type-only and
	// never execute at runtime, so they should not count toward IBNC.
	if isPythonTypeCheckingImport(parent, src) {
		return
	}

	// Check if this import is inside a try/except ImportError block.
	// This is often a Python feature-detection pattern where the import itself
	// signals optional dependency detection. Record that fact like a Go blank
	// import, but still register the actual binding so later call-site counting
	// can attribute usage such as "cryptography.*" or "fernet()".
	if isPythonTryExceptImport(parent, src) {
		key := blankImportAlias + importPath
		aliasMap[key] = appendUniquePURLs(aliasMap[key], purls)
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

// isPythonTryExceptImport checks if a Python import-related node is inside the
// try body of a try/except block that catches ImportError or
// ModuleNotFoundError. This is a common feature-detection pattern where the
// import itself is the usage (checking if the package is installed).
//
// importStmt may be an import_statement, import_from_statement, aliased_import,
// or another import-related descendant node. src is the file source for
// reading node content.
func isPythonTryExceptImport(importStmt *sitter.Node, src []byte) bool {
	// Walk up from the import statement to find the nearest enclosing
	// try_statement while tracking the child path. Only imports contained in the
	// try_statement's body field should be treated as feature-detection imports;
	// imports in except/else/finally blocks are fallback or cleanup logic and
	// must not be classified as blank imports.
	child := importStmt
	current := importStmt.Parent()
	for depth := 0; current != nil && depth < maxAncestorWalkDepth; depth++ {
		if current.Type() == "try_statement" {
			tryBody := current.ChildByFieldName("body")
			if tryBody == nil || tryBody != child {
				return false
			}
			return hasPythonImportErrorHandler(current, src)
		}
		child = current
		current = current.Parent()
	}
	return false
}

// hasPythonImportErrorHandler checks whether a try_statement has an except_clause
// that catches ImportError, ModuleNotFoundError, or is a bare except (no type).
func hasPythonImportErrorHandler(tryStmt *sitter.Node, src []byte) bool {
	for i := 0; i < int(tryStmt.ChildCount()); i++ {
		child := tryStmt.Child(i)
		if child.Type() != "except_clause" {
			continue
		}

		// Check each child of the except_clause for exception type identifiers.
		// Exception types may appear as direct identifier children (single type)
		// or inside a tuple child (multiple types, e.g., "except (ImportError, ValueError)").
		hasExceptionType := false
		for j := 0; j < int(child.ChildCount()); j++ {
			gc := child.Child(j)
			switch gc.Type() {
			case "identifier":
				hasExceptionType = true
				name := gc.Content(src)
				if name == "ImportError" || name == "ModuleNotFoundError" {
					return true
				}
			case "tuple":
				// except (ExcA, ExcB): — identifiers are inside the tuple node.
				for k := 0; k < int(gc.ChildCount()); k++ {
					tc := gc.Child(k)
					if tc.Type() == "identifier" {
						hasExceptionType = true
						name := tc.Content(src)
						if name == "ImportError" || name == "ModuleNotFoundError" {
							return true
						}
					}
				}
			}
		}

		// Bare except (no exception type specified) catches everything
		// including ImportError.
		if !hasExceptionType {
			return true
		}
	}
	return false
}

// isPythonTypeCheckingImport checks if a Python import-related node is inside an
// `if TYPE_CHECKING:` or `if typing.TYPE_CHECKING:` block. Python's
// typing.TYPE_CHECKING constant is False at runtime, so imports guarded by it
// are type-only and never execute. These imports should not count toward IBNC.
//
// importStmt may be an import_statement, import_from_statement, or another
// import-related node. src is the file source for reading node content.
func isPythonTypeCheckingImport(importStmt *sitter.Node, src []byte) bool {
	// Walk up from the import statement to find the nearest enclosing if_statement,
	// tracking the child path to ensure the import is in the if body (consequence),
	// not in an else clause.
	child := importStmt
	current := importStmt.Parent()
	for depth := 0; current != nil && depth < maxAncestorWalkDepth; depth++ {
		if current.Type() == "if_statement" {
			// Only match imports in the if body (consequence), not else/elif.
			body := current.ChildByFieldName("consequence")
			if body == nil || body != child {
				return false
			}
			return isTypeCheckingCondition(current, src)
		}
		child = current
		current = current.Parent()
	}
	return false
}

// isTypeCheckingCondition checks if an if_statement's condition is
// `TYPE_CHECKING` (bare identifier) or `typing.TYPE_CHECKING` (attribute access).
func isTypeCheckingCondition(ifStmt *sitter.Node, src []byte) bool {
	cond := ifStmt.ChildByFieldName("condition")
	if cond == nil {
		return false
	}

	switch cond.Type() {
	case "identifier":
		// `if TYPE_CHECKING:`
		return cond.Content(src) == "TYPE_CHECKING"
	case "attribute":
		// `if typing.TYPE_CHECKING:`
		obj := cond.ChildByFieldName("object")
		attr := cond.ChildByFieldName("attribute")
		if obj != nil && attr != nil {
			return obj.Content(src) == "typing" && attr.Content(src) == "TYPE_CHECKING"
		}
	}
	return false
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
