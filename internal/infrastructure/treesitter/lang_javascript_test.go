//go:build cgo

package treesitter

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

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

func TestAnalyzer_JSXMemberExpression(t *testing.T) {
	dir := t.TempDir()
	src := `import * as Icons from "lucide-react";
import Form from "react-bootstrap";

function App() {
    return (
        <div>
            <Icons.Camera size={24} />
            <Icons.Arrow>text</Icons.Arrow>
            <Form.Control type="text" />
        </div>
    );
}
`
	err := os.WriteFile(filepath.Join(dir, "App.tsx"), []byte(src), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/lucide-react@0.300.0":   {"lucide-react"},
		"pkg:npm/react-bootstrap@2.10.0": {"react-bootstrap"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	icons, ok := result["pkg:npm/lucide-react@0.300.0"]
	if !ok {
		t.Fatal("expected coupling analysis for lucide-react")
	}
	// Icons.Camera (self-closing, 1 member_expression) + Icons.Arrow (opening
	// + closing, 2 member_expression nodes) = 3 call sites via the generic
	// member_expression pattern. 2 distinct symbols: Camera, Arrow.
	if icons.CallSiteCount != 3 {
		t.Errorf("lucide-react CallSiteCount = %d, want 3", icons.CallSiteCount)
	}
	if icons.APIBreadth != 2 {
		t.Errorf("lucide-react APIBreadth = %d, want 2", icons.APIBreadth)
	}

	form, ok := result["pkg:npm/react-bootstrap@2.10.0"]
	if !ok {
		t.Fatal("expected coupling analysis for react-bootstrap")
	}
	// Form.Control (self-closing) = 1 call site, 1 symbol.
	if form.CallSiteCount != 1 {
		t.Errorf("react-bootstrap CallSiteCount = %d, want 1", form.CallSiteCount)
	}
	if form.APIBreadth != 1 {
		t.Errorf("react-bootstrap APIBreadth = %d, want 1", form.APIBreadth)
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

// TestAnalyzer_TypeScriptImportTypeExclusion verifies that all forms of TypeScript
// `import type` statements are excluded from coupling analysis. Type-only imports
// are erased at compile time and produce no runtime code, so counting them inflates
// IBNC (imports-but-no-calls). Closes #268.
func TestAnalyzer_TypeScriptImportTypeExclusion(t *testing.T) {
	tests := []struct {
		name            string
		filename        string
		code            string
		importPaths     map[string][]string
		purlToCheck     string
		wantNoResult    bool // true if we expect no coupling result
		wantImportFiles int
		wantCallSites   int
	}{
		{
			name:     "import type with named imports excluded",
			filename: "types.ts",
			code: `import type { Foo, Bar } from "some-lib";

const x: Foo = {} as any;
`,
			importPaths: map[string][]string{
				"pkg:npm/some-lib@1.0.0": {"some-lib"},
			},
			purlToCheck:  "pkg:npm/some-lib@1.0.0",
			wantNoResult: true,
		},
		{
			// Exact pattern from strapi: import type { Core, UID } from '@strapi/types'
			name:     "import type scoped package excluded (strapi pattern)",
			filename: "register.ts",
			code: `import type { Core, UID } from "@strapi/types";

// Type-only — no runtime coupling
`,
			importPaths: map[string][]string{
				"pkg:npm/%40strapi/types@1.0.0": {"@strapi/types"},
			},
			purlToCheck:  "pkg:npm/%40strapi/types@1.0.0",
			wantNoResult: true,
		},
		{
			name:     "import type default import excluded",
			filename: "types.ts",
			code: `import type Foo from "some-lib";

const x: Foo = {} as any;
`,
			importPaths: map[string][]string{
				"pkg:npm/some-lib@1.0.0": {"some-lib"},
			},
			purlToCheck:  "pkg:npm/some-lib@1.0.0",
			wantNoResult: true,
		},
		{
			// Empty type import used for module augmentation
			name:     "import type empty braces excluded",
			filename: "augment.ts",
			code: `import type {} from "@strapi/types";
`,
			importPaths: map[string][]string{
				"pkg:npm/%40strapi/types@1.0.0": {"@strapi/types"},
			},
			purlToCheck:  "pkg:npm/%40strapi/types@1.0.0",
			wantNoResult: true,
		},
		{
			// Mixed import: `import { type X, Y }` has a runtime binding Y,
			// so the import should still count.
			name:     "mixed import with inline type specifier still counted",
			filename: "app.ts",
			code: `import { type Config, createApp } from "some-framework";

createApp();
`,
			importPaths: map[string][]string{
				"pkg:npm/some-framework@1.0.0": {"some-framework"},
			},
			purlToCheck:     "pkg:npm/some-framework@1.0.0",
			wantNoResult:    false,
			wantImportFiles: 1,
			wantCallSites:   1,
		},
		{
			name:     "regular named import unaffected",
			filename: "app.ts",
			code: `import { useState } from "react";

useState(0);
`,
			importPaths: map[string][]string{
				"pkg:npm/react@18.0.0": {"react"},
			},
			purlToCheck:     "pkg:npm/react@18.0.0",
			wantNoResult:    false,
			wantImportFiles: 1,
			wantCallSites:   1,
		},
		{
			// Type-only and regular import of different packages in the same file.
			// Only the runtime import should produce coupling.
			name:     "type-only and regular import coexist in same file",
			filename: "mixed.ts",
			code: `import type { Foo } from "type-only-pkg";
import { bar } from "runtime-pkg";

bar();
`,
			importPaths: map[string][]string{
				"pkg:npm/type-only-pkg@1.0.0": {"type-only-pkg"},
				"pkg:npm/runtime-pkg@1.0.0":   {"runtime-pkg"},
			},
			purlToCheck:  "pkg:npm/type-only-pkg@1.0.0",
			wantNoResult: true,
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

			if tt.wantNoResult {
				ca, ok := result[tt.purlToCheck]
				if ok {
					t.Errorf("expected no coupling for type-only import, got ImportFileCount=%d CallSiteCount=%d",
						ca.ImportFileCount, ca.CallSiteCount)
				}

				// Verify that other (runtime) imports in the same file still produce coupling.
				for purl := range tt.importPaths {
					if purl == tt.purlToCheck {
						continue
					}
					if rca, rok := result[purl]; !rok {
						t.Errorf("expected coupling for runtime import %s, got no result", purl)
					} else if rca.ImportFileCount == 0 {
						t.Errorf("expected ImportFileCount > 0 for runtime import %s, got 0", purl)
					}
				}
				return
			}

			ca, ok := result[tt.purlToCheck]
			if !ok {
				t.Fatalf("expected coupling analysis for %s", tt.purlToCheck)
			}
			if ca.ImportFileCount != tt.wantImportFiles {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImportFiles)
			}
			if ca.CallSiteCount != tt.wantCallSites {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCallSites)
			}
		})
	}
}

// TestAnalyzer_JSConditionalRequire verifies that require() calls inside
// try/catch and switch/case blocks are correctly detected. Closes #260.
func TestAnalyzer_JSConditionalRequire(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		code        string
		importPaths map[string][]string
		purl        string
		wantImports int
		wantCalls   int
		wantBlank   bool
	}{
		{
			name:     "require inside try/catch block",
			filename: "db.js",
			code: `try {
  var pg = require('pg');
  pg.connect('postgres://localhost/mydb');
} catch(e) {
  console.log('pg not available');
}
`,
			importPaths: map[string][]string{
				"pkg:npm/pg@8.0.0": {"pg"},
			},
			purl:        "pkg:npm/pg@8.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBlank:   false,
		},
		{
			name:     "require inside switch/case block",
			filename: "driver.js",
			code: `switch (dbType) {
  case 'pg':
    var driver = require('pg');
    driver.Pool();
    break;
  case 'mysql':
    break;
}
`,
			importPaths: map[string][]string{
				"pkg:npm/pg@8.0.0": {"pg"},
			},
			purl:        "pkg:npm/pg@8.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBlank:   false,
		},
		{
			name:     "bare require inside try/catch (side-effect)",
			filename: "polyfill.js",
			code: `try {
  require('optional-polyfill');
} catch(e) {}
`,
			importPaths: map[string][]string{
				"pkg:npm/optional-polyfill@1.0.0": {"optional-polyfill"},
			},
			purl:        "pkg:npm/optional-polyfill@1.0.0",
			wantImports: 1,
			wantCalls:   1, // baseline for blank/side-effect import (#261)
			wantBlank:   true,
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
			if ca.HasBlankImport != tt.wantBlank {
				t.Errorf("HasBlankImport = %v, want %v", ca.HasBlankImport, tt.wantBlank)
			}
			if ca.IsUnused {
				t.Error("IsUnused = true, want false")
			}
		})
	}
}

