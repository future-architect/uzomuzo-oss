package eoltext

import "testing"

// Tests for PyPI Option C logic (changelog prelude restriction for strong/contextual phrases).

// strong/contextual phrase only after changelog heading should be ignored ("retired" not in explicit pattern)
func TestPyPIChangelog_StrongAfterHeadingIgnored(t *testing.T) {
	// Build a prelude >200 chars so trimming activates.
	prelude := "Intro paragraph describing the package and its purpose. " +
		"It provides multiple features and has extensive documentation. " +
		"Users rely on it for many workflows and examples are included. " +
		"Further introductory material continues here describing usage."
	if len(prelude) < 210 { // safety check so test intent is clear
		t.Fatalf("prelude too short: %d", len(prelude))
	}
	text := prelude + "\n\nChangelog\n\nThis project is retired."
	res := DetectLifecycle(LifecycleDetectOpts{Source: SourcePyPI, PackageName: "sample", Text: text})
	if res.Matched {
		t.Fatalf("expected no match, got %+v", res)
	}
}

// explicit phrase after changelog heading is still detected anywhere
func TestPyPIChangelog_ExplicitAfterHeadingDetected(t *testing.T) {
	text := `Intro paragraph about the library providing useful features.

History

This project is deprecated.`
	res := DetectLifecycle(LifecycleDetectOpts{Source: SourcePyPI, PackageName: "sample", Text: text})
	if !res.Matched || res.Kind != KindExplicit {
		t.Fatalf("expected explicit match, got %+v", res)
	}
}

// early changelog heading (<200 chars) should not trigger trimming; strong phrase after heading should match
func TestPyPIChangelog_EarlyHeadingNoTrim(t *testing.T) {
	text := `Changelog\n\nProject has been superseded by NewLib for future development.`
	res := DetectLifecycle(LifecycleDetectOpts{Source: SourcePyPI, PackageName: "sample", Text: text})
	if !res.Matched || res.Kind != KindStrong {
		t.Fatalf("expected strong match, got %+v", res)
	}
}
