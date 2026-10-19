//go:build cgo

package treesitter

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domaindiet "github.com/future-architect/uzomuzo-oss/internal/domain/diet"
)

// TestAnalyzer_CloseIdempotent verifies that Analyzer.Close can be called
// multiple times without panic and that subsequent AnalyzeCoupling calls
// do not panic. The official tree-sitter Go bindings require explicit Close
// on Parser/Tree/Query/QueryCursor, so callers may combine
// `defer analyzer.Close()` and a test cleanup that calls it again — both
// must be safe. After Close, AnalyzeCoupling still walks and parses files
// but returns a nil result because the per-language nil-query guards inside
// extractImports/countCallSites prevent import/call extraction.
func TestAnalyzer_CloseIdempotent(t *testing.T) {
	analyzer := NewAnalyzer()
	analyzer.Close()
	// Second Close must not panic — the implementation nils released queries.
	analyzer.Close()

	// After Close, AnalyzeCoupling must not panic. With every per-language
	// query released, the import/call query nil guards return early; with no
	// imports collected, AnalyzeCoupling returns (nil, nil) per the
	// "no coupling data" branch.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main
import "github.com/foo/bar"
func main() { bar.Do() }
`), 0644); err != nil {
		t.Fatal(err)
	}
	importPaths := map[string][]string{
		"pkg:golang/github.com/foo/bar@v1.0.0": {"github.com/foo/bar"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Errorf("AnalyzeCoupling after Close returned err = %v, want nil", err)
	}
	if result != nil {
		t.Errorf("AnalyzeCoupling after Close returned %d entries, want nil result", len(result))
	}
}

func TestAnalyzer_SkipsDirs(t *testing.T) {
	dir := t.TempDir()
	vendorDir := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		t.Fatal(err)
	}
	// File in vendor should be skipped
	if err := os.WriteFile(filepath.Join(vendorDir, "main.go"), []byte(`package main
import "github.com/foo/bar"
func main() { bar.Do() }
`), 0644); err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	t.Cleanup(analyzer.Close)
	importPaths := map[string][]string{
		"pkg:golang/github.com/foo/bar@v1.0.0": {"github.com/foo/bar"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 0 {
		t.Errorf("expected no results when all files are in vendor, got %d", len(result))
	}
}

// TestAnalyzer_BuildOutputDirs verifies that build, dist and target are
// skipped only when their direct parent holds the manifest of a tool that
// writes there, and read when they are ordinary package directories (#546). A control file at the
// root always imports the dependency, so ImportFileCount is 2 when the
// directory is read and 1 when it is skipped.
func TestAnalyzer_BuildOutputDirs(t *testing.T) {
	const src = `package b
import "github.com/foo/bar"
func B() { bar.Do() }
`
	tests := []struct {
		name     string
		manifest string // manifest path relative to the root; "" for none
		dir      string
		wantRead bool
	}{
		{"Go package named build", "", "internal/build", true},
		{"build next to go.mod", "go.mod", "build", true},
		{"build with package.json in the grandparent", "package.json", "src/build", true},
		{"dist next to package.json", "package.json", "dist", false},
		{"target next to pom.xml", "pom.xml", "target", false},
		{"build next to build.gradle", "build.gradle", "build", false},
		{"build next to pyproject.toml", "pyproject.toml", "build", false},
		{"target next to Cargo.toml", "Cargo.toml", "target", false},
		{"target next to build.sbt", "build.sbt", "target", false},
		{"build next to Cargo.toml", "Cargo.toml", "build", true},
		{"dist next to build.sbt", "build.sbt", "dist", true},
		{"target next to package.json", "package.json", "target", true},
		{"dist next to build.gradle", "build.gradle", "dist", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			pkgDir := filepath.Join(root, filepath.FromSlash(tt.dir))
			if err := os.MkdirAll(pkgDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if tt.manifest != "" {
				// Only the manifest's existence is checked, so it is left empty.
				writeCorpusFile(t, filepath.Join(root, filepath.FromSlash(tt.manifest)), "")
			}
			writeCorpusFile(t, filepath.Join(pkgDir, "b.go"), src)
			writeCorpusFile(t, filepath.Join(root, "main.go"), src)
			want := 1
			if tt.wantRead {
				want = 2
			}
			if got := analyzeFooBar(t, root).ImportFileCount; got != want {
				t.Errorf("ImportFileCount = %d, want %d", got, want)
			}
		})
	}

	t.Run("scan root named build next to package.json", func(t *testing.T) {
		parent := t.TempDir()
		writeCorpusFile(t, filepath.Join(parent, "package.json"), "")
		root := filepath.Join(parent, "build")
		if err := os.Mkdir(root, 0o755); err != nil {
			t.Fatal(err)
		}
		writeCorpusFile(t, filepath.Join(root, "b.go"), src)
		if got := analyzeFooBar(t, root).ImportFileCount; got != 1 {
			t.Errorf("ImportFileCount = %d, want 1", got)
		}
	})

	t.Run("directory named like a manifest is not a manifest", func(t *testing.T) {
		root := t.TempDir()
		pkgDir := filepath.Join(root, "build")
		for _, d := range []string{filepath.Join(root, "package.json"), pkgDir} {
			if err := os.Mkdir(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		writeCorpusFile(t, filepath.Join(pkgDir, "b.go"), src)
		if got := analyzeFooBar(t, root).ImportFileCount; got != 1 {
			t.Errorf("ImportFileCount = %d, want 1", got)
		}
	})
}

// TestAnalyzer_LargeFileImportsCounted verifies that a file over
// largeFileBytes still counts as an importer, so a dependency imported only
// there is not reported unused (#546), while its calls are not counted. A file
// of exactly largeFileBytes is still parsed in full. The body repeats a call
// statement so the largeFilePrefixBytes cut lands in the middle of one: the
// import must still be found when the parsed prefix ends in broken syntax.
func TestAnalyzer_LargeFileImportsCounted(t *testing.T) {
	const head = "package gen\nimport \"github.com/foo/bar\"\nfunc F() { bar.Do() }\n"
	const line = "var _ = bar.Do()\n"
	if (largeFilePrefixBytes-len(head))%len(line) == 0 {
		t.Fatal("the prefix cut must land mid-statement; adjust head or line")
	}
	tests := []struct {
		name      string
		size      int
		fullParse bool
	}{
		{"exactly largeFileBytes is parsed in full", largeFileBytes, true},
		{"largeFileBytes+1 is parsed for imports only", largeFileBytes + 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := (tt.size - len(head)) / len(line)
			content := head + strings.Repeat(line, lines)
			content += strings.Repeat("\n", tt.size-len(content))
			dir := t.TempDir()
			writeCorpusFile(t, filepath.Join(dir, "gen.pb.go"), content)
			got := analyzeFooBar(t, dir)
			if got.ImportFileCount != 1 || got.IsUnused {
				t.Errorf("ImportFileCount = %d, IsUnused = %v; want 1, false", got.ImportFileCount, got.IsUnused)
			}
			wantCalls := 0
			if tt.fullParse {
				wantCalls = 1 + lines
			}
			if got.CallSiteCount != wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", got.CallSiteCount, wantCalls)
			}
		})
	}
}

func TestAnalyzer_NoMatchingImports(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import "fmt"

func main() { fmt.Println("hi") }
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	t.Cleanup(analyzer.Close)
	importPaths := map[string][]string{
		"pkg:golang/github.com/foo/bar@v1.0.0": {"github.com/foo/bar"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 0 {
		t.Errorf("expected no results for unmatched imports, got %d", len(result))
	}
}

func TestAnalyzer_ImportToPURLCollision(t *testing.T) {
	// When two PURLs (different versions of the same library) generate identical
	// import path candidates, both PURLs must receive coupling data.
	// This is the bug described in issue #180: last-write-wins causes
	// non-deterministic PURL assignment.
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import com.google.gson.Gson;

public class Main {
    public static void main(String[] args) {
        Gson gson = new Gson();
        String json = gson.toJson("hello");
    }
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	t.Cleanup(analyzer.Close)
	// Two versions of gson generating the same import path candidates.
	importPaths := map[string][]string{
		"pkg:maven/com.google.code.gson/gson@2.10.1": {"com.google.gson"},
		"pkg:maven/com.google.code.gson/gson@2.8.9":  {"com.google.gson"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	// Both PURLs must have coupling data — neither should be silently dropped.
	for purl := range importPaths {
		ca, ok := result[purl]
		if !ok {
			t.Errorf("missing coupling analysis for %s (collision dropped it)", purl)
			continue
		}
		if ca.ImportFileCount != 1 {
			t.Errorf("%s: ImportFileCount = %d, want 1", purl, ca.ImportFileCount)
		}
		// 3 call sites: Gson local var type (1) + new Gson() (1) + gson.toJson (1)
		if ca.CallSiteCount != 3 {
			t.Errorf("%s: CallSiteCount = %d, want 3 (Gson type decl, new Gson, toJson)", purl, ca.CallSiteCount)
		}
	}
}

func TestAnalyzer_ImportToPURLCollision_DuplicateImportPath(t *testing.T) {
	// Tests that duplicate import-path candidates from different PURLs are handled correctly.
	// Uses Go syntax, but the scenario is ecosystem-agnostic: two PURLs map to the same path.
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import "github.com/foo/bar"

func main() {
	bar.DoSomething()
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	t.Cleanup(analyzer.Close)
	importPaths := map[string][]string{
		"pkg:golang/github.com/foo/bar@v1.0.0": {"github.com/foo/bar"},
		"pkg:golang/github.com/foo/bar@v2.0.0": {"github.com/foo/bar"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	for purl := range importPaths {
		ca, ok := result[purl]
		if !ok {
			t.Errorf("missing coupling analysis for %s (collision dropped it)", purl)
			continue
		}
		if ca.ImportFileCount != 1 {
			t.Errorf("%s: ImportFileCount = %d, want 1", purl, ca.ImportFileCount)
		}
		if ca.CallSiteCount != 1 {
			t.Errorf("%s: CallSiteCount = %d, want 1", purl, ca.CallSiteCount)
		}
	}
}

// TestMergeAccumulators exercises the worker-pool merge directly: the parallel
// AnalyzeCoupling benchmarks only ever assert a non-empty result, which can't
// tell a correct union apart from one that silently dropped a file, a symbol,
// or a flag contributed by a different worker.
func TestMergeAccumulators(t *testing.T) {
	tests := []struct {
		name     string
		partials []map[string]*accumulator
		want     map[string]*accumulator
	}{
		{
			name:     "no partials",
			partials: nil,
			want:     map[string]*accumulator{},
		},
		{
			name: "single partial, single PURL",
			partials: []map[string]*accumulator{
				{"pkg:golang/a@1": {importFiles: map[string]bool{"a.go": true}, callSites: 2, symbols: map[string]bool{"Foo": true}}},
			},
			want: map[string]*accumulator{
				"pkg:golang/a@1": {importFiles: map[string]bool{"a.go": true}, callSites: 2, symbols: map[string]bool{"Foo": true}},
			},
		},
		{
			name: "disjoint PURLs across workers are not merged into each other",
			partials: []map[string]*accumulator{
				{"pkg:golang/a@1": {importFiles: map[string]bool{"a.go": true}, callSites: 1, symbols: map[string]bool{}}},
				{"pkg:golang/b@1": {importFiles: map[string]bool{"b.go": true}, callSites: 3, symbols: map[string]bool{}}},
			},
			want: map[string]*accumulator{
				"pkg:golang/a@1": {importFiles: map[string]bool{"a.go": true}, callSites: 1, symbols: map[string]bool{}},
				"pkg:golang/b@1": {importFiles: map[string]bool{"b.go": true}, callSites: 3, symbols: map[string]bool{}},
			},
		},
		{
			name: "same PURL seen by two workers: file/symbol sets union, call sites sum, flags OR",
			partials: []map[string]*accumulator{
				{
					"pkg:golang/a@1": {
						importFiles:  map[string]bool{"a.go": true},
						callSites:    2,
						symbols:      map[string]bool{"Foo": true},
						hasDotImport: true,
					},
				},
				{
					"pkg:golang/a@1": {
						importFiles:       map[string]bool{"b.go": true},
						callSites:         5,
						symbols:           map[string]bool{"Bar": true},
						hasWildcardImport: true,
					},
				},
			},
			want: map[string]*accumulator{
				"pkg:golang/a@1": {
					importFiles:       map[string]bool{"a.go": true, "b.go": true},
					callSites:         7,
					symbols:           map[string]bool{"Foo": true, "Bar": true},
					hasDotImport:      true,
					hasWildcardImport: true,
				},
			},
		},
		{
			name: "same PURL seen by three workers: union and sum accumulate across all of them",
			partials: []map[string]*accumulator{
				{"pkg:golang/a@1": {importFiles: map[string]bool{"a.go": true}, callSites: 1, symbols: map[string]bool{"Foo": true}}},
				{"pkg:golang/a@1": {importFiles: map[string]bool{"b.go": true}, callSites: 1, symbols: map[string]bool{"Bar": true}}},
				{"pkg:golang/a@1": {importFiles: map[string]bool{"a.go": true}, callSites: 1, symbols: map[string]bool{"Foo": true}, hasBlankImport: true}},
			},
			want: map[string]*accumulator{
				"pkg:golang/a@1": {
					importFiles:    map[string]bool{"a.go": true, "b.go": true},
					callSites:      3,
					symbols:        map[string]bool{"Foo": true, "Bar": true},
					hasBlankImport: true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeAccumulators(tt.partials)
			if len(got) != len(tt.want) {
				t.Fatalf("mergeAccumulators() returned %d PURLs, want %d", len(got), len(tt.want))
			}
			for purl, wantAcc := range tt.want {
				gotAcc, ok := got[purl]
				if !ok {
					t.Fatalf("missing merged accumulator for %s", purl)
				}
				if !maps.Equal(gotAcc.importFiles, wantAcc.importFiles) {
					t.Errorf("%s: importFiles = %v, want %v", purl, gotAcc.importFiles, wantAcc.importFiles)
				}
				if !maps.Equal(gotAcc.symbols, wantAcc.symbols) {
					t.Errorf("%s: symbols = %v, want %v", purl, gotAcc.symbols, wantAcc.symbols)
				}
				if gotAcc.callSites != wantAcc.callSites {
					t.Errorf("%s: callSites = %d, want %d", purl, gotAcc.callSites, wantAcc.callSites)
				}
				if gotAcc.hasDotImport != wantAcc.hasDotImport {
					t.Errorf("%s: hasDotImport = %v, want %v", purl, gotAcc.hasDotImport, wantAcc.hasDotImport)
				}
				if gotAcc.hasBlankImport != wantAcc.hasBlankImport {
					t.Errorf("%s: hasBlankImport = %v, want %v", purl, gotAcc.hasBlankImport, wantAcc.hasBlankImport)
				}
				if gotAcc.hasWildcardImport != wantAcc.hasWildcardImport {
					t.Errorf("%s: hasWildcardImport = %v, want %v", purl, gotAcc.hasWildcardImport, wantAcc.hasWildcardImport)
				}
			}
		})
	}
}

// analyzeFooBar analyzes root for the single Go dependency github.com/foo/bar
// and returns its result, failing the test when there is none.
func analyzeFooBar(t *testing.T, root string) *domaindiet.CouplingAnalysis {
	t.Helper()
	const purl = "pkg:golang/github.com/foo/bar@v1.0.0"
	analyzer := NewAnalyzer()
	t.Cleanup(analyzer.Close)
	result, err := analyzer.AnalyzeCoupling(context.Background(), root, map[string][]string{purl: {"github.com/foo/bar"}})
	if err != nil {
		t.Fatal(err)
	}
	got := result[purl]
	if got == nil {
		t.Fatalf("no result for %s", purl)
	}
	return got
}
