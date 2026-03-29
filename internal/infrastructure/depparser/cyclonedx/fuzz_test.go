package cyclonedx

import (
	"context"
	"testing"
)

// FuzzCycloneDXParse fuzzes CycloneDX SBOM JSON parsing for panics and crashes.
func FuzzCycloneDXParse(f *testing.F) {
	seeds := []string{
		`{"components":[]}`,
		`{"components":[{"purl":"pkg:golang/example.com/foo@v1.0.0"}]}`,
		`{"components":[{"purl":"pkg:maven/org.example/lib@1.0","components":[{"purl":"pkg:maven/org.example/sub@2.0"}]}]}`,
		`{"components":[{"purl":"invalid-purl"}]}`,
		`{"components":[{"purl":""}]}`,
		`{}`,
		``,
		`not-json`,
		`{"components":null}`,
		`{"components":[{"components":[{"components":[{"purl":"pkg:npm/deep@1.0"}]}]}]}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	p := &Parser{}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = p.Parse(context.Background(), data)
	})
}

// FuzzNormalizePURL fuzzes the internal PURL normalization used by CycloneDX parser.
func FuzzNormalizePURL(f *testing.F) {
	seeds := []string{
		"pkg:golang/example.com/foo@v1.0.0",
		"pkg:maven/org.example/lib@1.0?classifier=sources",
		"pkg:npm/%40scope/name@2.0.0#subpath",
		"pkg:pypi/requests@2.28.0",
		"",
		"not-a-purl",
		"pkg:unknown/name@version",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_, _ = normalizePURL(input)
	})
}
