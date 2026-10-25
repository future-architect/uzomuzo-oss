package pypi

import (
	"testing"
	"time"
)

// FuzzEarlierUpload checks that earlierUpload never panics, keeps current for
// an invalid time, and otherwise returns the earlier of the two times, treating
// a zero current value as unset.
func FuzzEarlierUpload(f *testing.F) {
	for _, seed := range []string{
		"",
		"not-a-time",
		"2026-10-21T00:00:00Z",
		"2026-10-21T19:22:08.353431Z",
		"2026-10-21T09:00:00+09:00",
		"2026-10-21T00:00:00",
		"0001-01-01T00:00:00Z",
	} {
		f.Add(seed, int64(0))
		f.Add(seed, time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC).UnixNano())
	}
	f.Fuzz(func(t *testing.T, raw string, currentNanos int64) {
		var current time.Time
		if currentNanos != 0 {
			current = time.Unix(0, currentNanos).UTC()
		}
		got := earlierUpload(current, raw)
		at, err := time.Parse(time.RFC3339Nano, raw)
		switch {
		case err != nil:
			if !got.Equal(current) {
				t.Fatalf("invalid %q changed current %v to %v", raw, current, got)
			}
		case current.IsZero() || at.Before(current):
			if !got.Equal(at) {
				t.Fatalf("earlierUpload(%v, %q) = %v, want %v", current, raw, got, at)
			}
		default:
			if !got.Equal(current) {
				t.Fatalf("earlierUpload(%v, %q) = %v, want current", current, raw, got)
			}
		}
	})
}
