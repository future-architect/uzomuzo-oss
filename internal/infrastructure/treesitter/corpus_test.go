//go:build cgo

package treesitter

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
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
		dir := filepath.Join(root, "src", fmt.Sprintf("mod%d", i%10))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			tb.Fatalf("creating corpus dir: %v", err)
		}
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
`, i%10, i))
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
`, i%10, i))
	}
	return root, importPaths
}

func writeCorpusFile(tb testing.TB, path, content string) {
	tb.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		tb.Fatalf("writing corpus file %s: %v", path, err)
	}
}
