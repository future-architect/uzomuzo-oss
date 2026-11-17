//go:build cgo

package treesitter

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// goNamesQuery captures what inferGoImportNames needs from one Go file: the
// X of every X.Y, every other identifier, the import specs and the package
// clause. go/ast has one Ident where tree-sitter-go has several node kinds;
// every kind a declaration can produce is listed, so that a declared struct
// field, method name, label or import alias excludes its name just like a
// variable does.
const goNamesQuery = `
(selector_expression operand: (identifier) @operand field: (field_identifier) @field)
(qualified_type package: (package_identifier) @operand name: (type_identifier) @field)
[(identifier) (type_identifier) (field_identifier) (package_identifier) (label_name)] @ident
(import_spec) @import
(package_clause (package_identifier) @package)
`

// goImportSpec is one import of a Go file. name is "" for an unaliased import.
type goImportSpec struct {
	path, name string
}

// goFileNames holds the identifiers of one Go file, split by position.
type goFileNames struct {
	pkg     string
	imports []goImportSpec
	// operands are names used as X in X.Y or in a qualified type X.T.
	operands map[string]bool
	// others are names seen anywhere else (declarations, plain uses, fields,
	// labels, aliases, the package clause).
	others map[string]bool
}

// collectGoNames runs goNamesQuery over one parsed Go file.
func collectGoNames(query *sitter.Query, root *sitter.Node, src []byte, cursor *sitter.QueryCursor) goFileNames {
	f := goFileNames{operands: make(map[string]bool), others: make(map[string]bool)}
	if query == nil {
		return f
	}
	names := query.CaptureNames()
	selectorParts := make(map[uint]bool)
	type ident struct {
		start uint
		text  string
	}
	var idents []ident

	matches := cursor.Matches(query, root, src)
	for {
		match := matches.Next()
		if match == nil {
			break
		}
		for _, c := range match.Captures {
			if int(c.Index) >= len(names) {
				continue
			}
			node := c.Node
			switch names[c.Index] {
			case "operand":
				selectorParts[node.StartByte()] = true
				f.operands[node.Utf8Text(src)] = true
			case "field":
				// Only the field of a selector whose operand is a plain identifier is
				// excluded. The c of a.b.c stays in others, which can only keep a
				// guess, never hand out a wrong name.
				selectorParts[node.StartByte()] = true
			case "ident":
				idents = append(idents, ident{node.StartByte(), node.Utf8Text(src)})
			case "package":
				f.pkg = node.Utf8Text(src)
			case "import":
				f.imports = append(f.imports, goImportFromSpec(&node, src))
			}
		}
	}
	for _, id := range idents {
		if !selectorParts[id.start] {
			f.others[id.text] = true
		}
	}
	return f
}

func goImportFromSpec(spec *sitter.Node, src []byte) goImportSpec {
	var imp goImportSpec
	if p := spec.ChildByFieldName("path"); p != nil {
		imp.path = strings.Trim(p.Utf8Text(src), "\"`")
	}
	if n := spec.ChildByFieldName("name"); n != nil {
		imp.name = n.Utf8Text(src)
	}
	return imp
}

// inferGoImportNames decides the package name of an unaliased import from how
// the file uses it (#551). Names used as X in X.Y that are not declared
// anywhere in the package (declared) and not explained by another import are
// left over; when exactly one import and one name are left, that import gets
// that name. Otherwise it returns nil and the caller keeps the guess from the
// import path.
//
// Every import takes part, including the standard library and imports outside
// the SBOM: leaving one out would leave its name unexplained and could hand
// that name to a dependency.
func inferGoImportNames(f goFileNames, declared map[string]bool) map[string]string {
	imports, names, ok := goUnexplained(f, declared)
	if !ok || len(imports) != 1 || len(names) != 1 {
		return nil
	}
	return map[string]string{imports[0]: names[0]}
}

