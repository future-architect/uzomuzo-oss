package eoltext

import (
	"strings"
	"testing"
)

var benchReadme = strings.Repeat(
	"# example-lib\n\nA utility library for parsing configuration files.\n\n"+
		"## Installation\n\n    npm install example-lib\n\n"+
		"## Usage\n\nSee the documentation for details on the API surface.\n\n",
	40,
) + "\n## Notice\n\nThis project is deprecated and no longer maintained. Please use example-lib-ng instead.\n"

func BenchmarkDetectLifecycleReadme(b *testing.B) {
	opts := LifecycleDetectOpts{
		Source:   SourceReadme,
		RepoName: "example-lib",
		Text:     benchReadme,
	}
	got := DetectLifecycle(opts)
	b.Logf("setup detection result: %+v", got)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DetectLifecycle(opts)
	}
}

func BenchmarkDetectLifecyclePyPI(b *testing.B) {
	opts := LifecycleDetectOpts{
		Source:      SourcePyPI,
		PackageName: "example-lib",
		Text: "Deprecated utility library\n" + strings.Repeat(
			"This package provides helpers for reading configuration files. ", 60,
		) + "\nThis package is deprecated; use example-lib-ng.",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DetectLifecycle(opts)
	}
}
