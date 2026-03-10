package purl

import "testing"

func TestWithVersion(t *testing.T) {
	t.Run("add_version_to_versionless_purl", func(t *testing.T) {
		out, err := WithVersion("pkg:npm/express", "4.18.2")
		if err != nil {
			t.Fatalf("WithVersion error: %v", err)
		}
		if out != "pkg:npm/express@4.18.2" {
			t.Fatalf("got %q, want %q", out, "pkg:npm/express@4.18.2")
		}
	})

	t.Run("replace_existing_version", func(t *testing.T) {
		out, err := WithVersion("pkg:npm/express@4.17.1", "4.18.2")
		if err != nil {
			t.Fatalf("WithVersion error: %v", err)
		}
		if out != "pkg:npm/express@4.18.2" {
			t.Fatalf("got %q, want %q", out, "pkg:npm/express@4.18.2")
		}
	})

	t.Run("preserve_qualifiers_and_subpath", func(t *testing.T) {
		out, err := WithVersion("pkg:maven/org.apache.commons/commons-lang3@3.12.0?type=jar#src", "3.14.0")
		if err != nil {
			t.Fatalf("WithVersion error: %v", err)
		}
		if out != "pkg:maven/org.apache.commons/commons-lang3@3.14.0?type=jar#src" {
			t.Fatalf("got %q, want %q", out, "pkg:maven/org.apache.commons/commons-lang3@3.14.0?type=jar#src")
		}
	})
}