// goUnexplained returns the unaliased imports whose guessed name is not used
// as an operand, and the operand names that are neither declared nor
// explained by an import. ok is false when the operands cannot be matched to
// imports:
//   - a dot import: its names are used without a prefix;
//   - two imports with the same name or guessed name (apis/apiextensions and
//     apis/apiextensions/v1beta1 are both guessed as apiextensions). Go rejects
//     two imports with one name, so one guess is wrong, and the name it hides
//     would otherwise be handed to an unrelated import.
func goUnexplained(f goFileNames, declared map[string]bool) (imports, names []string, ok bool) {
	used := make(map[string]bool)
	for name := range f.operands {
		if !declared[name] {
			used[name] = true
		}
	}
	explained := make(map[string]bool)
	seen := make(map[string]bool)
	for _, imp := range f.imports {
		name := imp.name
		switch {
		case name == ".":
			return nil, nil, false
		case name == "_":
			continue
		case name == "" && imp.path == "C":
			name = "C"
		case name == "":
			name = goAliasFromImportPath(imp.path)
			if !used[name] {
				imports = append(imports, imp.path)
			}
		}
		if seen[name] {
			return nil, nil, false
		}
		seen[name] = true
		explained[name] = true
	}
	for name := range used {
		if !explained[name] && !goPredeclared[name] {
			names = append(names, name)
		}
	}
	return imports, names, true
}

// goPredeclared holds Go's predeclared identifiers. They are never handed to
// an import: error.Error is a valid method expression, and the error left
// over would otherwise rename an import whose guess was right. They still
// count as used, so an import guessed as, say, max keeps that guess.
var goPredeclared = map[string]bool{
	"any": true, "bool": true, "byte": true, "comparable": true,
	"complex64": true, "complex128": true, "error": true, "float32": true,
	"float64": true, "int": true, "int8": true, "int16": true, "int32": true,
	"int64": true, "rune": true, "string": true, "uint": true, "uint8": true,
	"uint16": true, "uint32": true, "uint64": true, "uintptr": true,
	"true": true, "false": true, "iota": true, "nil": true,
	"append": true, "cap": true, "clear": true, "close": true, "complex": true,
	"copy": true, "delete": true, "imag": true, "len": true, "make": true,
	"max": true, "min": true, "new": true, "panic": true, "print": true,
	"println": true, "real": true, "recover": true,
}

// goDirIndex collects, per directory and package clause, the names that the
// package's files use outside an X.Y operand. It is built lazily, once per
// directory, for the duration of one AnalyzeCoupling call.
//
// The scan parses every .go file of the directory again, including the one
// analyzeFile already parsed. Reusing those trees would tie the result to the
// order in which workers reach the files of a directory.
//
// ctx is the AnalyzeCoupling call's context. The index lives only for that
// call, so it is kept here rather than threaded through analyzeFile, which
// has no context parameter of its own.
type goDirIndex struct {
	ctx  context.Context
	cfg  *langConfig
	mu   sync.Mutex
	dirs map[string]*goDirEntry
}

func newGoDirIndex(ctx context.Context, cfg *langConfig) *goDirIndex {
	return &goDirIndex{ctx: ctx, cfg: cfg, dirs: make(map[string]*goDirEntry)}
}

type goDirEntry struct {
	once sync.Once
	// others is nil when the directory cannot be used: it cannot be read, or a
	// .go file in it is not a regular file, is larger than largeFileBytes,
	// cannot be read, or does not parse cleanly. Its declarations are then
	// unknown.
	others map[string]map[string]bool
}

// declared returns the names used outside an X.Y operand by the files of
// package pkg in dir, or false when the directory cannot be used.
func (x *goDirIndex) declared(dir, pkg string) (map[string]bool, bool) {
	x.mu.Lock()
	e, ok := x.dirs[dir]
	if !ok {
		e = &goDirEntry{}
		x.dirs[dir] = e
	}
	x.mu.Unlock()

	e.once.Do(func() { e.others = x.scan(dir) })
	if e.others == nil {
		return nil, false
	}
	return e.others[pkg], true
}

