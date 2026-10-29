package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
)

func TestFileLineLocations(t *testing.T) {
	file := filepath.Join(t.TempDir(), "purls.txt")
	if err := os.WriteFile(file, []byte("# heading\n\n pkg:npm/a@1 \nhttps://github.com/a/b\npkg:npm/a@1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		opts  ProcessingOptions
		want  map[string][]depparser.SourceLocation
		purls []string
	}{
		{"all", ProcessingOptions{}, map[string][]depparser.SourceLocation{"pkg:npm/a@1": {{Line: 3}, {Line: 5}}, "https://github.com/a/b": {{Line: 4}}}, []string{"pkg:npm/a@1", "pkg:npm/a@1"}},
		{"range", ProcessingOptions{LineStart: 4, LineEnd: 5}, map[string][]depparser.SourceLocation{"pkg:npm/a@1": {{Line: 5}}, "https://github.com/a/b": {{Line: 4}}}, []string{"pkg:npm/a@1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			purls, urls, locs, err := categorizeFileLines(file, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(locs, tt.want) || !reflect.DeepEqual(purls, tt.purls) || !reflect.DeepEqual(urls, []string{"https://github.com/a/b"}) {
				t.Fatalf("got %v %v %v", purls, urls, locs)
			}
		})
	}
}

func TestSARIFFormat(t *testing.T) {
	got, err := ResolveFormat("SARIF ", 1)
	if err != nil || got != "sarif" {
		t.Fatalf("format = %q, error = %v", got, err)
	}
	for _, n := range []int{0, 1, 3, 4, 100} {
		got, err := ResolveFormat("", n)
		if err != nil || got == "sarif" {
			t.Fatalf("auto format = %q, error = %v", got, err)
		}
	}
}
