package eolphrases

import (
	"testing"
)

func TestTextsAtTier_Strong(t *testing.T) {
	phrases := TextsAtTier(TierStrong)
	if len(phrases) == 0 {
		t.Fatal("expected at least one TierStrong phrase")
	}
	// Verify the original 7 detector.go phrases are present.
	expected := []string{
		"no longer maintained",
		"this project has been discontinued",
		"no longer supported",
		"deprecated permanently",
		"superseded by",
		"replaced by",
		"retired",
	}
	set := toSet(phrases)
	for _, e := range expected {
		if _, ok := set[e]; !ok {
			t.Errorf("missing TierStrong phrase %q", e)
		}
	}
	// "deprecated" must NOT be in TierStrong (it is TierReeval).
	if _, ok := set["deprecated"]; ok {
		t.Error("\"deprecated\" should not be in TierStrong")
	}
}

func TestTextsAtTier_Reeval(t *testing.T) {
	phrases := TextsAtTier(TierReeval)
	set := toSet(phrases)
	// Must include TierStrong phrases too (>= TierReeval).
	if _, ok := set["retired"]; !ok {
		t.Error("TierReeval should include TierStrong phrases")
	}
	// Must include TierReeval phrases.
	if _, ok := set["deprecated"]; !ok {
		t.Error("missing TierReeval phrase \"deprecated\"")
	}
	// Must NOT include TierSnippet phrases.
	if _, ok := set["deprecation"]; ok {
		t.Error("TierReeval should not include TierSnippet phrases")
	}
}

func TestTextsAtTier_Snippet(t *testing.T) {
	phrases := TextsAtTier(TierSnippet)
	set := toSet(phrases)
	// Must include all tiers.
	if _, ok := set["retired"]; !ok {
		t.Error("TierSnippet should include TierStrong phrases")
	}
	if _, ok := set["deprecated"]; !ok {
		t.Error("TierSnippet should include TierReeval phrases")
	}
	if _, ok := set["deprecation"]; !ok {
		t.Error("TierSnippet should include TierSnippet phrases")
	}
}

func TestContainsStrongPhrase_BackwardCompatible(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"This project has been deprecated", true},
		{"No longer maintained since 2020", true},
		{"This is a great library", false},
		{"", false},
		{"The project is ABANDONED", true},
		{"Sunset notice posted", true},
	}
	for _, tt := range tests {
		got := ContainsStrongPhrase(tt.text)
		if (len(got) > 0) != tt.want {
			t.Errorf("ContainsStrongPhrase(%q) = %v, want match=%v", tt.text, got, tt.want)
		}
	}
}

func toSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}