// TestAnalyzer_JSBareIdentifierUsage verifies that imported constants used as
// bare identifiers (not in function calls or member expressions) are counted
// as call sites. Closes #278.
func TestAnalyzer_JSBareIdentifierUsage(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		code        string
		importPaths map[string][]string
		purl        string
		wantCalls   int
		wantBreadth int
	}{
		{
			name:     "constant in if condition",
			filename: "app.js",
			code: `import { DEV } from 'esm-env';

if (DEV) {
  console.log('dev mode');
}
`,
			importPaths: map[string][]string{
				"pkg:npm/esm-env@1.0.0": {"esm-env"},
			},
			purl:        "pkg:npm/esm-env@1.0.0",
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name:     "constant in ternary condition",
			filename: "app.js",
			code: `import { DEV } from 'esm-env';

const url = DEV ? 'http://localhost' : 'https://prod.com';
`,
			importPaths: map[string][]string{
				"pkg:npm/esm-env@1.0.0": {"esm-env"},
			},
			purl:        "pkg:npm/esm-env@1.0.0",
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name:     "constant in binary expression",
			filename: "app.js",
			code: `import { DEV } from 'esm-env';

const debug = DEV && true;
`,
			importPaths: map[string][]string{
				"pkg:npm/esm-env@1.0.0": {"esm-env"},
			},
			purl:        "pkg:npm/esm-env@1.0.0",
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name:     "constant assigned to variable",
			filename: "config.js",
			code: `import { DEV } from 'esm-env';

const isDev = DEV;
`,
			importPaths: map[string][]string{
				"pkg:npm/esm-env@1.0.0": {"esm-env"},
			},
			purl:        "pkg:npm/esm-env@1.0.0",
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name:     "constant in return statement",
			filename: "helper.js",
			code: `import { DEV } from 'esm-env';

function isDev() {
  return DEV;
}
`,
			importPaths: map[string][]string{
				"pkg:npm/esm-env@1.0.0": {"esm-env"},
			},
			purl:        "pkg:npm/esm-env@1.0.0",
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name:     "constant in assignment RHS",
			filename: "config.js",
			code: `import { DEV } from 'esm-env';

let mode;
mode = DEV;
`,
			importPaths: map[string][]string{
				"pkg:npm/esm-env@1.0.0": {"esm-env"},
			},
			purl:        "pkg:npm/esm-env@1.0.0",
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			name:     "multiple bare identifier patterns combined",
			filename: "complex.js",
			code: `import { DEV, BROWSER } from 'esm-env';

if (DEV) {
  console.log('dev');
}
const isBrowser = BROWSER;
`,
			importPaths: map[string][]string{
				"pkg:npm/esm-env@1.0.0": {"esm-env"},
			},
			purl:        "pkg:npm/esm-env@1.0.0",
			wantCalls:   2,
			wantBreadth: 2,
		},
		{
			name:     "non-imported identifier not counted",
			filename: "app.js",
			code: `import { DEV } from 'esm-env';

if (DEV) {
  console.log('dev');
}
const x = UNRELATED_CONST;
`,
			importPaths: map[string][]string{
				"pkg:npm/esm-env@1.0.0": {"esm-env"},
			},
			purl:        "pkg:npm/esm-env@1.0.0",
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

			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}
			if ca.HasBlankImport {
				t.Error("HasBlankImport = true, want false (named import, not side-effect)")
			}
			if ca.IsUnused {
				t.Error("IsUnused = true, want false")
			}
		})
	}
}

