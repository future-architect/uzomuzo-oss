package eolevaluator

import "testing"

// FuzzParseComposerFromPURL fuzzes Composer/Packagist PURL parsing.
func FuzzParseComposerFromPURL(f *testing.F) {
	seeds := []string{
		"pkg:composer/symfony/console@6.0.0",
		"pkg:packagist/vendor/name@1.0",
		"pkg:composer/vendor/name?extra=1",
		"pkg:composer/vendor/name#subpath",
		"",
		"pkg:npm/name@1.0",
		"pkg:composer/",
		"pkg:composer/vendor",
		"not-a-purl",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_, _ = parseComposerFromPURL(input)
	})
}

// FuzzParseNuGetIDFromPURL fuzzes NuGet PURL ID extraction.
func FuzzParseNuGetIDFromPURL(f *testing.F) {
	seeds := []string{
		"pkg:nuget/Newtonsoft.Json@13.0.1",
		"pkg:nuget/Newtonsoft.Json",
		"pkg:nuget/Package?qualifiers",
		"pkg:nuget/Package#subpath",
		"",
		"pkg:npm/name@1.0",
		"pkg:nuget/",
		"not-a-purl",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = parseNuGetIDFromPURL(input)
	})
}

// FuzzParseMavenFromPURL fuzzes Maven PURL parsing.
func FuzzParseMavenFromPURL(f *testing.F) {
	seeds := []string{
		"pkg:maven/org.apache.commons/commons-lang3@3.12.0",
		"pkg:maven/group/artifact@1.0?classifier=sources",
		"pkg:maven/group/artifact#subpath",
		"pkg:maven/group/artifact",
		"",
		"pkg:npm/name@1.0",
		"pkg:maven/",
		"pkg:maven/group",
		"not-a-purl",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_, _, _ = parseMavenFromPURL(input)
	})
}
