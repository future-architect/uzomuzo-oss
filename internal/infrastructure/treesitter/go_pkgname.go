//go:build cgo

package treesitter

import (
	"go/ast"
	"go/build"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/package-url/packageurl-go"
	"golang.org/x/mod/module"
)

// goPackageNameResolver reads the real package name of an imported Go package
// from the `package` clause in the local module cache (#551). It is created per
// AnalyzeCoupling call and shared by its workers, so the cache never outlives
// one analysis.
type goPackageNameResolver struct {
	root  string
	mu    sync.Mutex
	cache map[string][]string
}

func newGoPackageNameResolver() *goPackageNameResolver {
	root := os.Getenv("GOMODCACHE")
	if root == "" {
		paths := filepath.SplitList(build.Default.GOPATH)
		if len(paths) != 0 && paths[0] != "" {
			root = filepath.Join(paths[0], "pkg", "mod")
		}
	}
	if root != "" {
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root, _ = filepath.Abs(resolved)
		} else {
			root = ""
		}
	}
	return &goPackageNameResolver{root: root, cache: make(map[string][]string)}
}

// resolve returns the package name of importPath, or "" when it cannot be
// determined uniquely, in which case the caller keeps guessing from the path.
// The import path comes from an untrusted source file, so it is validated
// before it becomes part of a filesystem path.
func (r *goPackageNameResolver) resolve(importPath, matchedKey string, purls []string) string {
	if r.root == "" || module.CheckImportPath(importPath) != nil || len(matchedKey) > len(importPath) {
		return ""
	}
	modulePath := importPath[:len(matchedKey)]
	subdir := strings.TrimPrefix(importPath[len(matchedKey):], "/")
	escapedPath, err := module.EscapePath(modulePath)
	if err != nil {
		return ""
	}
	names := make(map[string]bool)
	for _, raw := range purls {
		purl, err := packageurl.FromString(raw)
		if err != nil || purl.Version == "" {
			continue
		}
		version, err := module.EscapeVersion(purl.Version)
		if err != nil {
			continue
		}
		dir := filepath.Join(r.root, escapedPath+"@"+version, filepath.FromSlash(subdir))
		for _, name := range r.namesInDirectory(dir) {
			names[name] = true
		}
	}
	if len(names) == 1 {
		for name := range names {
			return name
		}
	}
	if len(names) > 1 {
		found := make([]string, 0, len(names))
		for name := range names {
			found = append(found, name)
		}
		slices.Sort(found)
		slog.Debug("go_package_name_ambiguous", "import_path", importPath, "names", found)
	}
	return ""
}

func (r *goPackageNameResolver) namesInDirectory(dir string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if names, ok := r.cache[dir]; ok {
		return names
	}
	names := r.readDirectory(dir)
	r.cache[dir] = names
	return names
}

func (r *goPackageNameResolver) readDirectory(dir string) []string {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil
	}
	rel, err := filepath.Rel(r.root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil
	}
	names := make(map[string]bool)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(resolved, entry.Name()), nil, parser.PackageClauseOnly|parser.ParseComments)
		if err != nil || file.Name == nil || !token.IsIdentifier(file.Name.Name) || hasIgnoreBuildTag(file) {
			continue
		}
		names[file.Name.Name] = true
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	slices.Sort(result)
	return result
}

// hasIgnoreBuildTag reports whether a `//go:build` or `// +build` line
// mentions `ignore`.
// Generator scripts kept next to a package often say `package main` behind
// that tag; counting them would make every such directory look ambiguous.
func hasIgnoreBuildTag(file *ast.File) bool {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if !constraint.IsGoBuild(comment.Text) && !constraint.IsPlusBuild(comment.Text) {
				continue
			}
			expr, err := constraint.Parse(comment.Text)
			if err == nil && containsIgnoreTag(expr) {
				return true
			}
		}
	}
	return false
}

func containsIgnoreTag(expr constraint.Expr) bool {
	switch e := expr.(type) {
	case *constraint.TagExpr:
		return e.Tag == "ignore"
	case *constraint.NotExpr:
		return containsIgnoreTag(e.X)
	case *constraint.AndExpr:
		return containsIgnoreTag(e.X) || containsIgnoreTag(e.Y)
	case *constraint.OrExpr:
		return containsIgnoreTag(e.X) || containsIgnoreTag(e.Y)
	default:
		return false
	}
}
