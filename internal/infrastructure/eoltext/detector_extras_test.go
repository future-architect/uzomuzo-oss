package eoltext

import (
	"regexp"
	"testing"
)

// TestSentenceLooksLikePlatformVersionNotice verifies platform version only notices are detected
// and that presence of a project token suppresses the classification.
func TestSentenceLooksLikePlatformVersionNotice(t *testing.T) {
	cases := []struct {
		in     string
		tokens []string
		expect bool
		name   string
	}{
		{"Python 3.11 is no longer supported.", []string{"this project"}, true, "pure platform"},
		{"Node.js 14 is no longer maintained", []string{"pkgname"}, true, "node platform"},
		{"Python 3.11 is no longer supported in this project.", []string{"this project"}, false, "project token present"},
		{"Go 1.21 support removed from this package", []string{"this package"}, false, "package token present"},
		{"This project has reached end of life", []string{"this project"}, false, "not platform notice"},
	}
	for _, c := range cases {
		got := sentenceLooksLikePlatformVersionNotice(c.in, c.tokens)
		if got != c.expect {
			t.Errorf("%s: sentenceLooksLikePlatformVersionNotice(%q) = %v want %v", c.name, c.in, got, c.expect)
		}
	}
}

// TestSplitSentences validates the lightweight sentence splitter boundaries.
func TestSplitSentences(t *testing.T) {
	text := "First sentence.\nSecond line!\nThird? Fourth line without punctuation"
	got := splitSentences(text)
	wantLen := 4
	if len(got) != wantLen {
		t.Fatalf("splitSentences len=%d want %d => %#v", len(got), wantLen, got)
	}
	if got[0] != "First sentence." || got[1] != "Second line!" || got[2] != "Third?" {
		t.Errorf("unexpected first segments: %#v", got[:3])
	}
	if got[3] != "Fourth line without punctuation" {
		t.Errorf("unexpected tail segment: %q", got[3])
	}
}

// TestExtractSuccessorNearPhrase exercises window constraints (chars & newlines) for successor extraction.
func TestExtractSuccessorNearPhrase(t *testing.T) {
	pats := []*regexp.Regexp{regexp.MustCompile(`(?i)use\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)\s+instead`)}
	phrase := "no longer maintained"
	base := phrase + " and deprecated: please upgrade. Use newlib instead for future work."
	succ := extractSuccessorNearPhrase(base, phrase, pats, 400, 3)
	if succ != "newlib" {
		t.Fatalf("expected successor 'newlib', got %q", succ)
	}

	// Exceed char window (set maxChars small so successor falls outside)
	succ2 := extractSuccessorNearPhrase(base, phrase, pats, 10, 3)
	if succ2 != "" {
		t.Errorf("expected empty successor with tight char window, got %q", succ2)
	}

	// Exceed newline window
	multi := phrase + "\n\nMore details...\nFurther info...\nUse otherlib instead." // 4 newlines before successor
	succ3 := extractSuccessorNearPhrase(multi, phrase, pats, 400, 3)
	if succ3 != "" {
		t.Errorf("expected empty successor due to newline window, got %q", succ3)
	}

	// Phrase not found
	nf := extractSuccessorNearPhrase("text without target phrase use newlib instead", phrase, pats, 400, 3)
	if nf != "" {
		t.Errorf("expected empty successor when phrase missing, got %q", nf)
	}
}

// TestNewExplicitPatterns verifies the Issue #17 expanded explicit pattern detections.
func TestNewExplicitPatterns(t *testing.T) {
	cases := []struct {
		name   string
		source SourceKind
		text   string
		want   bool
	}{
		{"readme sunset", SourceReadme, "This project is sunset", true},
		{"readme sunsetted", SourceReadme, "This repository is now sunsetted", true},
		{"readme decommissioned", SourceReadme, "This package is decommissioned", true},
		{"readme obsoleted", SourceReadme, "This repo is obsoleted", true},
		{"readme sunsetting", SourceReadme, "Sunsetting this project effective today.", true},
		{"readme consider obsolete", SourceReadme, "Consider this project obsolete.", true},
		{"pypi sunset", SourcePyPI, "This project is sunset", true},
		{"pypi decommissioned", SourcePyPI, "This package is decommissioned", true},
		{"short sunset", SourceShortMessage, "This package is sunset", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := DetectLifecycle(LifecycleDetectOpts{
				Source:      tc.source,
				PackageName: "testpkg",
				RepoName:    "testrepo",
				Text:        tc.text,
			})
			if res.Matched != tc.want {
				t.Errorf("Matched=%v want %v (phrase=%q kind=%d)", res.Matched, tc.want, res.Phrase, res.Kind)
			}
		})
	}
}