func (x *goDirIndex) scan(dir string) map[string]map[string]bool {
	unusable := func(reason, file string, err error) map[string]map[string]bool {
		args := []any{"dir", dir, "file", file, "reason", reason}
		if err != nil {
			args = append(args, "error", err)
		}
		slog.Debug("go_dir_index_unusable", args...)
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return unusable("read_dir", "", err)
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(x.cfg.language); err != nil {
		return unusable("set_language", "", err)
	}
	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	byPkg := make(map[string]map[string]bool)
	for _, entry := range entries {
		file := entry.Name()
		// A canceled call discards the result anyway; stop reading the
		// remaining files instead of keeping other workers waiting on once.
		if err := x.ctx.Err(); err != nil {
			return unusable("canceled", file, err)
		}
		if entry.IsDir() || filepath.Ext(file) != ".go" {
			continue
		}
		// A symlinked file is still analyzed by analyzeFile, but its target
		// may lie outside --source; give up on the directory instead.
		if !entry.Type().IsRegular() {
			return unusable("not_regular_file", file, nil)
		}
		info, err := entry.Info()
		if err != nil {
			return unusable("stat", file, err)
		}
		if info.Size() > largeFileBytes {
			return unusable("large_file", file, nil)
		}
		src, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return unusable("read_file", file, err)
		}
		tree := parser.Parse(src, nil)
		if tree == nil {
			return unusable("parse", file, nil)
		}
		root := tree.RootNode()
		if root.HasError() {
			tree.Close()
			return unusable("syntax_error", file, nil)
		}
		f := collectGoNames(x.cfg.compiledNames, root, src, cursor)
		tree.Close()
		if byPkg[f.pkg] == nil {
			byPkg[f.pkg] = make(map[string]bool)
		}
		for name := range f.others {
			byPkg[f.pkg][name] = true
		}
	}
	return byPkg
}

// inferGoNames returns the package names decided by inferGoImportNames for the
// unaliased imports of one Go file, using the declarations of its whole
// package directory.
func inferGoNames(
	root *sitter.Node,
	src []byte,
	path string,
	importToPURL map[string][]string,
	dirs *goDirIndex,
	cursor *sitter.QueryCursor,
) map[string]string {
	// Checked before the names query, which captures every identifier and is
	// wasted on the many files that import no dependency without an alias.
	if root.HasError() || !hasUnaliasedGoDependency(root, src, importToPURL) {
		return nil
	}
	f := collectGoNames(dirs.cfg.compiledNames, root, src, cursor)
	// The package-wide names are a superset of this file's, so they can only
	// shrink the unexplained names and grow the unexplained imports. When the
	// file alone has no unexplained name, or two unexplained imports, the
	// directory scan cannot decide anything either.
	if imports, names, ok := goUnexplained(f, f.others); !ok || len(names) == 0 || len(imports) >= 2 {
		return nil
	}
	declared, ok := dirs.declared(filepath.Dir(path), f.pkg)
	if !ok {
		return nil
	}
	return inferGoImportNames(f, declared)
}

// hasUnaliasedGoDependency reports whether the file has an import without a
// name that matches a dependency. Imports are only allowed at the top of a Go
// file, so the top-level import declarations are enough.
func hasUnaliasedGoDependency(root *sitter.Node, src []byte, importToPURL map[string][]string) bool {
	for i := range root.NamedChildCount() {
		decl := root.NamedChild(i)
		if decl == nil || decl.Kind() != "import_declaration" {
			continue
		}
		for j := range decl.NamedChildCount() {
			child := decl.NamedChild(j)
			if child == nil {
				continue
			}
			specs := []*sitter.Node{child}
			if child.Kind() == "import_spec_list" {
				specs = specs[:0]
				for k := range child.NamedChildCount() {
					specs = append(specs, child.NamedChild(k))
				}
			}
			for _, spec := range specs {
				if spec == nil || spec.Kind() != "import_spec" {
					continue
				}
				imp := goImportFromSpec(spec, src)
				if imp.name != "" {
					continue
				}
				if _, ok := matchGoImport(imp.path, importToPURL); ok {
					return true
				}
			}
		}
	}
	return false
}
