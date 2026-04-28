package depsdev

import (
	"errors"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/common/links"
	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

func TestToDepsDevSystemAndName(t *testing.T) {
	parser := purl.NewParser()

	table := []struct {
		purlStr    string
		wantSystem string
		wantName   string
	}{
		{"pkg:gem/tzinfo@1.2.2", "rubygems", "tzinfo"},
		// PathEscape leaves "@" unescaped (sub-delim per RFC 3986 §3.3) but
		// percent-encodes "/" inside a single segment.
		{"pkg:npm/@vue/runtime-dom@3.4.26", "npm", "@vue%2Fruntime-dom"},
		{"pkg:npm/lodash@4.17.21", "npm", "lodash"},
		// PathEscape leaves ":" unescaped (sub-delim per RFC 3986 §3.3) so the
		// Maven coordinate stays human-readable in the URL.
		{"pkg:maven/org.apache.logging.log4j/log4j-api@2.14.1", "maven", "org.apache.logging.log4j:log4j-api"},
		{"pkg:golang/golang.org/x/sys@v0.0.0-20211205182925-97ca703d548d", "go", "golang.org%2Fx%2Fsys"},
		// scoped npm where the PURL uses encoded @ (e.g., %40types) — parser
		// decodes and JoinNpmName re-adds the canonical @ prefix.
		{"pkg:npm/%40types/mongodb@3.1.17", "npm", "@types%2Fmongodb"},
	}

	for _, tc := range table {
		parsed, err := parser.Parse(tc.purlStr)
		if err != nil {
			t.Fatalf("parse error for %s: %v", tc.purlStr, err)
		}
		sys, name, err := toDepsDevSystemAndName(parsed)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", tc.purlStr, err)
		}
		if sys != tc.wantSystem {
			t.Fatalf("system for %s: got %q, want %q", tc.purlStr, sys, tc.wantSystem)
		}
		if name != tc.wantName {
			t.Fatalf("name for %s: got %q, want %q", tc.purlStr, name, tc.wantName)
		}
	}
}

func TestToDepsDevSystemAndName_UnsupportedEcosystem(t *testing.T) {
	parser := purl.NewParser()
	cases := []string{
		"pkg:composer/laravel/framework@10.0.0",
		"pkg:hex/phoenix@1.7.0",
		"pkg:swift/github.com/apple/swift-collections@1.0.0",
	}
	for _, purlStr := range cases {
		t.Run(purlStr, func(t *testing.T) {
			parsed, err := parser.Parse(purlStr)
			if err != nil {
				t.Fatalf("parse error for %s: %v", purlStr, err)
			}
			sys, name, err := toDepsDevSystemAndName(parsed)
			if !errors.Is(err, links.ErrUnsupportedEcosystem) {
				t.Fatalf("expected ErrUnsupportedEcosystem for %s, got err=%v", purlStr, err)
			}
			if sys != "" || name != "" {
				t.Fatalf("expected empty system/name on error, got (%q, %q)", sys, name)
			}
		})
	}
}

func TestToDepsDevSystemAndName_NilPURL(t *testing.T) {
	sys, name, err := toDepsDevSystemAndName(nil)
	if err == nil {
		t.Fatalf("expected error for nil PURL")
	}
	if sys != "" || name != "" {
		t.Fatalf("expected empty system/name on error, got (%q, %q)", sys, name)
	}
}

func TestToDepsDevSystemAndName_EmptyName(t *testing.T) {
	// A failed Parse returns a ParsedPURL with a zero-value packageURL
	// (empty ecosystem and name). The adapter must return a plain error —
	// not ErrUnsupportedEcosystem — so callers propagate it as a hard
	// error rather than a graceful skip.
	parser := purl.NewParser()
	parsed, _ := parser.Parse("pkg:npm/")
	sys, name, err := toDepsDevSystemAndName(parsed)
	if err == nil {
		t.Fatalf("expected error for empty-name PURL")
	}
	if errors.Is(err, links.ErrUnsupportedEcosystem) {
		t.Fatalf("empty-name error should NOT be ErrUnsupportedEcosystem, got: %v", err)
	}
	if sys != "" || name != "" {
		t.Fatalf("expected empty system/name on error, got (%q, %q)", sys, name)
	}
}