// TestNewStrongPhrases verifies that new TierStrong keywords from Issue #17 work.
func TestNewStrongPhrases(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"decommissioned", "This project has been decommissioned."},
		{"development halted", "This project development halted."},
		{"end of maintenance", "This project has reached end of maintenance."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := DetectLifecycle(LifecycleDetectOpts{
				Source:   SourceReadme,
				RepoName: "testrepo",
				Text:     tc.text,
			})
			if !res.Matched || res.Kind != KindStrong {
				t.Errorf("expected KindStrong match, got Matched=%v Kind=%d Phrase=%q", res.Matched, res.Kind, res.Phrase)
			}
		})
	}
}

// TestNewReevalPhrases verifies that new TierReeval keywords are detected as strong.
func TestNewReevalPhrases(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"sunset", "This project is sunset."},
		{"maintenance ended", "This project maintenance ended."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := DetectLifecycle(LifecycleDetectOpts{
				Source:   SourceReadme,
				RepoName: "testrepo",
				Text:     tc.text,
			})
			if !res.Matched {
				t.Errorf("expected match for %q, got none", tc.name)
			}
		})
	}
}

// TestSentenceNegatives verifies that sentence-level negatives suppress only the target sentence.
func TestSentenceNegatives(t *testing.T) {
	// Sentence with both EOL phrase and negative -> should NOT match
	t.Run("negative suppresses same sentence", func(t *testing.T) {
		text := "This project is actively maintained and no longer supported by the original author."
		res := DetectLifecycle(LifecycleDetectOpts{
			Source:   SourceReadme,
			RepoName: "testrepo",
			Text:     text,
		})
		if res.Matched {
			t.Errorf("expected no match when sentence-level negative is present, got Phrase=%q", res.Phrase)
		}
	})

	// Two sentences: first has negative, second has EOL phrase -> second sentence should match
	t.Run("negative does not suppress other sentences", func(t *testing.T) {
		text := "This project is actively maintained. However the legacy branch has been retired."
		res := DetectLifecycle(LifecycleDetectOpts{
			Source:   SourceReadme,
			RepoName: "testrepo",
			Text:     text,
		})
		if !res.Matched {
			t.Error("expected match from second sentence")
		}
	})

	// under active development
	t.Run("under active development suppresses", func(t *testing.T) {
		text := "This project is deprecated but under active development."
		res := DetectLifecycle(LifecycleDetectOpts{
			Source:   SourceReadme,
			RepoName: "testrepo",
			Text:     text,
		})
		if res.Matched {
			t.Errorf("expected no match when 'under active development' present, got Phrase=%q", res.Phrase)
		}
	})
}

// TestContextualPatternsForStats verifies the accessor returns labeled contextual patterns.
func TestContextualPatternsForStats(t *testing.T) {
	pats := ContextualPatternsForStats()
	if len(pats) != 8 {
		t.Fatalf("ContextualPatternsForStats returned %d patterns, want 8", len(pats))
	}
	for i, p := range pats {
		if p.Label == "" {
			t.Errorf("pattern[%d] has empty label", i)
		}
		if p.Rx == nil {
			t.Errorf("pattern[%d] (%s) has nil Rx", i, p.Label)
		}
	}
	// Spot-check: pattern[3] should match "moved into read-only mode"
	if !pats[3].Rx.MatchString("this repository has moved into read-only mode") {
		t.Error("pattern[3] did not match 'moved into read-only mode'")
	}
}

// TestExplicitPatternsForStats verifies the accessor returns the Readme explicit pattern.
func TestExplicitPatternsForStats(t *testing.T) {
	pats := ExplicitPatternsForStats()
	if len(pats) != 1 {
		t.Fatalf("ExplicitPatternsForStats returned %d patterns, want 1", len(pats))
	}
	p := pats[0]
	if p.Label == "" {
		t.Error("explicit pattern has empty label")
	}
	cases := []struct {
		text string
		want bool
	}{
		{"this project is deprecated", true},
		{"this package is now abandoned", true},
		{"no longer maintained", true},
		{"reached end of life", true},
		{"just a normal readme", false},
	}
	for _, c := range cases {
		if got := p.Rx.MatchString(c.text); got != c.want {
			t.Errorf("explicit pattern on %q = %v, want %v", c.text, got, c.want)
		}
	}
}

