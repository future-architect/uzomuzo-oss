package eoltext

import "testing"

// FuzzDetectLifecyclePyPI fuzzes the unified lifecycle detector with PyPI source.
func FuzzDetectLifecyclePyPI(f *testing.F) {
	seeds := []string{
		"This project is deprecated. Use newpkg instead.",
		"This package is no longer maintained.",
		"Not deprecated. Still maintained.",
		"Deprecated function foo() in this project.",
		"This project has reached end of life.",
		"Will be removed on 2025-06-30.",
		"",
		"Just a normal package description.",
		"Python 3.8 no longer supported.",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = DetectLifecycle(LifecycleDetectOpts{
			Source:      SourcePyPI,
			PackageName: "testpkg",
			Text:        input,
		})
	})
}

// FuzzDetectLifecycleReadme fuzzes the unified lifecycle detector with Readme source.
func FuzzDetectLifecycleReadme(f *testing.F) {
	seeds := []string{
		"# My Project\n\nThis project is deprecated.",
		"This repository is no longer maintained. Use @new/pkg instead.",
		"Moved into read-only mode.",
		"## Changelog\n\n### v1.0\n- Initial release",
		"",
		"Active project with no deprecation.",
		"Deprecated class MyClass is no longer used.",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = DetectLifecycle(LifecycleDetectOpts{
			Source:   SourceReadme,
			RepoName: "testrepo",
			Text:     input,
		})
	})
}

// FuzzDetectLifecycleShortMessage fuzzes the unified lifecycle detector with short message source.
func FuzzDetectLifecycleShortMessage(f *testing.F) {
	seeds := []string{
		"This package is deprecated. Use newpkg instead.",
		"Package has been superseded by better-pkg.",
		"No longer supported.",
		"",
		"Just a normal message.",
		"Critical bugs found.",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = DetectLifecycle(LifecycleDetectOpts{
			Source:      SourceShortMessage,
			PackageName: "testpkg",
			Text:        input,
		})
	})
}

// FuzzExtractDateFromSubmatch fuzzes date extraction from regex submatches.
func FuzzExtractDateFromSubmatch(f *testing.F) {
	seeds := []string{
		"2025-06-30",
		"2025/06/30",
		"June 30, 2025",
		"June 30 2025",
		"",
		"not-a-date",
		"12345",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = extractDateFromSubmatch([]string{input})
	})
}
