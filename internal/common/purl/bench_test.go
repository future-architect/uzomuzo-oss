package purl

import "testing"

var benchPURLs = []string{
	"pkg:maven/org.slf4j/slf4j-api@2.0.16",
	"pkg:golang/github.com/gin-gonic/gin@v1.10.0",
	"pkg:npm/@babel/core@7.24.0",
	"pkg:npm/express@4.18.2",
	"pkg:pypi/requests",
}

func BenchmarkParserParse(b *testing.B) {
	p := NewParser()
	for _, s := range benchPURLs {
		parsed, err := p.Parse(s)
		if err != nil {
			b.Fatalf("Parse(%q) failed during setup: %v", s, err)
		}
		if parsed == nil {
			b.Fatalf("Parse(%q) returned nil during setup", s)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range benchPURLs {
			if _, err := p.Parse(s); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkIsStableVersion(b *testing.B) {
	versions := []string{"1.2.3", "v1.10.0", "2.0.0-rc.1", "0.0.0-20240101120000-abcdef123456", "7.24.0"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, v := range versions {
			_ = IsStableVersion(v)
		}
	}
}
