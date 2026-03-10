package depsdev

import (
	"net/url"
	"testing"

	"github.com/future-architect/uzomuzo/internal/common/purl"
)

func TestToDepsDevSystemAndName(t *testing.T) {
	parser := purl.NewParser()

	table := []struct {
		purlStr    string
		wantSystem string
		wantName   string
	}{
		{"pkg:gem/tzinfo@1.2.2", "rubygems", "tzinfo"},
		{"pkg:npm/@vue/runtime-dom@3.4.26", "npm", url.QueryEscape("@vue/runtime-dom")},
		{"pkg:npm/lodash@4.17.21", "npm", "lodash"},
		{"pkg:maven/org.apache.logging.log4j/log4j-api@2.14.1", "maven", url.QueryEscape("org.apache.logging.log4j:log4j-api")},
		{"pkg:golang/golang.org/x/sys@v0.0.0-20211205182925-97ca703d548d", "go", "golang.org%2Fx%2Fsys"},
		// scoped npm where the PURL uses encoded @ (e.g., %40types)
		{"pkg:npm/%40types/mongodb@3.1.17", "npm", url.QueryEscape("@types/mongodb")},
	}

	for _, tc := range table {
		parsed, err := parser.Parse(tc.purlStr)
		if err != nil {
			t.Fatalf("parse error for %s: %v", tc.purlStr, err)
		}
		sys, name := toDepsDevSystemAndName(parsed)
		if sys != tc.wantSystem {
			t.Fatalf("system for %s: got %q, want %q", tc.purlStr, sys, tc.wantSystem)
		}
		if name != tc.wantName {
			t.Fatalf("name for %s: got %q, want %q", tc.purlStr, name, tc.wantName)
		}
	}
}
