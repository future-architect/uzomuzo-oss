package ghaworkflow_test

import (
	"reflect"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/ghaworkflow"
)

func TestWorkflowLocations(t *testing.T) {
	data := []byte("jobs:\n  reusable:\n    uses: owner/reusable/.github/workflows/build.yml@main\n  build:\n    steps:\n      - uses: actions/checkout@v4\n      - uses: ./local\n      - uses: docker://alpine\n      - uses: actions/checkout@v3\n")
	urls, locs, err := ghaworkflow.ParseGitHubURLsWithLocations(data)
	if err != nil {
		t.Fatal(err)
	}
	wantURLs := []string{"https://github.com/actions/checkout", "https://github.com/owner/reusable"}
	wantLocs := map[string][]depparser.SourceLocation{wantURLs[0]: {{Line: 6}, {Line: 9}}, wantURLs[1]: {{Line: 3}}}
	if !reflect.DeepEqual(urls, wantURLs) || !reflect.DeepEqual(locs, wantLocs) {
		t.Fatalf("got %v %v, want %v %v", urls, locs, wantURLs, wantLocs)
	}
	old, err := ghaworkflow.ParseGitHubURLs(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(urls, old) {
		t.Fatalf("URLs changed: %v vs %v", urls, old)
	}
	if _, _, err := ghaworkflow.ParseGitHubURLsWithLocations([]byte("{{invalid yaml")); err == nil {
		t.Fatal("expected malformed YAML error")
	}
}

func FuzzParseGitHubURLsWithLocations(f *testing.F) {
	f.Add([]byte("jobs:\n  build:\n    steps:\n      - uses: actions/checkout@v4\n"))
	f.Add([]byte(""))
	f.Add([]byte("jobs: {test: {steps: [{uses: &action actions/checkout@v4}, {uses: *action}]}}"))
	f.Fuzz(func(t *testing.T, data []byte) {
		urls, locs, err := ghaworkflow.ParseGitHubURLsWithLocations(data)
		old, oldErr := ghaworkflow.ParseGitHubURLs(data)
		if (err == nil) != (oldErr == nil) {
			t.Fatalf("parse errors differ: %v vs %v", err, oldErr)
		}
		if err != nil {
			return
		}
		if !reflect.DeepEqual(urls, old) {
			t.Fatalf("URLs differ: %v vs %v", urls, old)
		}
		for _, locations := range locs {
			for _, loc := range locations {
				if loc.Line < 1 {
					t.Fatalf("invalid line %d", loc.Line)
				}
			}
		}
	})
}
