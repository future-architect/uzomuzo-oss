package analysis

import (
	"strings"
	"testing"
	"time"
)

// FuzzReleaseLine checks that releaseLine and isPrerelease never panic on
// registry-controlled version strings and never report an empty line as ok.
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

// FuzzDetectDormantBurstMembership checks that every version listed in a
// detected burst resolves to the same burst when it is the one asked about.
func FuzzDetectDormantBurstMembership(f *testing.F) {
	f.Add([]byte{0, 24, 0}, uint8(24))
	f.Add([]byte{0, 24, 24}, uint8(24))
	f.Add([]byte{0, 200, 1, 23, 1}, uint8(100))
	versions := []string{"1.0.0", "1.0.1", "2.0.0", "2.0.1", "0.3.0", "3.0.0", "0.4.0", "4.0.0"}
	f.Fuzz(func(t *testing.T, gapsHours []byte, silenceHours uint8) {
		if len(gapsHours) > len(versions) {
			gapsHours = gapsHours[:len(versions)]
		}
		h := &ReleaseHistory{PublishedAt: map[string]time.Time{}}
		at := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		for i, g := range gapsHours {
			at = at.Add(time.Duration(g) * time.Hour)
			h.PublishedAt[versions[i]] = at
		}
		minSilence := time.Duration(silenceHours) * time.Hour
		for v := range h.PublishedAt {
			b := DetectDormantBurst(h, v, at, minSilence, 0)
			if b == nil {
				continue
			}
			for _, other := range b.Versions {
				ob := DetectDormantBurst(h, other, at, minSilence, 0)
				if ob == nil || strings.Join(ob.Versions, ",") != strings.Join(b.Versions, ",") {
					t.Fatalf("%s sees burst %v, but %s sees %+v", v, b.Versions, other, ob)
				}
			}
		}
	})
}
