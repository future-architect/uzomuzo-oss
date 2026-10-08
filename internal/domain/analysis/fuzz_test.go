package analysis

import (
	"testing"
)

func FuzzReleaseLine(f *testing.F) {
	for _, s := range []string{"12.0.1", "0.7.29", "0.1-alpha", "v2", "0", "", "0.", "-1.0.0", "1e9.0.0", "99999999999999999999.0.0"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, v string) {
		l, ok := releaseLine(v)
		if ok && l == "" {
			t.Fatalf("releaseLine(%q) = ok with empty line", v)
		}
		_ = isPrerelease(v)
	})
}
