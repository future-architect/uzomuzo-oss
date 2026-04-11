//go:build cgo

package treesitter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
		if ca.CallSiteCount != 2 {
			t.Errorf("%s: CallSiteCount = %d, want 2 (new Gson, toJson)", purl, ca.CallSiteCount)
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
