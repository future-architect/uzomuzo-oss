//go:build cgo

package treesitter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
				if !mapsEqual(gotAcc.importFiles, wantAcc.importFiles) {
					t.Errorf("%s: importFiles = %v, want %v", purl, gotAcc.importFiles, wantAcc.importFiles)
				}
				if !mapsEqual(gotAcc.symbols, wantAcc.symbols) {
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

func mapsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