func TestAnalyzer_CJSDestructuredRequire(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		code        string
		importPaths map[string][]string
		purl        string
		wantImports int
		wantCalls   int
		wantBreadth int
		wantSymbols []string
	}{
		{
			name:     "shorthand destructuring: const { X } = require('pkg')",
			filename: "index.js",
			code: `const { RawSource, ConcatSource } = require("webpack-sources");

const x = new RawSource("hello");
`,
			importPaths: map[string][]string{
				"pkg:npm/webpack-sources@3.0.0": {"webpack-sources"},
			},
			purl:        "pkg:npm/webpack-sources@3.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
			wantSymbols: []string{"RawSource"},
		},
		{
			name:     "renamed destructuring: const { X: alias } = require('pkg')",
			filename: "index.js",
			code: `const { Tapable: tap } = require("tapable");

tap.init();
tap.run();
`,
			importPaths: map[string][]string{
				"pkg:npm/tapable@2.0.0": {"tapable"},
			},
			purl:        "pkg:npm/tapable@2.0.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
			wantSymbols: []string{"init", "run"},
		},
		{
			name:     "multiple destructured bindings with usage",
			filename: "index.js",
			code: `const { join, resolve, basename } = require("path");

const p = join("a", "b");
const abs = resolve(".");
const name = basename("/foo/bar.txt");
`,
			importPaths: map[string][]string{
				"pkg:npm/path@1.0.0": {"path"},
			},
			purl:        "pkg:npm/path@1.0.0",
			wantImports: 1,
			wantCalls:   3,
			wantBreadth: 3,
			wantSymbols: []string{"basename", "join", "resolve"},
		},
		{
			name:     "simple CJS require still works alongside destructured",
			filename: "index.js",
			code: `const axios = require("axios");

axios.get("https://example.com");
axios.post("https://example.com");
`,
			importPaths: map[string][]string{
				"pkg:npm/axios@1.6.0": {"axios"},
			},
			purl:        "pkg:npm/axios@1.6.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
			wantSymbols: []string{"get", "post"},
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

func TestAnalyzer_JSInlineRequireCallSites(t *testing.T) {
	tests := []struct {
		name         string
		filename     string
		code         string
		importPaths  map[string][]string
		purl         string
		wantImports  int
		wantCalls    int
		wantIsUnused bool
		wantBlank    bool
		wantBreadth  int
		wantNoResult bool // true if we expect no coupling result for the PURL
	}{
		{
			name:     "chained property access: require('pkg').method()",
			filename: "index.js",
			code: `var html = require('dom-serialize').serializeDocument(document);
`,
			importPaths: map[string][]string{
				"pkg:npm/dom-serialize@2.0.0": {"dom-serialize"},
			},
			purl:         "pkg:npm/dom-serialize@2.0.0",
			wantImports:  1,
			wantCalls:    1, // baseline for blank/side-effect import (#261)
			wantIsUnused: false,
			wantBlank:    true,
			wantBreadth:  0,
		},
		{
			name:     "immediate invocation: require('pkg')()",
			filename: "index.js",
			code: `require('browser-stdout')();
`,
			importPaths: map[string][]string{
				"pkg:npm/browser-stdout@1.3.1": {"browser-stdout"},
			},
			purl:         "pkg:npm/browser-stdout@1.3.1",
			wantImports:  1,
			wantCalls:    1, // baseline for blank/side-effect import (#261)
			wantIsUnused: false,
			wantBlank:    true,
			wantBreadth:  0,
		},
		{
			name:     "factory pattern: require('pkg')('arg')",
			filename: "index.js",
			code: `var deprecate = require('depd')('express');
`,
			importPaths: map[string][]string{
				"pkg:npm/depd@2.0.0": {"depd"},
			},
			purl:         "pkg:npm/depd@2.0.0",
			wantImports:  1,
			wantCalls:    1, // baseline for blank/side-effect import (#261)
			wantIsUnused: false,
			wantBlank:    true,
			wantBreadth:  0,
		},
		{
			name:     "bare side-effect require: require('pkg')",
			filename: "index.js",
			code: `require('side-effect-only');
`,
			importPaths: map[string][]string{
				"pkg:npm/side-effect-only@1.0.0": {"side-effect-only"},
			},
			purl:         "pkg:npm/side-effect-only@1.0.0",
			wantImports:  1,
			wantCalls:    1, // baseline for blank/side-effect import (#261)
			wantIsUnused: false,
			wantBlank:    true,
			wantBreadth:  0,
		},
		{
			name:     "normal require with variable binding still works",
			filename: "index.js",
			code: `const serialize = require('dom-serialize');

serialize(document);
`,
			importPaths: map[string][]string{
				"pkg:npm/dom-serialize@2.0.0": {"dom-serialize"},
			},
			purl:         "pkg:npm/dom-serialize@2.0.0",
			wantImports:  1,
			wantCalls:    1,
			wantIsUnused: false,
			wantBlank:    false,
			wantBreadth:  1,
		},
		{
			name:     "chained member access without call: require('pkg').prop",
			filename: "index.js",
			code: `var version = require('some-pkg').version;
`,
			importPaths: map[string][]string{
				"pkg:npm/some-pkg@1.0.0": {"some-pkg"},
			},
			purl:         "pkg:npm/some-pkg@1.0.0",
			wantImports:  1,
			wantCalls:    1, // baseline for blank/side-effect import (#261)
			wantIsUnused: false,
			wantBlank:    true,
			wantBreadth:  0,
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

			if tt.wantNoResult {
				if _, ok := result[tt.purl]; ok {
					t.Fatalf("expected no coupling result for %s, but got one", tt.purl)
				}
				return
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
			if ca.IsUnused != tt.wantIsUnused {
				t.Errorf("IsUnused = %v, want %v", ca.IsUnused, tt.wantIsUnused)
			}
			if ca.HasBlankImport != tt.wantBlank {
				t.Errorf("HasBlankImport = %v, want %v", ca.HasBlankImport, tt.wantBlank)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}
		})
	}
}

// TestAnalyzer_JSReExport verifies that re-exports (export { foo } from "pkg",
// export * from "pkg") are detected as dependency usage. Re-exports introduce no
// local binding, so they are registered as blank imports. Type-only re-exports
// (export type { Foo } from "pkg") are skipped.
func TestAnalyzer_JSReExport(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		code        string
		importPaths map[string][]string
		purl        string
		wantImports int
		wantCalls   int // expected CallSiteCount (1 baseline for blank-import-only)
		wantBlank   bool
		wantUnused  bool
	}{
		{
			name:     "named re-export",
			filename: "index.ts",
			code:     `export { useState } from "react";` + "\n",
			importPaths: map[string][]string{
				"pkg:npm/react@18.0.0": {"react"},
			},
			purl:        "pkg:npm/react@18.0.0",
			wantImports: 1,
			wantCalls:   1, // blank-import baseline
			wantBlank:   true,
		},
		{
			name:     "aliased re-export",
			filename: "index.ts",
			code:     `export { useState as default } from "react";` + "\n",
			importPaths: map[string][]string{
				"pkg:npm/react@18.0.0": {"react"},
			},
			purl:        "pkg:npm/react@18.0.0",
			wantImports: 1,
			wantCalls:   1, // blank-import baseline
			wantBlank:   true,
		},
		{
			name:     "namespace re-export",
			filename: "index.js",
			code:     `export * from "lodash";` + "\n",
			importPaths: map[string][]string{
				"pkg:npm/lodash@4.17.21": {"lodash"},
			},
			purl:        "pkg:npm/lodash@4.17.21",
			wantImports: 1,
			wantCalls:   1, // blank-import baseline
			wantBlank:   true,
		},
		{
			name:     "namespace aliased re-export",
			filename: "index.ts",
			code:     `export * as utils from "lodash";` + "\n",
			importPaths: map[string][]string{
				"pkg:npm/lodash@4.17.21": {"lodash"},
			},
			purl:        "pkg:npm/lodash@4.17.21",
			wantImports: 1,
			wantCalls:   1, // blank-import baseline
			wantBlank:   true,
		},
		{
			name:     "type-only re-export skipped",
			filename: "index.ts",
			code:     `export type { Foo } from "bar";` + "\n",
			importPaths: map[string][]string{
				"pkg:npm/bar@1.0.0": {"bar"},
			},
			purl:       "pkg:npm/bar@1.0.0",
			wantUnused: true,
		},
		{
			name:     "re-export alongside regular import",
			filename: "index.ts",
			code: `import axios from "axios";
export { get } from "axios";

axios.post("/api");
`,
			importPaths: map[string][]string{
				"pkg:npm/axios@1.6.0": {"axios"},
			},
			purl:        "pkg:npm/axios@1.6.0",
			wantImports: 1,
			wantCalls:   1, // axios.post("/api") from the regular import
			wantBlank:   true,
		},
		{
			name:     "local export no from clause does not match",
			filename: "index.js",
			code: `const foo = 42;
export { foo };
`,
			importPaths: map[string][]string{
				"pkg:npm/foo@1.0.0": {"foo"},
			},
			purl:       "pkg:npm/foo@1.0.0",
			wantUnused: true,
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

			if tt.wantUnused {
				// The PURL should not appear in the result map — the import
				// was either filtered (type-only) or never captured.
				if result != nil {
					if ca, ok := result[tt.purl]; ok {
						t.Errorf("expected PURL %s absent from results, got ImportFileCount=%d IsUnused=%v",
							tt.purl, ca.ImportFileCount, ca.IsUnused)
					}
				}
				return
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
			if ca.HasBlankImport != tt.wantBlank {
				t.Errorf("HasBlankImport = %v, want %v", ca.HasBlankImport, tt.wantBlank)
			}
			if ca.IsUnused {
				t.Error("IsUnused = true, want false")
			}
		})
	}
}

func TestAnalyzer_JSDynamicImport(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		code        string
		importPaths map[string][]string
		purl        string
		wantImports int
		wantCalls   int // expected CallSiteCount (1 baseline for blank-import-only)
		wantBreadth int
		wantBlank   bool
		wantUnused  bool
	}{
		{
			name:     "basic dynamic import",
			filename: "index.js",
			code:     `const mod = await import("lodash");` + "\n",
			importPaths: map[string][]string{
				"pkg:npm/lodash@4.17.21": {"lodash"},
			},
			purl:        "pkg:npm/lodash@4.17.21",
			wantImports: 1,
			wantCalls:   1, // blank-import baseline
			wantBreadth: 0,
			wantBlank:   true,
		},
		{
			name:     "dynamic import with promise chain",
			filename: "index.js",
			code:     `import("lodash").then(m => m.default());` + "\n",
			importPaths: map[string][]string{
				"pkg:npm/lodash@4.17.21": {"lodash"},
			},
			purl:        "pkg:npm/lodash@4.17.21",
			wantImports: 1,
			wantCalls:   1, // blank-import baseline
			wantBreadth: 0,
			wantBlank:   true,
		},
		{
			name:     "destructured dynamic import",
			filename: "index.ts",
			code:     `const { get } = await import("axios");` + "\n",
			importPaths: map[string][]string{
				"pkg:npm/axios@1.6.0": {"axios"},
			},
			purl:        "pkg:npm/axios@1.6.0",
			wantImports: 1,
			wantCalls:   1, // blank-import baseline
			wantBreadth: 0,
			wantBlank:   true,
		},
		{
			name:     "dynamic import of scoped package",
			filename: "index.ts",
			code:     `const s3 = await import("@aws-sdk/client-s3");` + "\n",
			importPaths: map[string][]string{
				"pkg:npm/%40aws-sdk/client-s3@3.0.0": {"@aws-sdk/client-s3"},
			},
			purl:        "pkg:npm/%40aws-sdk/client-s3@3.0.0",
			wantImports: 1,
			wantCalls:   1, // blank-import baseline
			wantBreadth: 0,
			wantBlank:   true,
		},
		{
			name:     "dynamic import coexists with static import",
			filename: "index.ts",
			code: `import axios from "axios";
const lazy = await import("axios");

axios.get("/api");
`,
			importPaths: map[string][]string{
				"pkg:npm/axios@1.6.0": {"axios"},
			},
			purl:        "pkg:npm/axios@1.6.0",
			wantImports: 1,
			wantCalls:   1, // axios.get from static import
			wantBreadth: 1, // get
			wantBlank:   true,
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

			if tt.wantUnused {
				if result != nil {
					if _, ok := result[tt.purl]; ok {
						t.Errorf("expected no coupling analysis for %s", tt.purl)
					}
				}
				return
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
			if ca.HasBlankImport != tt.wantBlank {
				t.Errorf("HasBlankImport = %v, want %v", ca.HasBlankImport, tt.wantBlank)
			}
			if ca.IsUnused {
				t.Error("IsUnused = true, want false")
			}
		})
	}
}

