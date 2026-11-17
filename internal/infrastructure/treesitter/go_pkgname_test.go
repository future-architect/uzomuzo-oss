//go:build cgo

package treesitter

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	purlAWS      = "pkg:golang/github.com/aws/aws-sdk-go-v2@v1.32.0"
	purlK8sAPI   = "pkg:golang/k8s.io/api@v0.31.0"
	purlJSONIter = "pkg:golang/github.com/json-iterator/go@v1.1.12"
	purlLRU      = "pkg:golang/github.com/hashicorp/golang-lru@v1.0.2"
	purlBar      = "pkg:golang/github.com/foo/bar@v1.0.0"
	purlTestify  = "pkg:golang/github.com/stretchr/testify@v1.9.0"
)

var goPackageNamePaths = map[string][]string{
	purlAWS:      {"github.com/aws/aws-sdk-go-v2"},
	purlK8sAPI:   {"k8s.io/api"},
	purlJSONIter: {"github.com/json-iterator/go"},
	purlLRU:      {"github.com/hashicorp/golang-lru"},
	purlBar:      {"github.com/foo/bar"},
	purlTestify:  {"github.com/stretchr/testify"},
}

// goPathsWithoutK8s is goPackageNamePaths with k8s.io/api left out of the SBOM.
var goPathsWithoutK8s = func() map[string][]string {
	m := make(map[string][]string, len(goPackageNamePaths))
	for purl, paths := range goPackageNamePaths {
		if purl != purlK8sAPI {
			m[purl] = paths
		}
	}
	return m
}()

type wantCoupling struct {
	calls   int
	symbols []string
}

