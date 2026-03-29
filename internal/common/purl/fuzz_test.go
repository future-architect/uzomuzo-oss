package purl

import "testing"

// FuzzParse fuzzes the unified PURL parser for panics and unexpected crashes.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"pkg:golang/github.com/example/repo@v1.0.0",
		"pkg:maven/org.apache.commons/commons-lang3@3.12.0",
		"pkg:npm/%40angular/core@16.0.0",
		"pkg:pypi/requests@2.28.0",
		"pkg:nuget/Newtonsoft.Json@13.0.1",
		"pkg:cargo/serde@1.0.0",
		"pkg:composer/symfony/console@6.0.0",
		"",
		"not-a-purl",
		"pkg:",
		"pkg:unknown/",
		"pkg:golang/@v1.0.0",
		"pkg:maven/group:artifact@1.0",
		"pkg:npm/name@version?qualifiers=value#subpath",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	p := NewParser()
	f.Fuzz(func(t *testing.T, input string) {
		parsed, _ := p.Parse(input)
		if parsed != nil {
			// Exercise accessor methods to check for panics
			_ = parsed.GetEcosystem()
			_ = parsed.GetPackageName()
			_ = parsed.Namespace()
			_ = parsed.Name()
			_ = parsed.Version()
		}
	})
}

// FuzzNormalizeMavenCollapsedCoordinates fuzzes Maven PURL normalization.
func FuzzNormalizeMavenCollapsedCoordinates(f *testing.F) {
	seeds := []string{
		"pkg:maven/org.slf4j:slf4j-api@1.7.36",
		"pkg:maven/org.slf4j/slf4j-api@1.7.36",
		"pkg:maven/org.slf4j:slf4j-api?classifier=sources",
		"pkg:maven/org.slf4j%3Aslf4j-api@1.7.36",
		"pkg:maven/",
		"pkg:maven/nogroup@1.0",
		"",
		"not-maven",
		"pkg:maven/a:b:c@1.0",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = NormalizeMavenCollapsedCoordinates(input)
	})
}

// FuzzIsStableVersion fuzzes version stability detection.
func FuzzIsStableVersion(f *testing.F) {
	seeds := []string{
		"1.0.0", "2.0.0-alpha", "3.0.0-beta.1", "4.0.0-rc1",
		"1.0.0-SNAPSHOT", "v1.2.3", "", "dev-master", "1.0.0-preview.2",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = IsStableVersion(input)
	})
}