// TestAnalyzer_CJSPropertyAssignRequire verifies that require() assigned to an
// object property (e.g., `file.glob = require('glob')`) is tracked correctly.
// The full qualified member expression (for example, `file.glob`) becomes an
// alias so that subsequent usage like `file.glob.sync()` or `file.glob()` is
// counted as a call site.
// Closes #290.
func TestAnalyzer_CJSPropertyAssignRequire(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		code        string
		importPaths map[string][]string
		purl        string
		wantImports int
		wantCalls   int
		wantBreadth int
		wantSymbols []string
	}{
		{
			name:     "property assign with method call: file.glob = require('glob')",
			filename: "index.js",
			code: `file.glob = require('glob');

file.glob.sync(pattern);
file.glob.hasMagic(pattern);
`,
			importPaths: map[string][]string{
				"pkg:npm/glob@7.0.0": {"glob"},
			},
			purl:        "pkg:npm/glob@7.0.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
			wantSymbols: []string{"hasMagic", "sync"},
		},
		{
			name:     "property assign with direct call: file.minimatch = require('minimatch')",
			filename: "index.js",
			code: `file.minimatch = require('minimatch');

file.minimatch(pattern, '*.js');
`,
			importPaths: map[string][]string{
				"pkg:npm/minimatch@3.0.0": {"minimatch"},
			},
			purl:        "pkg:npm/minimatch@3.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
			wantSymbols: []string{"file.minimatch"},
		},
		{
			name:     "property assign with dateformat pattern: template.date = require('dateformat')",
			filename: "index.js",
			code: `template.date = require('dateformat');

var result = template.date(new Date(), "yyyy-mm-dd");
`,
			importPaths: map[string][]string{
				"pkg:npm/dateformat@4.0.0": {"dateformat"},
			},
			purl:        "pkg:npm/dateformat@4.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
			wantSymbols: []string{"template.date"},
		},
		{
			name:     "property assign with findup pattern: file.findup = require('findup-sync')",
			filename: "index.js",
			code: `file.findup = require('findup-sync');

var fp = file.findup('Gruntfile.js');
`,
			importPaths: map[string][]string{
				"pkg:npm/findup-sync@5.0.0": {"findup-sync"},
			},
			purl:        "pkg:npm/findup-sync@5.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
			wantSymbols: []string{"file.findup"},
		},
		{
			name:     "no false positive: method name does not collide with separate import alias",
			filename: "index.js",
			code: `file.glob = require('glob');
const sync = require('sync');

file.glob.sync(pattern);
sync();
`,
			importPaths: map[string][]string{
				"pkg:npm/glob@7.0.0": {"glob"},
				"pkg:npm/sync@1.0.0": {"sync"},
			},
			purl:        "pkg:npm/sync@1.0.0",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
			wantSymbols: []string{"sync"},
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

// TestAnalyzer_AngularDecoratorRegistrations verifies that identifiers inside
// Angular decorator array arguments (@NgModule imports/declarations, standalone
// @Component imports) are detected as call sites. This prevents false IBNC for
// component-library dependencies that are only used via HTML template selectors.
// Closes #262.
func TestAnalyzer_AngularDecoratorRegistrations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		filename    string
		code        string
		importPaths map[string][]string
		purl        string
		wantCalls   int
		wantBreadth int
		wantSymbols []string
	}{
		{
			name:     "NgModule imports array",
			filename: "app.module.ts",
			code: `import { NgModule } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { HttpClientModule } from "@angular/common/http";
import { AppComponent } from "./app.component";

@NgModule({
  imports: [FormsModule, HttpClientModule],
  declarations: [AppComponent],
})
export class AppModule {}
`,
			importPaths: map[string][]string{
				"pkg:npm/%40angular/forms@17.0.0": {"@angular/forms"},
			},
			purl:        "pkg:npm/%40angular/forms@17.0.0",
			wantCalls:   1,
			wantBreadth: 1,
			wantSymbols: []string{"FormsModule"},
		},
		{
			name:     "NgModule declarations array",
			filename: "app.module.ts",
			code: `import { NgModule } from "@angular/core";
import { BrowserModule } from "@angular/platform-browser";
import { HeroListComponent } from "@hero/components";
import { AppComponent } from "./app.component";

@NgModule({
  imports: [BrowserModule],
  declarations: [AppComponent, HeroListComponent],
})
export class AppModule {}
`,
			importPaths: map[string][]string{
				"pkg:npm/%40hero/components@1.0.0": {"@hero/components"},
			},
			purl:        "pkg:npm/%40hero/components@1.0.0",
			wantCalls:   1,
			wantBreadth: 1,
			wantSymbols: []string{"HeroListComponent"},
		},
		{
			name:     "standalone Component imports",
			filename: "my.component.ts",
			code: `import { Component } from "@angular/core";
import { CommonModule } from "@angular/common";
import { RouterModule } from "@angular/router";

@Component({
  standalone: true,
  imports: [CommonModule, RouterModule],
  template: '<div></div>',
})
export class MyComponent {}
`,
			importPaths: map[string][]string{
				"pkg:npm/%40angular/common@17.0.0": {"@angular/common"},
				"pkg:npm/%40angular/router@17.0.0": {"@angular/router"},
			},
			purl:        "pkg:npm/%40angular/common@17.0.0",
			wantCalls:   1,
			wantBreadth: 1,
			wantSymbols: []string{"CommonModule"},
		},
		{
			name:     "Injectable decorator NOT captured as extra call sites",
			filename: "my.service.ts",
			code: `import { Injectable } from "@angular/core";

@Injectable({
  providedIn: 'root',
})
export class MyService {}
`,
			importPaths: map[string][]string{
				"pkg:npm/%40angular/core@17.0.0": {"@angular/core"},
			},
			purl: "pkg:npm/%40angular/core@17.0.0",
			// Injectable() is captured as a bare call_expression by existing patterns.
			// Its string argument ('root') should not produce extra call sites.
			wantCalls:   1,
			wantBreadth: 1,
			wantSymbols: []string{"Injectable"},
		},
		{
			name:     "Vue defineComponent with components option",
			filename: "MyPage.ts",
			code: `import { defineComponent } from "vue";
import MyChild from "my-child-component";

export default defineComponent({
  components: {
    MyChild,
  },
  setup() {
    return {};
  }
});
`,
			importPaths: map[string][]string{
				"pkg:npm/my-child-component@1.0.0": {"my-child-component"},
			},
			purl:        "pkg:npm/my-child-component@1.0.0",
			wantCalls:   1,
			wantBreadth: 1,
			wantSymbols: []string{"MyChild"},
		},
		{
			name:     "multiple modules in NgModule imports and exports",
			filename: "shared.module.ts",
			code: `import { NgModule } from "@angular/core";
import { CommonModule } from "@angular/common";
import { FormsModule, ReactiveFormsModule } from "@angular/forms";

@NgModule({
  imports: [CommonModule, FormsModule, ReactiveFormsModule],
  exports: [CommonModule, FormsModule, ReactiveFormsModule],
})
export class SharedModule {}
`,
			importPaths: map[string][]string{
				"pkg:npm/%40angular/forms@17.0.0": {"@angular/forms"},
			},
			purl: "pkg:npm/%40angular/forms@17.0.0",
			// FormsModule appears in both imports and exports (2 sites),
			// ReactiveFormsModule appears in both imports and exports (2 sites).
			wantCalls:   4,
			wantBreadth: 2,
			wantSymbols: []string{"FormsModule", "ReactiveFormsModule"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

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

			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}

			wantSymbols := append([]string(nil), tt.wantSymbols...)
			gotSymbols := append([]string(nil), ca.Symbols...)
			sort.Strings(wantSymbols)
			sort.Strings(gotSymbols)
			if len(gotSymbols) != len(wantSymbols) {
				t.Errorf("Symbols = %v, want %v", gotSymbols, wantSymbols)
			} else {
				for i, s := range gotSymbols {
					if s != wantSymbols[i] {
						t.Errorf("Symbols[%d] = %q, want %q", i, s, wantSymbols[i])
					}
				}
			}
		})
	}
}