// TestAnalyzer_GoPackageNameFromUsage covers #551: the package name of an
// unaliased import differs from the name guessed from its path, and is taken
// from how the importing package uses it.
func TestAnalyzer_GoPackageNameFromUsage(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  map[string]wantCoupling
	}{
		{
			// Guess "signer"; the local variable named signer must not be counted.
			name: "aws signer v4",
			files: map[string]string{"sign.go": `package main

import "github.com/aws/aws-sdk-go-v2/aws/signer/v4"

type local struct{}

func (local) First() {}

func sign() {
	s := v4.NewSigner()
	_ = s
	v4.GetSignedRequestSignature(nil)
}

func useLocal() {
	signer := local{}
	signer.First()
}
`},
			want: map[string]wantCoupling{purlAWS: {2, []string{"GetSignedRequestSignature", "NewSigner"}}},
		},
		{
			// Guess "core"; uses are qualified types and a selector.
			name: "kubernetes core v1",
			files: map[string]string{"pods.go": `package main

import "k8s.io/api/core/v1"

func pods() []v1.Pod { return []v1.Pod{{}} }

func ns() string { return v1.NamespaceDefault }
`},
			want: map[string]wantCoupling{purlK8sAPI: {3, []string{"NamespaceDefault", "Pod"}}},
		},
		{
			// Guess "go".
			name: "json-iterator",
			files: map[string]string{"codec.go": `package main

import "github.com/json-iterator/go"

func codec(x any) {
	b, _ := jsoniter.Marshal(x)
	_ = jsoniter.Unmarshal(b, &x)
}
`},
			want: map[string]wantCoupling{purlJSONIter: {2, []string{"Marshal", "Unmarshal"}}},
		},
		{
			// Guess "golanglru".
			name: "golang-lru",
			files: map[string]string{"cache.go": `package main

import "github.com/hashicorp/golang-lru"

func cache() {
	c, _ := lru.New(128)
	c.Add(1, 2)
	_, _ = lru.NewWithEvict(8, nil)
}
`},
			want: map[string]wantCoupling{purlLRU: {2, []string{"New", "NewWithEvict"}}},
		},
		{
			// C.CString is explained by import "C", so jsoniter is the one name left.
			name: "cgo import",
			files: map[string]string{"codec.go": `package main

import "C"

import "github.com/json-iterator/go"

func codec(x any) {
	_ = C.CString("x")
	_, _ = jsoniter.Marshal(x)
}
`},
			want: map[string]wantCoupling{purlJSONIter: {1, []string{"Marshal"}}},
		},
		{
			// settings is declared in another file of the package, so it is not
			// mistaken for the jsoniter package name.
			name: "declaration in a sibling file of the same package",
			files: map[string]string{
				"config.go": "package main\n\nvar settings = struct{ Debug bool }{}\n",
				"codec.go": `package main

import "github.com/json-iterator/go"

func codec(x any) {
	if settings.Debug {
		_, _ = jsoniter.Marshal(x)
	}
}
`,
			},
			want: map[string]wantCoupling{purlJSONIter: {1, []string{"Marshal"}}},
		},
		{
			// An in-package _test.go file shares declarations with the non-test files.
			name: "in-package test file uses a declaration from a non-test file",
			files: map[string]string{
				"helper.go": "package main\n\nvar helper = struct{ Name string }{}\n",
				"codec_test.go": `package main

import (
	"testing"

	"github.com/json-iterator/go"
)

func TestCodec(t *testing.T) {
	_, _ = jsoniter.Marshal(helper.Name)
}
`,
			},
			want: map[string]wantCoupling{purlJSONIter: {1, []string{"Marshal"}}},
		},
		{
			// A local named jsoniter in the external test package main_test does not
			// hide the name in package main.
			name: "external test package is a separate package",
			files: map[string]string{
				"codec.go": `package main

import "github.com/json-iterator/go"

func codec(x any) { _, _ = jsoniter.Marshal(x) }
`,
				"codec_test.go": `package main_test

import "testing"

func TestLocal(t *testing.T) {
	jsoniter := struct{ Marshal func() }{}
	jsoniter.Marshal()
}
`,
			},
			want: map[string]wantCoupling{purlJSONIter: {1, []string{"Marshal"}}},
		},
		{
			// metav1 is explained by its alias, so v1 is the only name left.
			name: "aliased import beside an unaliased one",
			files: map[string]string{"main.go": `package main

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/api/core/v1"
)

var _ = v1.Pod{ObjectMeta: metav1.ObjectMeta{}}
`},
			want: map[string]wantCoupling{purlK8sAPI: {1, []string{"Pod"}}},
		},
		{
			name: "blank imports beside the import to name",
			files: map[string]string{"main.go": `package main

import (
	_ "embed"
	_ "net/http/pprof"
	"github.com/json-iterator/go"
)

func main() { _, _ = jsoniter.Marshal(nil) }
`},
			want: map[string]wantCoupling{purlJSONIter: {1, []string{"Marshal"}}},
		},
		{
			// The package clause names the importing package, not anything the
			// file uses, so package v1 does not hide the import's v1.
			name: "package clause with the import's real name",
			files: map[string]string{"pod.go": `package v1

import "k8s.io/api/core/v1"

var _ = v1.Pod{}
`},
			want: map[string]wantCoupling{purlK8sAPI: {1, []string{"Pod"}}},
		},
		{
			// Comments before the package clause and a cgo preamble between it
			// and the imports must not stop the walk over the import block.
			name: "comments before and after the package clause",
			files: map[string]string{"main.go": `// Copyright 2026 Example Authors.

//go:build linux

// Package main encodes.
package main

/*
#include <stdlib.h>
*/
import "C"

import "github.com/json-iterator/go"

func main() { _, _ = jsoniter.Marshal(nil) }
`},
			want: map[string]wantCoupling{purlJSONIter: {1, []string{"Marshal"}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertGoCoupling(t, analyzeGoProject(t, tc.files), tc.want)
		})
	}
}