// TestContextualReadOnlyMode verifies the "moved into read-only mode" contextual pattern.
func TestContextualReadOnlyMode(t *testing.T) {
	text := "This repository has moved into read-only mode."
	res := DetectLifecycle(LifecycleDetectOpts{
		Source:   SourceReadme,
		RepoName: "testrepo",
		Text:     text,
	})
	if !res.Matched || res.Kind != KindContextual {
		t.Errorf("expected KindContextual match for read-only mode, got Matched=%v Kind=%d", res.Matched, res.Kind)
	}
}

// TestNewSuccessorPatterns verifies expanded successor extraction.
func TestNewSuccessorPatterns(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"replaced with", "This project is deprecated permanently. Replaced with newlib.", "newlib"},
		{"successor is", "This project is deprecated permanently. The successor is betterlib.", "betterlib"},
		{"consolidated into", "This project is deprecated permanently. Consolidated into monorepo.", "monorepo"},
		{"integrated into", "This project is deprecated permanently. Integrated into @org/core.", "@org/core"},
		{"X supersedes (readme)", "This project is deprecated permanently. betterlib supersedes this.", "betterlib"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := DetectLifecycle(LifecycleDetectOpts{
				Source:   SourceReadme,
				RepoName: "testrepo",
				Text:     tc.text,
			})
			if !res.Matched {
				t.Fatal("expected match")
			}
			if res.Successor != tc.want {
				t.Errorf("Successor=%q want %q", res.Successor, tc.want)
			}
		})
	}
}

// TestDateBasedPatterns verifies date-anchored contextual EOL patterns.
func TestDateBasedPatterns(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"will be removed by date", "This library will be removed by March 15, 2026."},
		{"will be sunset on date", "This package will be sunset on 2025-06-30."},
		{"support continues until", "Support continues until 2025-12-31."},
		{"security fixes until", "Security fixes until 2025/06/30 only."},
		{"after date no support", "After 2025-06-01 no support will be provided."},
		{"after date without updates", "After January 1, 2026 without updates."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := DetectLifecycle(LifecycleDetectOpts{
				Source:   SourceReadme,
				RepoName: "testrepo",
				Text:     tc.text,
			})
			if !res.Matched {
				t.Errorf("expected match, got none for %q", tc.text)
			}
		})
	}
}

// TestDateExtraction verifies that Date is populated from date-anchored contextual patterns.
func TestDateExtraction(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		wantDate string
	}{
		{"will be removed by ISO date", "This library will be removed on 2025-06-30.", "2025-06-30"},
		{"will be retired by slash date", "This package will be retired by 2025/12/31.", "2025/12/31"},
		{"will be sunset on month-day-year", "This package will be sunset on March 15, 2026.", "March 15, 2026"},
		{"support continues until", "Support continues until 2025-12-31.", "2025-12-31"},
		{"security fixes until", "Security fixes until 2025/06/30 only.", "2025/06/30"},
		{"after date no support", "After 2025-06-01 no support will be provided.", "2025-06-01"},
		{"after month-day-year without updates", "After January 1, 2026 without updates.", "January 1, 2026"},
		{"non-date contextual pattern", "This project has reached end-of-life.", ""},
		{"strong phrase no date", "This project is no longer maintained.", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := DetectLifecycle(LifecycleDetectOpts{
				Source:   SourceReadme,
				RepoName: "testrepo",
				Text:     tc.text,
			})
			if !res.Matched {
				t.Fatalf("expected match for %q", tc.text)
			}
			if res.Date != tc.wantDate {
				t.Errorf("Date=%q, want %q", res.Date, tc.wantDate)
			}
		})
	}
}

// TestDetectionKindString verifies the String() method on DetectionKind.
func TestDetectionKindString(t *testing.T) {
	cases := []struct {
		kind DetectionKind
		want string
	}{
		{KindNone, ""},
		{KindStrong, "strong"},
		{KindContextual, "contextual"},
		{KindExplicit, "explicit"},
	}
	for _, tc := range cases {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("DetectionKind(%d).String()=%q, want %q", tc.kind, got, tc.want)
		}
	}
}
