//go:build cgo

package treesitter

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestAnalyzer_Go(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	"fmt"
	"github.com/foo/bar"
)

func main() {
	bar.DoSomething()
	bar.DoOther()
	fmt.Println("hello")
}
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

	ca, ok := result["pkg:golang/github.com/foo/bar@v1.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for pkg:golang/github.com/foo/bar@v1.0.0")
	}

	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	if ca.CallSiteCount != 2 {
		t.Errorf("CallSiteCount = %d, want 2", ca.CallSiteCount)
	}
	if ca.APIBreadth != 2 {
		t.Errorf("APIBreadth = %d, want 2", ca.APIBreadth)
	}
	if ca.IsUnused {
		t.Error("IsUnused = true, want false")
	}
}

func TestAnalyzer_GoAliasedImport(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	baz "github.com/foo/bar"
)

func main() {
	baz.DoSomething()
}
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

	ca, ok := result["pkg:golang/github.com/foo/bar@v1.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for aliased import")
	}

	if ca.CallSiteCount != 1 {
		t.Errorf("CallSiteCount = %d, want 1", ca.CallSiteCount)
	}
}

func TestAnalyzer_GoMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []struct {
		name, content string
	}{
		{"a.go", `package main

import "github.com/foo/bar"

func a() { bar.A() }
`},
		{"b.go", `package main

import "github.com/foo/bar"

func b() { bar.B(); bar.C() }
`},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:golang/github.com/foo/bar@v1.0.0": {"github.com/foo/bar"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca := result["pkg:golang/github.com/foo/bar@v1.0.0"]
	if ca == nil {
		t.Fatal("expected result")
	}

	if ca.ImportFileCount != 2 {
		t.Errorf("ImportFileCount = %d, want 2", ca.ImportFileCount)
	}
	if ca.CallSiteCount != 3 {
		t.Errorf("CallSiteCount = %d, want 3", ca.CallSiteCount)
	}
	if ca.APIBreadth != 3 {
		t.Errorf("APIBreadth = %d, want 3", ca.APIBreadth)
	}

	sort.Strings(ca.ImportFiles)
	if len(ca.ImportFiles) != 2 {
		t.Errorf("ImportFiles len = %d, want 2", len(ca.ImportFiles))
	}
}

func TestAnalyzer_Python(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(`import requests
from os import path

requests.get("https://example.com")
requests.post("https://example.com")
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:pypi/requests@2.31.0": {"requests"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:pypi/requests@2.31.0"]
	if !ok {
		t.Fatal("expected coupling analysis for requests")
	}

	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	if ca.CallSiteCount != 2 {
		t.Errorf("CallSiteCount = %d, want 2", ca.CallSiteCount)
	}
	if ca.APIBreadth != 2 {
		t.Errorf("APIBreadth = %d, want 2", ca.APIBreadth)
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

func TestAnalyzer_UnusedImport(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import "github.com/foo/bar"

func main() {}
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

	ca := result["pkg:golang/github.com/foo/bar@v1.0.0"]
	if ca == nil {
		t.Fatal("expected result for imported but unused package")
	}
	if !ca.IsUnused {
		t.Error("IsUnused = false, want true")
	}
	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
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