// TestAnalyzer_DecoratorPredicateFilter verifies that the
// `(#match? @decorator "^(NgModule|Component)$")` predicate on the Angular/Vue
// decorator query rejects unrelated decorators. After the migration to the
// official tree-sitter binding, predicate evaluation moved from
// `cursor.FilterPredicates` to the iterator-internal `SatisfiesTextPredicate`
// implementation; this test pins the precision so a regression in predicate
// evaluation surfaces as a test failure rather than silent over-counting.
//
// The fixture mixes a matching `@NgModule` (whose array argument should
// produce a call site for FormsModule) with a non-matching `@CustomMeta`
// decorator that has the exact same AST shape. Only FormsModule should be
// counted; SomeOtherModule must NOT contribute to call sites.
func TestAnalyzer_DecoratorPredicateFilter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	code := `import { NgModule } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { SomeOtherModule } from "@other/lib";

function CustomMeta(meta: any) { return (target: any) => target; }

@NgModule({
  imports: [FormsModule],
})
export class AppModule {}

@CustomMeta({
  imports: [SomeOtherModule],
})
export class FakeModule {}
`
	if err := os.WriteFile(filepath.Join(dir, "app.module.ts"), []byte(code), 0644); err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:npm/%40angular/forms@17.0.0": {"@angular/forms"},
		"pkg:npm/%40other/lib@1.0.0":      {"@other/lib"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	// FormsModule must be counted exactly once via the @NgModule decorator.
	forms, ok := result["pkg:npm/%40angular/forms@17.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for @angular/forms")
	}
	if forms.CallSiteCount != 1 {
		t.Errorf("@angular/forms CallSiteCount = %d, want 1", forms.CallSiteCount)
	}
	if forms.APIBreadth != 1 {
		t.Errorf("@angular/forms APIBreadth = %d, want 1", forms.APIBreadth)
	}

	// SomeOtherModule appears under @CustomMeta which the predicate excludes.
	// The import statement still creates an entry (ImportFileCount=1), but
	// no call site should be attributed via the decorator query path.
	other, ok := result["pkg:npm/%40other/lib@1.0.0"]
	if !ok {
		t.Fatal("expected coupling analysis for @other/lib")
	}
	if other.CallSiteCount != 0 {
		t.Errorf("@other/lib CallSiteCount = %d, want 0 (CustomMeta decorator must not match the predicate)", other.CallSiteCount)
	}
}
