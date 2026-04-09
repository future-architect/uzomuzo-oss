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
	wantSymbols := []string{"DoOther", "DoSomething"}
	if len(ca.Symbols) != len(wantSymbols) {
		t.Errorf("Symbols = %v, want %v", ca.Symbols, wantSymbols)
	} else {
		for i, s := range ca.Symbols {
			if s != wantSymbols[i] {
				t.Errorf("Symbols[%d] = %q, want %q", i, s, wantSymbols[i])
			}
		}
	}
	if ca.HasBlankImport {
		t.Error("HasBlankImport = true, want false")
	}
	if ca.HasDotImport {
		t.Error("HasDotImport = true, want false")
	}
	if ca.HasWildcardImport {
		t.Error("HasWildcardImport = true, want false")
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

func TestAnalyzer_GoBlankAndDotImport(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	_ "github.com/lib/pq"
	. "github.com/onsi/gomega"
	"github.com/foo/bar"
)

func main() {
	Expect(true).To(BeTrue())
	bar.Do()
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:golang/github.com/lib/pq@v1.10.0":     {"github.com/lib/pq"},
		"pkg:golang/github.com/onsi/gomega@v1.0.0": {"github.com/onsi/gomega"},
		"pkg:golang/github.com/foo/bar@v1.0.0":     {"github.com/foo/bar"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	// Blank import: has_blank_import=true, is not unused
	caPQ, ok := result["pkg:golang/github.com/lib/pq@v1.10.0"]
	if !ok {
		t.Fatal("expected coupling analysis for blank import pq")
	}
	if !caPQ.HasBlankImport {
		t.Error("pq: HasBlankImport = false, want true")
	}
	if caPQ.HasDotImport {
		t.Error("pq: HasDotImport = true, want false")
	}
	if caPQ.HasWildcardImport {
		t.Error("pq: HasWildcardImport = true, want false")
	}
	if caPQ.IsUnused {
		t.Error("pq: IsUnused = true, want false (blank import is intentional)")
	}

	// Dot import: has_dot_import=true, is not unused, has baseline call sites
	caGomega, ok := result["pkg:golang/github.com/onsi/gomega@v1.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for dot import gomega")
	}
	if !caGomega.HasDotImport {
		t.Error("gomega: HasDotImport = false, want true")
	}
	if caGomega.HasBlankImport {
		t.Error("gomega: HasBlankImport = true, want false")
	}
	if caGomega.IsUnused {
		t.Error("gomega: IsUnused = true, want false (dot import is used)")
	}
	if caGomega.CallSiteCount < 1 {
		t.Errorf("gomega: CallSiteCount = %d, want >= 1 (dot import baseline)", caGomega.CallSiteCount)
	}

	// Regular import: no special flags
	caBar, ok := result["pkg:golang/github.com/foo/bar@v1.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for regular import bar")
	}
	if caBar.HasBlankImport {
		t.Error("bar: HasBlankImport = true, want false")
	}
	if caBar.HasDotImport {
		t.Error("bar: HasDotImport = true, want false")
	}
	if caBar.HasWildcardImport {
		t.Error("bar: HasWildcardImport = true, want false")
	}
	wantSymbols := []string{"Do"}
	if len(caBar.Symbols) != 1 || caBar.Symbols[0] != "Do" {
		t.Errorf("bar: Symbols = %v, want %v", caBar.Symbols, wantSymbols)
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
	if ca.IsUnused {
		t.Error("IsUnused = true, want false (package is imported)")
	}
	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	if ca.CallSiteCount != 0 {
		t.Errorf("CallSiteCount = %d, want 0", ca.CallSiteCount)
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

func TestAnalyzer_JavaScript(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(`import axios from "axios";
import { readFile } from "fs";

axios.get("https://example.com");
axios.post("https://example.com");
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/axios@1.6.0": {"axios"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:npm/axios@1.6.0"]
	if !ok {
		t.Fatal("expected coupling analysis for axios")
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

func TestAnalyzer_TypeScript(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte(`import axios from "axios";

const res = axios.get("https://example.com");
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/axios@1.6.0": {"axios"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:npm/axios@1.6.0"]
	if !ok {
		t.Fatal("expected coupling analysis for axios")
	}

	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	if ca.CallSiteCount != 1 {
		t.Errorf("CallSiteCount = %d, want 1", ca.CallSiteCount)
	}
}

func TestAnalyzer_TSXJSXComponentUsage(t *testing.T) {
	dir := t.TempDir()
	// Mix self-closing tags (<Camera />, <Camera />) and non-self-closing
	// tags (<ArrowRight>...</ArrowRight>) to cover both jsx_self_closing_element
	// and jsx_opening_element patterns. The closing tag should NOT be counted
	// as a separate call site.
	err := os.WriteFile(filepath.Join(dir, "App.tsx"), []byte(`import { Camera, ArrowRight } from "lucide-react";

function App() {
  return (
    <div>
      <Camera size={24} />
      <ArrowRight className="h-5">
        <span>Next</span>
      </ArrowRight>
      <Camera />
    </div>
  );
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/lucide-react@0.300.0": {"lucide-react"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:npm/lucide-react@0.300.0"]
	if !ok {
		t.Fatal("expected coupling analysis for lucide-react")
	}

	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	// 3 JSX usages: <Camera .../>, <ArrowRight>...</ArrowRight>, <Camera />
	// The non-self-closing <ArrowRight> counts once (opening element only).
	if ca.CallSiteCount != 3 {
		t.Errorf("CallSiteCount = %d, want 3", ca.CallSiteCount)
	}
	if ca.APIBreadth != 2 {
		t.Errorf("APIBreadth = %d, want 2", ca.APIBreadth)
	}
	wantSymbols := []string{"ArrowRight", "Camera"}
	sort.Strings(ca.Symbols)
	if len(ca.Symbols) != len(wantSymbols) {
		t.Errorf("Symbols = %v, want %v", ca.Symbols, wantSymbols)
	} else {
		for i, s := range ca.Symbols {
			if s != wantSymbols[i] {
				t.Errorf("Symbols[%d] = %q, want %q", i, s, wantSymbols[i])
			}
		}
	}
}

func TestAnalyzer_JSXComponentInJSX(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "App.jsx"), []byte(`import { HiArrowUp } from "react-icons/hi";

function App() {
  return <HiArrowUp className="icon" />;
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/react-icons@4.0.0": {"react-icons"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:npm/react-icons@4.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for react-icons")
	}

	if ca.CallSiteCount != 1 {
		t.Errorf("CallSiteCount = %d, want 1", ca.CallSiteCount)
	}
	if ca.APIBreadth != 1 {
		t.Errorf("APIBreadth = %d, want 1", ca.APIBreadth)
	}
}

func TestAnalyzer_Java(t *testing.T) {
	dir := t.TempDir()
	// Java variable-declaration resolution: "Gson gson = new Gson()" should
	// allow "gson.toJson()" to be counted as a call site for the Gson import.
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import com.google.gson.Gson;

public class Main {
    public static void main(String[] args) {
        Gson gson = new Gson();
        String json = gson.toJson("hello");
        String json2 = gson.fromJson("{}", String.class);
    }
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/com.google.code.gson/gson@2.10": {"com.google.gson"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:maven/com.google.code.gson/gson@2.10"]
	if !ok {
		t.Fatal("expected coupling analysis for gson")
	}

	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	if ca.CallSiteCount != 2 {
		t.Errorf("CallSiteCount = %d, want 2", ca.CallSiteCount)
	}
	if ca.APIBreadth != 2 {
		t.Errorf("APIBreadth = %d, want 2 (toJson, fromJson)", ca.APIBreadth)
	}
	if ca.IsUnused {
		t.Error("IsUnused = true, want false")
	}
}

func TestAnalyzer_JavaStaticCall(t *testing.T) {
	dir := t.TempDir()
	// Static calls use the class name directly (e.g., StringUtils.isBlank).
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import org.apache.commons.lang3.StringUtils;

public class Main {
    public static void main(String[] args) {
        boolean b = StringUtils.isBlank("");
    }
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/org.apache.commons/commons-lang3@3.14": {"org.apache.commons.lang3"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:maven/org.apache.commons/commons-lang3@3.14"]
	if !ok {
		t.Fatal("expected coupling analysis for commons-lang3")
	}

	if ca.CallSiteCount != 1 {
		t.Errorf("CallSiteCount = %d, want 1", ca.CallSiteCount)
	}
}

func TestAnalyzer_JavaStaticImport(t *testing.T) {
	dir := t.TempDir()
	// Static imports bring individual methods/fields into scope without qualification.
	// "import static org.junit.Assert.assertEquals" allows bare "assertEquals()" calls.
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import org.junit.Test;

public class Main {
    @Test
    public void testSomething() {
        assertEquals("hello", "hello");
        assertEquals(42, 42);
        assertTrue(true);
    }
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/junit/junit@4.13.2": {"org.junit"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:maven/junit/junit@4.13.2"]
	if !ok {
		t.Fatal("expected coupling analysis for junit")
	}

	// The fixture has 3 relevant imported symbols: 2 static (assertEquals, assertTrue)
	// and 1 regular (Test), all declared in the same file, so ImportFileCount = 1.
	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	// 4 call sites: assertEquals() x2 + assertTrue() x1 + @Test x1
	if ca.CallSiteCount != 4 {
		t.Errorf("CallSiteCount = %d, want 4", ca.CallSiteCount)
	}
	// 3 distinct symbols: assertEquals, assertTrue, Test (annotation)
	if ca.APIBreadth != 3 {
		t.Errorf("APIBreadth = %d, want 3 (assertEquals, assertTrue, Test)", ca.APIBreadth)
	}
	if ca.IsUnused {
		t.Error("IsUnused = true, want false")
	}
}

func TestAnalyzer_GoCaseInsensitivePURL(t *testing.T) {
	dir := t.TempDir()
	// Source code uses mixed-case import path (as authored by the module owner).
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import "github.com/Masterminds/semver/v3"

func main() {
	semver.NewVersion("1.0.0")
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	// SBOM/PURL uses lowercased namespace (PURL spec normalizes to lowercase).
	importPaths := map[string][]string{
		"pkg:golang/github.com/masterminds/semver/v3@v3.4.0": {"github.com/masterminds/semver/v3"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:golang/github.com/masterminds/semver/v3@v3.4.0"]
	if !ok {
		t.Fatal("expected coupling analysis for case-mismatched PURL")
	}

	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	if ca.CallSiteCount != 1 {
		t.Errorf("CallSiteCount = %d, want 1", ca.CallSiteCount)
	}
	if ca.IsUnused {
		t.Error("IsUnused = true, want false")
	}
}

func TestAnalyzer_JSCaseInsensitivePURL(t *testing.T) {
	dir := t.TempDir()
	// Source code uses mixed-case import path, but PURL (and hence importToPURL key)
	// is lowercased. The lookup must be case-insensitive.
	err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte(`import MyLib from "MyLib";

MyLib.doSomething();
MyLib.doOther();
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/mylib@1.0.0": {"mylib"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:npm/mylib@1.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for case-mismatched JS import")
	}

	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	if ca.CallSiteCount != 2 {
		t.Errorf("CallSiteCount = %d, want 2", ca.CallSiteCount)
	}
	if ca.IsUnused {
		t.Error("IsUnused = true, want false")
	}
}

func TestAnalyzer_JavaCaseInsensitivePURL(t *testing.T) {
	dir := t.TempDir()
	// Java import paths are case-sensitive, but the PURL-derived importToPURL
	// keys are lowercased. Lookup must be case-insensitive.
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import com.Google.Gson.Gson;

public class Main {
    public static void main(String[] args) {
        Gson g = new Gson();
        g.toJson("test");
    }
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/com.google.code.gson/gson@2.10": {"com.google.gson"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:maven/com.google.code.gson/gson@2.10"]
	if !ok {
		t.Fatal("expected coupling analysis for case-mismatched Java import")
	}

	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
}

func TestAnalyzer_JavaScriptScopedDefaultImport(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(`import cloud from "@strapi/plugin-cloud";

cloud.deploy();
cloud.status();
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/%40strapi/plugin-cloud@1.0.0": {"@strapi/plugin-cloud"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:npm/%40strapi/plugin-cloud@1.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for @strapi/plugin-cloud")
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

func TestAnalyzer_TypeScriptScopedNamespaceImport(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte(`import * as S3 from "@aws-sdk/client-s3";

S3.GetObjectCommand();
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/%40aws-sdk/client-s3@3.0.0": {"@aws-sdk/client-s3"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:npm/%40aws-sdk/client-s3@3.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for @aws-sdk/client-s3")
	}
	if ca.CallSiteCount != 1 {
		t.Errorf("CallSiteCount = %d, want 1", ca.CallSiteCount)
	}
}

func TestAnalyzer_JavaScriptScopedRequire(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(`const cloud = require("@strapi/plugin-cloud");

cloud.deploy();
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/%40strapi/plugin-cloud@1.0.0": {"@strapi/plugin-cloud"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:npm/%40strapi/plugin-cloud@1.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for @strapi/plugin-cloud (CJS)")
	}
	if ca.CallSiteCount != 1 {
		t.Errorf("CallSiteCount = %d, want 1", ca.CallSiteCount)
	}
}

func TestAnalyzer_TypeScriptTypeOnlyImport(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte(`import type { Foo } from "@scope/pkg";

// No runtime usage — should not count
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/%40scope/pkg@1.0.0": {"@scope/pkg"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 0 {
		t.Errorf("expected no coupling for type-only import, got %d results", len(result))
	}
}

func TestAnalyzer_JavaScriptNamedImport(t *testing.T) {
	dir := t.TempDir()
	// Named imports bring individual bindings into scope.
	// "import { useState, useEffect } from 'react'" allows bare calls like useState().
	err := os.WriteFile(filepath.Join(dir, "app.js"), []byte(`import { useState, useEffect, useCallback } from "react";
import axios from "axios";

const [count, setCount] = useState(0);
useEffect(() => { console.log("mounted"); });
useCallback(() => {}, []);

axios.get("https://api.example.com");
axios.post("https://api.example.com");
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/react@18.2.0": {"react"},
		"pkg:npm/axios@1.6.0":  {"axios"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	// React: 3 named import calls (useState, useEffect, useCallback)
	reactCA, ok := result["pkg:npm/react@18.2.0"]
	if !ok {
		t.Fatal("expected coupling analysis for react")
	}
	if reactCA.ImportFileCount != 1 {
		t.Errorf("react ImportFileCount = %d, want 1", reactCA.ImportFileCount)
	}
	if reactCA.CallSiteCount != 3 {
		t.Errorf("react CallSiteCount = %d, want 3", reactCA.CallSiteCount)
	}
	if reactCA.APIBreadth != 3 {
		t.Errorf("react APIBreadth = %d, want 3 (useState, useEffect, useCallback)", reactCA.APIBreadth)
	}

	// Axios: 2 member calls (axios.get, axios.post) — regression check for default imports
	axiosCA, ok := result["pkg:npm/axios@1.6.0"]
	if !ok {
		t.Fatal("expected coupling analysis for axios")
	}
	if axiosCA.CallSiteCount != 2 {
		t.Errorf("axios CallSiteCount = %d, want 2", axiosCA.CallSiteCount)
	}
}

func TestAnalyzer_JavaScriptAliasedNamedImport(t *testing.T) {
	dir := t.TempDir()
	// Aliased named import: import { x as y } should register "y", not "x".
	err := os.WriteFile(filepath.Join(dir, "app.js"), []byte(`import { useEffect as ue } from "react";

ue(() => {});
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/react@18.2.0": {"react"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:npm/react@18.2.0"]
	if !ok {
		t.Fatal("expected coupling analysis for react")
	}
	if ca.CallSiteCount != 1 {
		t.Errorf("CallSiteCount = %d, want 1", ca.CallSiteCount)
	}
}

func TestAnalyzer_JavaScriptCombinedDefaultAndNamespaceImport(t *testing.T) {
	dir := t.TempDir()
	// Combined import: both default and namespace bindings should be registered.
	err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(`import cloud, * as cloudNS from "@strapi/plugin-cloud";

cloud.deploy();
cloudNS.status();
cloudNS.teardown();
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/%40strapi/plugin-cloud@1.0.0": {"@strapi/plugin-cloud"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:npm/%40strapi/plugin-cloud@1.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for @strapi/plugin-cloud")
	}
	if ca.CallSiteCount != 3 {
		t.Errorf("CallSiteCount = %d, want 3 (1 via default + 2 via namespace)", ca.CallSiteCount)
	}
	if ca.APIBreadth != 3 {
		t.Errorf("APIBreadth = %d, want 3", ca.APIBreadth)
	}
}

func TestAnalyzer_PythonFromImport(t *testing.T) {
	tests := []struct {
		name         string
		code         string
		importPaths  map[string][]string
		purl         string
		wantImports  int
		wantCalls    int
		wantBreadth  int
		wantWildcard bool
	}{
		{
			name: "basic from-import with bare calls",
			code: `from requests import get, post

get("https://example.com")
post("https://example.com")
`,
			importPaths: map[string][]string{
				"pkg:pypi/requests@2.31.0": {"requests"},
			},
			purl:        "pkg:pypi/requests@2.31.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
		},
		{
			name: "mixed import and from-import styles",
			code: `import requests
from requests import get

requests.post("https://example.com")
get("https://example.com")
`,
			importPaths: map[string][]string{
				"pkg:pypi/requests@2.31.0": {"requests"},
			},
			purl:        "pkg:pypi/requests@2.31.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
		},
		{
			name: "aliased from-import",
			code: `from os.path import join as pjoin

pjoin("a", "b")
`,
			importPaths: map[string][]string{
				"pkg:pypi/os-path@1.0.0": {"os.path"},
			},
			purl:        "pkg:pypi/os-path@1.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "from-import without calls is not unused",
			code: `from sqlmodel import Session

x = Session
`,
			importPaths: map[string][]string{
				"pkg:pypi/sqlmodel@0.0.8": {"sqlmodel"},
			},
			purl:        "pkg:pypi/sqlmodel@0.0.8",
			wantImports: 1,
			wantCalls:   0,
			wantBreadth: 0,
		},
		{
			name: "from-import with multiple calls",
			code: `from inline_snapshot import snapshot, outsource

snapshot([1, 2, 3])
snapshot("hello")
outsource("data")
`,
			importPaths: map[string][]string{
				"pkg:pypi/inline-snapshot@0.8.0": {"inline_snapshot"},
			},
			purl:        "pkg:pypi/inline-snapshot@0.8.0",
			wantImports: 1,
			wantCalls:   3,
			wantBreadth: 2,
		},
		{
			name: "aliased import statement",
			code: `import requests as r

r.get("https://example.com")
r.post("https://example.com")
`,
			importPaths: map[string][]string{
				"pkg:pypi/requests@2.31.0": {"requests"},
			},
			purl:        "pkg:pypi/requests@2.31.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
		},
		{
			name: "wildcard from-import records import file",
			code: `from flask import *

app = Flask(__name__)
`,
			importPaths: map[string][]string{
				"pkg:pypi/flask@3.0.0": {"flask"},
			},
			purl:         "pkg:pypi/flask@3.0.0",
			wantImports:  1,
			wantCalls:    0,
			wantBreadth:  0,
			wantWildcard: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(tt.code), 0644)
			if err != nil {
				t.Fatal(err)
			}

			analyzer := NewAnalyzer()
			result, err := analyzer.AnalyzeCoupling(context.Background(), dir, tt.importPaths)
			if err != nil {
				t.Fatal(err)
			}

			ca, ok := result[tt.purl]
			if !ok {
				t.Fatalf("expected coupling analysis for %s", tt.purl)
			}

			if ca.ImportFileCount != tt.wantImports {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImports)
			}
			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}
			if ca.HasWildcardImport != tt.wantWildcard {
				t.Errorf("HasWildcardImport = %v, want %v", ca.HasWildcardImport, tt.wantWildcard)
			}
		})
	}
}

func TestAnalyzer_PythonPrefixNoFalseMatch(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(`import requests
import request

requests.get("https://example.com")
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:pypi/requests@2.31.0": {"requests"},
		"pkg:pypi/request@1.0.0":   {"request"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	// "requests" should match pkg:pypi/requests, not pkg:pypi/request
	caRequests, ok := result["pkg:pypi/requests@2.31.0"]
	if !ok {
		t.Fatal("expected coupling analysis for requests")
	}
	if caRequests.CallSiteCount != 1 {
		t.Errorf("requests CallSiteCount = %d, want 1", caRequests.CallSiteCount)
	}

	// "request" should be a separate entry with no call sites but still imported
	caRequest, ok := result["pkg:pypi/request@1.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for request")
	}
	if caRequest.IsUnused {
		t.Error("request should not be unused (it is imported)")
	}
	if caRequest.CallSiteCount != 0 {
		t.Errorf("request CallSiteCount = %d, want 0", caRequest.CallSiteCount)
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
		if ca.CallSiteCount != 1 {
			t.Errorf("%s: CallSiteCount = %d, want 1", purl, ca.CallSiteCount)
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

func TestAnalyzer_JSRequireCompoundPatterns(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		code        string
		importPaths map[string][]string
		purl        string
		wantImports int
		wantCalls   int
		wantBreadth int
	}{
		{
			name:     "ES default import with property access",
			filename: "index.js",
			code: `import followRedirects from 'follow-redirects';

followRedirects.http;
followRedirects.https;
`,
			importPaths: map[string][]string{
				"pkg:npm/follow-redirects@1.15.0": {"follow-redirects"},
			},
			purl:        "pkg:npm/follow-redirects@1.15.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
		},
		{
			// new FormData() is counted via new_expression, FormData.prototype via member_expression.
			name:     "ES default import with new and member access",
			filename: "index.js",
			code: `import FormData from 'form-data';

new FormData();
FormData.prototype;
`,
			importPaths: map[string][]string{
				"pkg:npm/form-data@4.0.0": {"form-data"},
			},
			purl:        "pkg:npm/form-data@4.0.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
		},
		{
			name:     "require with property access",
			filename: "index.js",
			code: `const globals = require('lodash-doc-globals');

globals.use();
`,
			importPaths: map[string][]string{
				"pkg:npm/lodash-doc-globals@1.0.0": {"lodash-doc-globals"},
			},
			purl:        "pkg:npm/lodash-doc-globals@1.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name:     "require in logical OR expression",
			filename: "index.js",
			code: `var QUnit = root.QUnit || require('qunit-extras');

QUnit.test();
QUnit.module();
`,
			importPaths: map[string][]string{
				"pkg:npm/qunit-extras@1.0.0": {"qunit-extras"},
			},
			purl:        "pkg:npm/qunit-extras@1.0.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
		},
		{
			name:     "require in ternary expression",
			filename: "index.js",
			code: `var lib = typeof window !== 'undefined' ? window.lib : require('my-lib');

lib.init();
`,
			importPaths: map[string][]string{
				"pkg:npm/my-lib@1.0.0": {"my-lib"},
			},
			purl:        "pkg:npm/my-lib@1.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			err := os.WriteFile(filepath.Join(dir, tt.filename), []byte(tt.code), 0644)
			if err != nil {
				t.Fatal(err)
			}

			analyzer := NewAnalyzer()
			result, err := analyzer.AnalyzeCoupling(context.Background(), dir, tt.importPaths)
			if err != nil {
				t.Fatal(err)
			}

			ca, ok := result[tt.purl]
			if !ok {
				t.Fatalf("expected coupling analysis for %s", tt.purl)
			}

			if ca.ImportFileCount != tt.wantImports {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImports)
			}
			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}
		})
	}
}
func TestAnalyzer_GoHyphenatedPackageName(t *testing.T) {
	tests := []struct {
		name        string
		code        string
		importPaths map[string][]string
		purl        string
		wantImports int
		wantCalls   int
		wantBreadth int
	}{
		{
			name: "suffix -go stripped (opentracing-go)",
			code: `package main

import "github.com/opentracing/opentracing-go"

func main() {
	opentracing.StartSpanFromContext(nil, "test")
}
`,
			importPaths: map[string][]string{
				"pkg:golang/github.com/opentracing/opentracing-go@v1.2.0": {"github.com/opentracing/opentracing-go"},
			},
			purl:        "pkg:golang/github.com/opentracing/opentracing-go@v1.2.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "prefix go- stripped (go-loser)",
			code: `package main

import "github.com/bboreham/go-loser"

func main() {
	loser.New(nil, nil)
}
`,
			importPaths: map[string][]string{
				"pkg:golang/github.com/bboreham/go-loser@v0.0.4": {"github.com/bboreham/go-loser"},
			},
			purl:        "pkg:golang/github.com/bboreham/go-loser@v0.0.4",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "suffix -go stripped (mmap-go)",
			code: `package main

import "github.com/edsrzf/mmap-go"

func main() {
	mmap.Map(nil, 0, 0)
	_ = mmap.RDWR
}
`,
			importPaths: map[string][]string{
				"pkg:golang/github.com/edsrzf/mmap-go@v1.1.0": {"github.com/edsrzf/mmap-go"},
			},
			purl:        "pkg:golang/github.com/edsrzf/mmap-go@v1.1.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
		},
		{
			name: "prefix go- stripped (go-spew) via sub-package",
			code: `package main

import "github.com/davecgh/go-spew/spew"

func main() {
	spew.Dump("test")
}
`,
			importPaths: map[string][]string{
				"pkg:golang/github.com/davecgh/go-spew@v1.1.2": {"github.com/davecgh/go-spew"},
			},
			purl:        "pkg:golang/github.com/davecgh/go-spew@v1.1.2",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "explicit alias overrides hyphen heuristic",
			code: `package main

import ot "github.com/opentracing/opentracing-go"

func main() {
	ot.StartSpanFromContext(nil, "test")
}
`,
			importPaths: map[string][]string{
				"pkg:golang/github.com/opentracing/opentracing-go@v1.2.0": {"github.com/opentracing/opentracing-go"},
			},
			purl:        "pkg:golang/github.com/opentracing/opentracing-go@v1.2.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(tt.code), 0644); err != nil {
				t.Fatal(err)
			}

			analyzer := NewAnalyzer()
			result, err := analyzer.AnalyzeCoupling(context.Background(), dir, tt.importPaths)
			if err != nil {
				t.Fatal(err)
			}

			ca, ok := result[tt.purl]
			if !ok {
				t.Fatalf("expected coupling analysis for %s", tt.purl)
			}

			if ca.ImportFileCount != tt.wantImports {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImports)
			}
			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}
		})
	}
}

func TestAnalyzer_GoSubPackageImport(t *testing.T) {
	tests := []struct {
		name        string
		code        string
		importPaths map[string][]string
		purl        string
		wantImports int
		wantCalls   int
		wantBreadth int
	}{
		{
			name: "sub-package import uses last segment alias",
			code: `package main

import "github.com/prometheus/alertmanager/api/v2/models"

func main() {
	models.LabelSetToAPI(nil)
}
`,
			importPaths: map[string][]string{
				"pkg:golang/github.com/prometheus/alertmanager@v0.27.0": {"github.com/prometheus/alertmanager"},
			},
			purl:        "pkg:golang/github.com/prometheus/alertmanager@v0.27.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "sub-package with type references",
			code: `package main

import "github.com/prometheus/alertmanager/api/v2/models"

func main() {
	var _ models.Alert
	_ = models.Alert{}
	models.LabelSetToAPI(nil)
}
`,
			importPaths: map[string][]string{
				"pkg:golang/github.com/prometheus/alertmanager@v0.27.0": {"github.com/prometheus/alertmanager"},
			},
			purl:        "pkg:golang/github.com/prometheus/alertmanager@v0.27.0",
			wantImports: 1,
			wantCalls:   3,
			wantBreadth: 2,
		},
		{
			name: "multiple sub-package imports from same PURL",
			code: `package main

import (
	"github.com/prometheus/alertmanager/api/v2/models"
	"github.com/prometheus/alertmanager/types"
)

func main() {
	models.LabelSetToAPI(nil)
	types.ParseLabels("foo")
}
`,
			importPaths: map[string][]string{
				"pkg:golang/github.com/prometheus/alertmanager@v0.27.0": {"github.com/prometheus/alertmanager"},
			},
			purl:        "pkg:golang/github.com/prometheus/alertmanager@v0.27.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(tt.code), 0644); err != nil {
				t.Fatal(err)
			}

			analyzer := NewAnalyzer()
			result, err := analyzer.AnalyzeCoupling(context.Background(), dir, tt.importPaths)
			if err != nil {
				t.Fatal(err)
			}

			ca, ok := result[tt.purl]
			if !ok {
				t.Fatalf("expected coupling analysis for %s", tt.purl)
			}

			if ca.ImportFileCount != tt.wantImports {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImports)
			}
			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}
		})
	}
}

func TestAnalyzer_GoTypeReferences(t *testing.T) {
	tests := []struct {
		name        string
		code        string
		wantCalls   int
		wantBreadth int
	}{
		{
			name: "function call only",
			code: `package main

import "github.com/foo/bar"

func main() {
	bar.DoSomething()
}
`,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "type in variable declaration",
			code: `package main

import "github.com/foo/bar"

func main() {
	var _ bar.MyType
}
`,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "type in composite literal",
			code: `package main

import "github.com/foo/bar"

func main() {
	_ = bar.MyType{}
}
`,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "constant reference",
			code: `package main

import "github.com/foo/bar"

func main() {
	_ = bar.RDWR
}
`,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "mixed type refs and function calls",
			code: `package main

import "github.com/foo/bar"

func main() {
	var _ bar.MyType
	_ = bar.MyType{}
	bar.DoSomething()
	_ = bar.RDWR
}
`,
			wantCalls:   4,
			wantBreadth: 3,
		},
		{
			name: "type in function parameter",
			code: `package main

import "github.com/foo/bar"

func process(t bar.MyType) {}
`,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name: "type in return value",
			code: `package main

import "github.com/foo/bar"

func create() bar.MyType { return bar.MyType{} }
`,
			wantCalls:   2,
			wantBreadth: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(tt.code), 0644); err != nil {
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

			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}
		})
	}
}

func TestGoPackageFromHyphenated(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"bar", "bar"},
		{"opentracing-go", "opentracing"},
		{"mmap-go", "mmap"},
		{"go-loser", "loser"},
		{"go-spew", "spew"},
		{"go-difflib", "difflib"},
		{"color", "color"},
		{"proto-go-sql", "protogosql"},
		{"go-foo-go", "foo"},
		{"testify", "testify"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := goPackageFromHyphenated(tt.input)
			if got != tt.want {
				t.Errorf("goPackageFromHyphenated(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAnalyzer_JavaAnnotation(t *testing.T) {
	dir := t.TempDir()
	// Java annotation libraries (e.g., @Nullable, @Inject) are imported
	// but their usage via annotations was not counted as call sites.
	// This test verifies that annotations contribute to call_site_count.
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import javax.annotation.Nullable;
import com.google.inject.Inject;
import com.fasterxml.jackson.annotation.JsonProperty;

public class Main {
    @Inject
    private Service service;

    @Nullable
    public String getName() {
        return null;
    }

    @JsonProperty("name")
    public String name;
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/com.google.code.findbugs/jsr305@3.0.2":              {"javax.annotation"},
		"pkg:maven/com.google.inject/guice@5.1":                       {"com.google.inject"},
		"pkg:maven/com.fasterxml.jackson.core/jackson-annotations@2.15": {"com.fasterxml.jackson.annotation"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		purl          string
		wantImports   int
		wantCallSites int
		wantBreadth   int
	}{
		{
			name:          "marker annotation @Nullable",
			purl:          "pkg:maven/com.google.code.findbugs/jsr305@3.0.2",
			wantImports:   1,
			wantCallSites: 1,
			wantBreadth:   1,
		},
		{
			name:          "marker annotation @Inject",
			purl:          "pkg:maven/com.google.inject/guice@5.1",
			wantImports:   1,
			wantCallSites: 1,
			wantBreadth:   1,
		},
		{
			name:          "annotation with arguments @JsonProperty",
			purl:          "pkg:maven/com.fasterxml.jackson.core/jackson-annotations@2.15",
			wantImports:   1,
			wantCallSites: 1,
			wantBreadth:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ca, ok := result[tt.purl]
			if !ok {
				t.Fatalf("expected coupling analysis for %s", tt.purl)
			}
			if ca.ImportFileCount != tt.wantImports {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImports)
			}
			if ca.CallSiteCount != tt.wantCallSites {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCallSites)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}
			if ca.IsUnused {
				t.Error("IsUnused = true, want false")
			}
		})
	}
}

func TestAnalyzer_TypeScriptNamedImportCallSites(t *testing.T) {
	tests := []struct {
		name        string
		code        string
		purl        string
		pkg         string
		wantCalls   int
		wantBreadth int
		wantSymbols []string
	}{
		{
			name: "named class import with constructor",
			code: `import { Mutex } from "async-mutex";

const mutex = new Mutex();
mutex.acquire();
`,
			purl:        "pkg:npm/async-mutex@0.4.0",
			pkg:         "async-mutex",
			wantCalls:   1,
			wantBreadth: 1,
			wantSymbols: []string{"Mutex"},
		},
		{
			name: "named export import used as value",
			code: `import { ATTR_SERVICE_NAME } from "@opentelemetry/semantic-conventions";

const config = {
  [ATTR_SERVICE_NAME]: "my-service",
};
`,
			purl:        "pkg:npm/%40opentelemetry/semantic-conventions@1.0.0",
			pkg:         "@opentelemetry/semantic-conventions",
			wantCalls:   1,
			wantBreadth: 1,
			wantSymbols: []string{"ATTR_SERVICE_NAME"},
		},
		{
			name: "named class import with new and method",
			code: `import { PrismaPg } from "@prisma/adapter-pg";

const adapter = new PrismaPg(pool);
`,
			purl:        "pkg:npm/%40prisma/adapter-pg@1.0.0",
			pkg:         "@prisma/adapter-pg",
			wantCalls:   1,
			wantBreadth: 1,
			wantSymbols: []string{"PrismaPg"},
		},
		{
			name: "multiple named imports with mixed usage",
			code: `import { EventEmitter, Transform, Readable } from "stream-utils";

const emitter = new EventEmitter();
const t = new Transform();
const data = Readable.from([1, 2, 3]);
`,
			purl:        "pkg:npm/stream-utils@1.0.0",
			pkg:         "stream-utils",
			wantCalls:   3,
			wantBreadth: 3,
			// EventEmitter/Transform from new_expression, "from" from Readable.from() member_expression
			wantSymbols: []string{"EventEmitter", "Transform", "from"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "app.ts"), []byte(tt.code), 0644); err != nil {
				t.Fatal(err)
			}

			analyzer := NewAnalyzer()
			importPaths := map[string][]string{
				tt.purl: {tt.pkg},
			}
			result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
			if err != nil {
				t.Fatal(err)
			}

			ca, ok := result[tt.purl]
			if !ok {
				t.Fatalf("expected coupling analysis for %s", tt.purl)
			}
			if ca.ImportFileCount != 1 {
				t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
			}
			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}

			sort.Strings(tt.wantSymbols)
			if len(ca.Symbols) != len(tt.wantSymbols) {
				t.Errorf("Symbols = %v, want %v", ca.Symbols, tt.wantSymbols)
			} else {
				for i, s := range ca.Symbols {
					if s != tt.wantSymbols[i] {
						t.Errorf("Symbols[%d] = %q, want %q", i, s, tt.wantSymbols[i])
					}
				}
			}
		})
	}
}

// TestAnalyzer_TypeScriptDefaultImportCall verifies that a default import binding
// used as a direct function call is detected as a call site.
// e.g., `import _generate from '@babel/generator'; _generate(ast);`
func TestAnalyzer_TypeScriptDefaultImportCall(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "codegen.ts"), []byte(`import _generate from '@babel/generator';

const result = _generate(ast, { comments: true });
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/%40babel/generator@7.0.0": {"@babel/generator"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:npm/%40babel/generator@7.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for @babel/generator")
	}
	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	if ca.CallSiteCount < 1 {
		t.Errorf("CallSiteCount = %d, want >= 1", ca.CallSiteCount)
	}
}

// TestAnalyzer_TypeScriptSideEffectImport verifies that a bare side-effect import
// (`import 'reflect-metadata'`) is tracked as used (not classified as unused).
func TestAnalyzer_TypeScriptSideEffectImport(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "bootstrap.ts"), []byte(`import 'reflect-metadata';

// No bindings — this is a side-effect-only import (polyfill registration).
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/reflect-metadata@0.1.13": {"reflect-metadata"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:npm/reflect-metadata@0.1.13"]
	if !ok {
		t.Fatal("expected coupling analysis for reflect-metadata")
	}
	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	if ca.IsUnused {
		t.Error("IsUnused = true, want false (side-effect import should not be classified as unused)")
	}
	if !ca.HasBlankImport {
		t.Error("HasBlankImport = false, want true (side-effect import analogous to Go blank import)")
	}
}

// TestAnalyzer_TypeScriptTypeOnlyImportExclusion verifies that `import type { ... }`
// in a .ts file (not just .tsx) is excluded from coupling analysis.
func TestAnalyzer_TypeScriptTypeOnlyImportExclusion(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "types.ts"), []byte(`import type { Foo, Bar } from "some-lib";

// Type-only import — no runtime coupling.
const x: Foo = {} as any;
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/some-lib@1.0.0": {"some-lib"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 0 {
		t.Errorf("expected no coupling for type-only import in .ts file, got %d results", len(result))
	}
}

func TestAnalyzer_PythonTryExceptImport(t *testing.T) {
	tests := []struct {
		name            string
		code            string
		wantBlankImport bool
		wantUnused      bool
		wantImportCount int
		wantCallSites   int
	}{
		{
			name: "try/except ImportError bare import",
			code: `try:
    import cryptography
except ImportError:
    pass
`,
			wantBlankImport: true,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   0,
		},
		{
			name: "try/except ModuleNotFoundError",
			code: `try:
    import cryptography
except ModuleNotFoundError:
    raise RuntimeError("missing")
`,
			wantBlankImport: true,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   0,
		},
		{
			name: "try/except bare except",
			code: `try:
    import cryptography
except:
    pass
`,
			wantBlankImport: true,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   0,
		},
		{
			name: "try/except with from-import",
			code: `try:
    from cryptography import fernet
except ImportError:
    fernet = None
`,
			wantBlankImport: true,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   0,
		},
		{
			name: "regular import not in try/except",
			code: `import cryptography
cryptography.fernet.Fernet("key")
`,
			wantBlankImport: false,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   1,
		},
		{
			name: "try/except with unrelated exception type",
			code: `try:
    import cryptography
except ValueError:
    pass
`,
			wantBlankImport: false,
			wantUnused:      false,
			wantImportCount: 1,
			wantCallSites:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(tt.code), 0644)
			if err != nil {
				t.Fatal(err)
			}

			analyzer := NewAnalyzer()
			importPaths := map[string][]string{
				"pkg:pypi/cryptography@41.0.0": {"cryptography"},
			}
			result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
			if err != nil {
				t.Fatal(err)
			}

			ca, ok := result["pkg:pypi/cryptography@41.0.0"]
			if !ok {
				t.Fatal("expected coupling analysis for cryptography")
			}

			if ca.HasBlankImport != tt.wantBlankImport {
				t.Errorf("HasBlankImport = %v, want %v", ca.HasBlankImport, tt.wantBlankImport)
			}
			if ca.IsUnused != tt.wantUnused {
				t.Errorf("IsUnused = %v, want %v", ca.IsUnused, tt.wantUnused)
			}
			if ca.ImportFileCount != tt.wantImportCount {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImportCount)
			}
			if ca.CallSiteCount != tt.wantCallSites {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCallSites)
			}
		})
	}
}
