//go:build cgo

package treesitter

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	domaindiet "github.com/future-architect/uzomuzo-oss/internal/domain/diet"
)

func writeBenchCorpus(tb testing.TB, filesPerLang int) (string, map[string][]string) {
	tb.Helper()
	root := tb.TempDir()

	importPaths := map[string][]string{
		"pkg:golang/github.com/foo/bar@v1.0.0":       {"github.com/foo/bar"},
		"pkg:npm/lodash@4.17.21":                     {"lodash"},
		"pkg:pypi/requests@2.31.0":                   {"requests"},
		"pkg:maven/com.google.code.gson/gson@2.10.1": {"com.google.gson"},
	}

	for i := 0; i < filesPerLang; i++ {
		mod := i % 10
		dir := corpusModDir(tb, root, mod)
		writeGoCorpusFile(tb, dir, mod, i)
		writeCorpusFile(tb, filepath.Join(dir, fmt.Sprintf("file%d.js", i)), fmt.Sprintf(`const _ = require('lodash');

function run%d() {
  _.map([1, 2, 3], (x) => x * 2);
  return _.uniq([1, 1, 2]);
}

module.exports = { run%d };
`, i, i))
		writeCorpusFile(tb, filepath.Join(dir, fmt.Sprintf("file%d.py", i)), fmt.Sprintf(`import requests


def run_%d():
    resp = requests.get("https://example.com")
    return requests.utils.default_headers(), resp
`, i))
		writeCorpusFile(tb, filepath.Join(dir, fmt.Sprintf("File%d.java", i)), fmt.Sprintf(`package mod%d;

import com.google.gson.Gson;

public class File%d {
    public String run() {
        Gson gson = new Gson();
        return gson.toJson(new Object());
    }
}
`, mod, i))
	}
	return root, importPaths
}

// corpusModDir creates (if needed) and returns the per-module directory used
// by every corpus generator in this package, so the mkdir scaffold has one
// source of truth instead of being copy-pasted per generator.
func corpusModDir(tb testing.TB, root string, mod int) string {
	tb.Helper()
	dir := filepath.Join(root, "src", fmt.Sprintf("mod%d", mod))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		tb.Fatalf("creating corpus dir: %v", err)
	}
	return dir
}

// writeGoCorpusFile writes the shared Go source template used by every
// generator that needs a parseable .go file: one import plus one call site,
// which is exactly what AnalyzeCoupling's import/call queries need to match.
// mod names the package (must match the directory's mod%d suffix); i makes
// the filename and function name unique within the corpus.
func writeGoCorpusFile(tb testing.TB, dir string, mod, i int) {
	tb.Helper()
	writeCorpusFile(tb, filepath.Join(dir, fmt.Sprintf("file%d.go", i)), fmt.Sprintf(`package mod%d

import (
	"fmt"
	"github.com/foo/bar"
)

func Run%d() {
	bar.Do()
	bar.Also()
	fmt.Println(bar.Value())
}
`, mod, i))
}

func writeCorpusFile(tb testing.TB, path, content string) {
	tb.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		tb.Fatalf("writing corpus file %s: %v", path, err)
	}
}

// requireCouplingResult fails the benchmark's setup phase if AnalyzeCoupling
// errored or produced no data, so a broken corpus is never silently measured
// as a no-op.
func requireCouplingResult(tb testing.TB, result map[string]*domaindiet.CouplingAnalysis, err error) {
	tb.Helper()
	if err != nil {
		tb.Fatalf("AnalyzeCoupling failed during setup: %v", err)
	}
	if len(result) == 0 {
		tb.Fatal("AnalyzeCoupling returned no coupling data; the benchmark would measure a no-op")
	}
}
