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

func TestAnalyzer_GoBlankImportBaselineCallSite(t *testing.T) {
	// Regression test for #261: blank imports should get CallSiteCount=1
	// as a baseline (mirrors the existing dot-import behavior). Before the
	// fix, blank imports had CallSiteCount=0, causing them to be scored as
	// "imported but no calls" even though they have no callable API.
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	_ "github.com/lib/pq"
	"github.com/foo/bar"
)

func main() {
	bar.Do()
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:golang/github.com/lib/pq@v1.10.0": {"github.com/lib/pq"},
		"pkg:golang/github.com/foo/bar@v1.0.0": {"github.com/foo/bar"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	caPQ, ok := result["pkg:golang/github.com/lib/pq@v1.10.0"]
	if !ok {
		t.Fatal("expected coupling analysis for blank import pq")
	}
	if !caPQ.HasBlankImport {
		t.Error("pq: HasBlankImport = false, want true")
	}
	if caPQ.CallSiteCount != 1 {
		t.Errorf("pq: CallSiteCount = %d, want 1 (blank import baseline)", caPQ.CallSiteCount)
	}
	if caPQ.IsUnused {
		t.Error("pq: IsUnused = true, want false")
	}

	// Verify regular import still works normally alongside blank import.
	caBar, ok := result["pkg:golang/github.com/foo/bar@v1.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for regular import bar")
	}
	if caBar.CallSiteCount != 1 {
		t.Errorf("bar: CallSiteCount = %d, want 1", caBar.CallSiteCount)
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

func TestAnalyzer_GoSuffixedModuleNames(t *testing.T) {
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
			name: ".go suffix stripped (miscreant.go)",
			code: `package main

import "github.com/miscreant/miscreant.go"

func main() {
	miscreant.NewAEAD("AES-SIV", nil)
	miscreant.NewAEAD("AES-PMAC-SIV", nil)
}
`,
			importPaths: map[string][]string{
				"pkg:golang/github.com/miscreant/miscreant.go@v0.3.0": {"github.com/miscreant/miscreant.go"},
			},
			purl:        "pkg:golang/github.com/miscreant/miscreant.go@v0.3.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 1,
		},
		{
			name: "-golang suffix stripped (geoip2-golang)",
			code: `package main

import "github.com/oschwald/geoip2-golang"

func main() {
	geoip2.Open("test.mmdb")
	geoip2.FromBytes(nil)
	geoip2.Open("test2.mmdb")
}
`,
			importPaths: map[string][]string{
				"pkg:golang/github.com/oschwald/geoip2-golang@v1.9.0": {"github.com/oschwald/geoip2-golang"},
			},
			purl:        "pkg:golang/github.com/oschwald/geoip2-golang@v1.9.0",
			wantImports: 1,
			wantCalls:   3,
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

func TestGoAliasFromImportPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"github.com/miscreant/miscreant.go", "miscreant"},       // .go suffix stripped
		{"github.com/oschwald/geoip2-golang", "geoip2"},          // -golang suffix stripped
		{"github.com/stretchr/testify", "testify"},               // normal path
		{"example.com/foo/v2", "foo"},                            // major version peeled
		{"gopkg.in/yaml.v3", "yaml"},                             // gopkg.in version stripped
		{"gopkg.in/foo.go.v2", "foo"},                            // gopkg.in + .go suffix
		{"github.com/go-redis/redis/v9", "redis"},                // major version + go- prefix
		{"github.com/opentracing/opentracing-go", "opentracing"}, // -go suffix stripped
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := goAliasFromImportPath(tt.input)
			if got != tt.want {
				t.Errorf("goAliasFromImportPath(%q) = %q, want %q", tt.input, got, tt.want)
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
		{"geoip2-golang", "geoip2"},
		{"maxminddb-golang", "maxminddb"},
		{"foo-golang", "foo"},
		{"-golang", "-golang"},       // empty after strip; guard preserves original
		{"-go", "-go"},               // empty after strip; guard preserves original
		{"go-", "go-"},               // empty after prefix strip; guard preserves original
		{"go-golang", "go"},          // strip -golang -> "go" (no hyphens)
		{"go-redis", "redis"},        // real package: prefix go- stripped
		{"go-sqlite3", "sqlite3"},    // real package: prefix go- stripped
		{"foo-bar-golang", "foobar"}, // -golang stripped, remaining hyphen removed
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
