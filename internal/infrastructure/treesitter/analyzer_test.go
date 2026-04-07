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
		name        string
		code        string
		importPaths map[string][]string
		purl        string
		wantImports int
		wantCalls   int
		wantBreadth int
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
			purl:        "pkg:pypi/flask@3.0.0",
			wantImports: 1,
			wantCalls:   0,
			wantBreadth: 0,
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
