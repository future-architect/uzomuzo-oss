package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDietExperimentalNotice_LinksToExistingSection keeps the notice's link
// pointing at a heading that exists in docs/diet.md.
func TestDietExperimentalNotice_LinksToExistingSection(t *testing.T) {
	var buf bytes.Buffer
	writeDietExperimentalNotice(&buf)
	got := buf.String()

	if !strings.Contains(got, "experimental") {
		t.Errorf("notice does not say experimental: %q", got)
	}
	const anchor = "docs/diet.md#accuracy"
	if !strings.Contains(got, anchor) {
		t.Fatalf("notice does not link %s: %q", anchor, got)
	}

	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "diet.md"))
	if err != nil {
		t.Fatalf("read docs/diet.md: %v", err)
	}
	if !strings.Contains(string(doc), "\n## Accuracy\n") {
		t.Errorf("docs/diet.md has no \"## Accuracy\" heading for the notice to link to")
	}
}
