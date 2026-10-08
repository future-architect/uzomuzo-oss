package npmjs

import (
	"strings"
	"testing"
)

// FuzzParsePublishHistory checks that packument decoding never panics and never
// keeps a package-level key or a zero time as a version.
func FuzzParsePublishHistory(f *testing.F) {
	f.Add(`{"time":{"created":"2014-01-01T00:00:00Z","1.0.0":"2020-01-01T00:00:00Z","x":1},"versions":{"1.0.0":{"name":"a"}}}`)
	f.Add(`{"time":{"unpublished":{"time":"2020-01-01T00:00:00Z"}}}`)
	f.Add(`{"versions":{"1.0.0":"not an object"}}`)
	f.Add(`[]`)
	f.Add(`{"time":{"1.0.0":"0001-01-01T00:00:00Z"}}`)
	f.Fuzz(func(t *testing.T, body string) {
		h, err := parsePublishHistory(strings.NewReader(body))
		if err != nil {
			return
		}
		for v, at := range h.PublishedAt {
			if at.IsZero() || v == "created" || v == "modified" || v == "unpublished" {
				t.Fatalf("unexpected entry %q=%v", v, at)
			}
		}
	})
}