// TestAnalyzer_GoPackageNameFallsBack pins the cases where the usage does not
// decide the name: the path-based guess is kept, so the result is unchanged.
func TestAnalyzer_GoPackageNameFallsBack(t *testing.T) {
	big := "package main\n\n// " + strings.Repeat("x", largeFileBytes) + "\n"
	codec := `package main

import "github.com/json-iterator/go"

func codec(x any) { _, _ = jsoniter.Marshal(x) }
`
	tests := []struct {
		name  string
		files map[string]string
		// sbom replaces goPackageNamePaths when set.
		sbom map[string][]string
		want map[string]wantCoupling
	}{
		{
			name: "real name is a struct field in a sibling file",
			files: map[string]string{
				"config.go": "package main\n\ntype config struct{ v1 int }\n",
				"main.go":   "package main\n\nimport \"k8s.io/api/core/v1\"\n\nvar _ = v1.Pod{}\n",
			},
			want: map[string]wantCoupling{purlK8sAPI: {0, nil}},
		},
		{
			name: "real name is a label in a sibling file",
			files: map[string]string{
				"loop.go": "package main\n\nfunc loop() {\nv1:\n\tfor {\n\t\tbreak v1\n\t}\n}\n",
				"main.go": "package main\n\nimport \"k8s.io/api/core/v1\"\n\nvar _ = v1.Pod{}\n",
			},
			want: map[string]wantCoupling{purlK8sAPI: {0, nil}},
		},
		{
			name: "real name is a local type in a sibling file",
			files: map[string]string{
				"kind.go": "package main\n\nfunc kind() {\n\ttype v1 int\n\tvar _ v1\n}\n",
				"main.go": "package main\n\nimport \"k8s.io/api/core/v1\"\n\nvar _ = v1.Pod{}\n",
			},
			want: map[string]wantCoupling{purlK8sAPI: {0, nil}},
		},
		{
			// Only the sibling's aliased call is counted.
			name: "real name is an import alias in a sibling file",
			files: map[string]string{
				"alias.go": "package main\n\nimport v1 \"k8s.io/api/core/v1\"\n\nvar _ = v1.Pod{}\n",
				"main.go":  "package main\n\nimport \"k8s.io/api/core/v1\"\n\nvar _ = v1.Pod{}\n",
			},
			want: map[string]wantCoupling{purlK8sAPI: {1, []string{"Pod"}}},
		},
		{
			// k8s.io/api/core is guessed as core, the alias of the other import.
			// Its name corev1 is illustrative; with core also a field elsewhere,
			// the shared name stops the inference instead of renaming it. As on
			// main, core.Do is then credited to both imports named core.
			name: "alias equal to another import's guess",
			files: map[string]string{
				"config.go": "package main\n\ntype config struct{ core int }\n",
				"main.go": `package main

import (
	core "github.com/foo/bar"
	"k8s.io/api/core"
)

func main() {
	core.Do()
	_ = corev1.Pod{}
}
`,
			},
			want: map[string]wantCoupling{purlBar: {1, []string{"Do"}}, purlK8sAPI: {1, []string{"Do"}}},
		},
		{
			// error.Error is a method expression on the predeclared error type.
			// bar is guessed correctly but also declared as a local in another
			// file, so error must not be the name that bar gets.
			name: "predeclared error used as an operand",
			files: map[string]string{
				"other.go": "package main\n\nfunc other() { bar := 1; _ = bar }\n",
				"main.go": `package main

import "github.com/foo/bar"

func main() {
	bar.Do()
	bar.Do()
	_ = error.Error
}
`,
			},
			want: map[string]wantCoupling{purlBar: {2, []string{"Do"}}},
		},
		{
			// example.com/error is guessed as error, which error.Error seems to
			// confirm. Its real name errors would then be handed to bar, whose
			// guess was right.
			name: "import guessed as a predeclared name",
			files: map[string]string{
				"other.go": "package main\n\nfunc other() { bar := 1; _ = bar }\n",
				"main.go": `package main

import (
	"example.com/error"
	"github.com/foo/bar"
)

func main() {
	bar.Do()
	_ = errors.New("x")
	_ = error.Error
}
`,
			},
			want: map[string]wantCoupling{purlBar: {1, []string{"Do"}}},
		},
		{
			name: "two unexplained imports in one file",
			files: map[string]string{"main.go": `package main

import (
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/hashicorp/golang-lru"
	"github.com/json-iterator/go"
	"k8s.io/api/core/v1"
)

func main() {
	v4.NewSigner()
	_, _ = lru.New(1)
	_, _ = jsoniter.Marshal(nil)
	_ = v1.Pod{}
}
`},
			want: map[string]wantCoupling{
				purlAWS:      {0, nil},
				purlLRU:      {0, nil},
				purlJSONIter: {0, nil},
				purlK8sAPI:   {0, nil},
			},
		},
		{
			// k8s.io/api is not in the SBOM here, but its import still takes part,
			// so two imports remain unexplained.
			name: "two unexplained imports, one of them outside the SBOM",
			files: map[string]string{"main.go": `package main

import (
	"github.com/json-iterator/go"
	"k8s.io/api/core/v1"
)

func main() {
	_, _ = jsoniter.Marshal(v1.Pod{})
}
`},
			sbom: goPathsWithoutK8s,
			want: map[string]wantCoupling{purlJSONIter: {0, nil}},
		},
		{
			// bar is guessed correctly but also declared as a local in another file.
			// Ignoring the import outside the SBOM would leave bar as the only
			// unexplained import and give it the name v1.
			name: "correct guess is kept when the only other unexplained import is outside the SBOM",
			files: map[string]string{
				"other.go": "package main\n\nfunc other() { bar := 1; _ = bar }\n",
				"main.go": `package main

import (
	"github.com/foo/bar"
	"k8s.io/api/core/v1"
)

func main() {
	bar.Do()
	_ = v1.Pod{}
}
`,
			},
			sbom: goPathsWithoutK8s,
			want: map[string]wantCoupling{purlBar: {1, []string{"Do"}}},
		},
		{
			// Both apis/apiextensions and apis/apiextensions/v1beta1 are guessed as
			// apiextensions, so one guess is wrong and v1beta1 is left over. bar's
			// guess is right but bar is also a local elsewhere; handing it v1beta1
			// would be wrong. Found in k8s.io/apiextensions-apiserver.
			name: "two imports with the same guess",
			files: map[string]string{
				"other.go": "package main\n\nfunc other() { bar := 1; _ = bar }\n",
				"main_test.go": `package main

import (
	"testing"

	"github.com/foo/bar"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
)

func TestX(t *testing.T) {
	_ = apiextensions.CustomResourceDefinition{}
	_ = v1beta1.KubeAPIApprovedAnnotation
	bar.Do()
}
`,
			},
			want: map[string]wantCoupling{purlBar: {1, []string{"Do"}}},
		},
		{
			name: "two unexplained names",
			files: map[string]string{"main.go": `package main

import "github.com/json-iterator/go"

func main() {
	_, _ = jsoniter.Marshal(nil)
	mystery.Call()
}
`},
			want: map[string]wantCoupling{purlJSONIter: {0, nil}},
		},
		{
			name: "dot import in the file",
			files: map[string]string{"main.go": `package main

import (
	. "strings"

	"github.com/json-iterator/go"
)

func main() { _, _ = jsoniter.Marshal(ToUpper("x")) }
`},
			want: map[string]wantCoupling{purlJSONIter: {0, nil}},
		},
		{
			name:  "a file in the directory is too large to read",
			files: map[string]string{"codec.go": codec, "big.go": big},
			want:  map[string]wantCoupling{purlJSONIter: {0, nil}},
		},
		{
			name:  "a file in the directory has a syntax error",
			files: map[string]string{"codec.go": codec, "broken.go": "package main\n\nfunc broken( {\n"},
			want:  map[string]wantCoupling{purlJSONIter: {0, nil}},
		},
		{
			// testify's assert := assert.New(t) shadows the package name with a
			// local. The guess is right and nothing else is unexplained, so it stays;
			// the local's calls are counted as before.
			name: "test file shadowing a correctly guessed package",
			files: map[string]string{"main_test.go": `package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestX(t *testing.T) {
	assert := assert.New(t)
	assert.Equal(1, 1)
}
`},
			want: map[string]wantCoupling{purlTestify: {2, []string{"Equal", "New"}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertGoCoupling(t, analyzeGoProjectWith(t, t.TempDir(), tc.files, tc.sbom), tc.want)
		})
	}
}

// TestAnalyzer_GoPackageNameSymlinkFallsBack: a symlinked .go file in the
// directory may point outside --source, so its declarations are not read and
// the directory is not used for inference.
func TestAnalyzer_GoPackageNameSymlinkFallsBack(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package main\n\nvar settings = struct{ Debug bool }{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "config.go")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	files := map[string]string{"codec.go": `package main

import "github.com/json-iterator/go"

func codec(x any) {
	if settings.Debug {
		_, _ = jsoniter.Marshal(x)
	}
}
`}
	assertGoCoupling(t, analyzeGoProjectIn(t, dir, files), map[string]wantCoupling{purlJSONIter: {0, nil}})
}

// TestGoDirIndex_ScanStopsWhenCanceled checks that a canceled call gives up
// on the directory instead of reading its files.
func TestGoDirIndex_ScanStopsWhenCanceled(t *testing.T) {
	dir := t.TempDir()
	writeCorpusFile(t, filepath.Join(dir, "main.go"), "package main\n\nvar settings = 1\n")
	analyzer := NewAnalyzer()
	t.Cleanup(analyzer.Close)
	cfg := analyzer.configs[langGo]
	cfg.ensureCompiled()

	if got := newGoDirIndex(context.Background(), cfg).scan(dir); !got["main"]["settings"] {
		t.Fatalf("scan with a live context = %v, want main.settings", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := newGoDirIndex(ctx, cfg).scan(dir); got != nil {
		t.Errorf("scan with a canceled context = %v, want nil", got)
	}
}

func analyzeGoProject(t *testing.T, files map[string]string) map[string]wantCoupling {
	t.Helper()
	return analyzeGoProjectIn(t, t.TempDir(), files)
}

func analyzeGoProjectIn(t *testing.T, dir string, files map[string]string) map[string]wantCoupling {
	t.Helper()
	return analyzeGoProjectWith(t, dir, files, nil)
}

// analyzeGoProjectWith analyzes files written to dir against sbom, or against
// goPackageNamePaths when sbom is nil.
func analyzeGoProjectWith(t *testing.T, dir string, files map[string]string, sbom map[string][]string) map[string]wantCoupling {
	t.Helper()
	if sbom == nil {
		sbom = goPackageNamePaths
	}
	for name, content := range files {
		writeCorpusFile(t, filepath.Join(dir, name), content)
	}
	analyzer := NewAnalyzer()
	t.Cleanup(analyzer.Close)
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, sbom)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]wantCoupling, len(result))
	for purl, ca := range result {
		if ca.APIBreadth != len(ca.Symbols) {
			t.Errorf("%s: APIBreadth = %d, want len(Symbols) = %d", purl, ca.APIBreadth, len(ca.Symbols))
		}
		got[purl] = wantCoupling{ca.CallSiteCount, ca.Symbols}
	}
	return got
}

// assertGoCoupling checks the dependencies in want and that every other
// dependency has no call, so a call credited to the wrong import fails.
func assertGoCoupling(t *testing.T, got, want map[string]wantCoupling) {
	t.Helper()
	for purl, g := range got {
		if _, ok := want[purl]; !ok && g.calls != 0 {
			t.Errorf("%s: CallSiteCount = %d, want 0 (not expected to be used)", purl, g.calls)
		}
	}
	for purl, w := range want {
		g, ok := got[purl]
		if !ok {
			t.Errorf("%s: no coupling result", purl)
			continue
		}
		if g.calls != w.calls {
			t.Errorf("%s: CallSiteCount = %d, want %d", purl, g.calls, w.calls)
		}
		if len(g.symbols) != 0 || len(w.symbols) != 0 {
			if !reflect.DeepEqual(g.symbols, w.symbols) {
				t.Errorf("%s: Symbols = %v, want %v", purl, g.symbols, w.symbols)
			}
		}
	}
}
