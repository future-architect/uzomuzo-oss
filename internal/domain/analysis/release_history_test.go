package analysis

import (
	"reflect"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad time %q: %v", s, err)
	}
	return v
}

// history builds a ReleaseHistory from version→RFC3339 pairs; versions listed
// in removed are left out of Installable.
func history(t *testing.T, published map[string]string, removed ...string) *ReleaseHistory {
	t.Helper()
	h := &ReleaseHistory{Registry: RegistryNpm, PublishedAt: map[string]time.Time{}, Installable: map[string]struct{}{}}
	for v, s := range published {
		h.PublishedAt[v] = mustTime(t, s)
		h.Installable[v] = struct{}{}
	}
	for _, v := range removed {
		delete(h.Installable, v)
	}
	return h
}

const year = 365 * 24 * time.Hour

// TestDetectDormantBurst replays the burst timestamps of real incidents
// (registry.npmjs.org "time", fetched 2026-10-08; the release before each
// silence is rounded to midnight, so silences differ from ADR-0026 by a day) and of legitimate releases
// that the rule must leave alone. See ADR-0026.
func TestDetectDormantBurst(t *testing.T) {
	t.Parallel()

	// node-ipc: 12.0.0 is the last legitimate release; 12.0.1, 9.2.3 and 9.1.6
	// were published by a hijacked dormant account 55 seconds apart and later
	// removed. 9.2.2 is the previous release on the 9 line.
	nodeIPC := history(t, map[string]string{
		"9.2.2":  "2022-03-15T00:00:00Z",
		"12.0.0": "2024-08-12T16:28:35Z",
		"12.0.1": "2026-05-14T14:25:30Z",
		"9.2.3":  "2026-05-14T14:26:01Z",
		"9.1.6":  "2026-05-14T14:26:25Z",
		"14.0.0": "2026-08-24T22:17:14Z",
	}, "12.0.1", "9.2.3", "9.1.6")
	afterNodeIPC := mustTime(t, "2026-10-08T00:00:00Z")

	// rc (2021-11-04): three lines within 28 seconds after 1,257 days.
	rc := history(t, map[string]string{
		"1.2.8": "2018-05-26T00:00:00Z",
		"1.2.9": "2021-11-04T15:30:19Z",
		"1.3.9": "2021-11-04T15:30:34Z",
		"2.3.9": "2021-11-04T15:30:47Z",
	}, "1.2.9", "1.3.9", "2.3.9")
	afterRC := mustTime(t, "2021-11-05T00:00:00Z")

	// is (2025-07-19): 3.3.1 and 5.0.0 seven hours apart, then the clean 3.3.2.
	is := history(t, map[string]string{
		"3.3.0": "2018-12-14T00:00:00Z",
		"3.3.1": "2025-07-19T11:40:30Z",
		"5.0.0": "2025-07-19T18:36:23Z",
		"3.3.2": "2025-07-19T19:13:33Z",
	}, "3.3.1", "5.0.0")
	afterIs := mustTime(t, "2025-07-20T00:00:00Z")

	// event-stream 3.3.5 (2018-09-05): a takeover on one line only. The rule
	// is not meant to see it; this pins the documented miss.
	eventStream := history(t, map[string]string{
		"3.3.4": "2016-07-17T00:00:00Z",
		"3.3.5": "2018-09-05T00:00:00Z",
		"3.3.6": "2018-09-09T08:28:59Z",
	})

	// axios (2026-03-31): a hijack of an active package — no silence.
	axios := history(t, map[string]string{
		"1.14.0": "2026-03-27T00:00:00Z",
		"1.14.1": "2026-03-31T00:21:00Z",
		"0.30.4": "2026-03-31T01:00:00Z",
	})

	// braces-like legitimate fix: one release after five years.
	singleLine := history(t, map[string]string{
		"3.0.2": "2019-04-07T00:00:00Z",
		"3.0.3": "2024-05-20T00:00:00Z",
	})

	tests := []struct {
		name        string
		h           *ReleaseHistory
		version     string
		now         time.Time
		maxAge      time.Duration
		wantNil     bool
		wantSilent  int
		wantLines   []string
		wantVers    []string
		wantRemoved bool
	}{
		{name: "node-ipc 12.0.1 fires", h: nodeIPC, version: "12.0.1", now: afterNodeIPC, maxAge: year,
			wantSilent: 639, wantLines: []string{"9", "12"}, wantVers: []string{"12.0.1", "9.2.3", "9.1.6"}, wantRemoved: true},
		{name: "node-ipc 9.1.6 (last of the burst) fires with the same burst", h: nodeIPC, version: "9.1.6", now: afterNodeIPC, maxAge: year,
			wantSilent: 639, wantLines: []string{"9", "12"}, wantVers: []string{"12.0.1", "9.2.3", "9.1.6"}, wantRemoved: true},
		{name: "node-ipc 12.0.0, a single release, does not fire", h: nodeIPC, version: "12.0.0", now: afterNodeIPC, maxAge: 0, wantNil: true},
		{name: "node-ipc 14.0.0 follows the burst by 102 days and does not fire", h: nodeIPC, version: "14.0.0", now: afterNodeIPC, maxAge: year, wantNil: true},
		{name: "rc fires across 1 and 2", h: rc, version: "1.2.9", now: afterRC, maxAge: year,
			wantSilent: 1258, wantLines: []string{"1", "2"}, wantVers: []string{"1.2.9", "1.3.9", "2.3.9"}, wantRemoved: true},
		{name: "is 3.3.2, the clean follow-up, is inside the same burst", h: is, version: "3.3.2", now: afterIs, maxAge: year,
			wantSilent: 2409, wantLines: []string{"3", "5"}, wantVers: []string{"3.3.1", "5.0.0", "3.3.2"}, wantRemoved: false},
		{name: "event-stream: one line only is a documented miss", h: eventStream, version: "3.3.5", now: mustTime(t, "2018-09-10T00:00:00Z"), maxAge: year, wantNil: true},
		{name: "axios: active package, no silence", h: axios, version: "1.14.1", now: mustTime(t, "2026-04-01T00:00:00Z"), maxAge: year, wantNil: true},
		{name: "single release after five years does not fire", h: singleLine, version: "3.0.3", now: mustTime(t, "2024-06-01T00:00:00Z"), maxAge: year, wantNil: true},
		{name: "older than maxAge does not fire", h: rc, version: "1.2.9", now: mustTime(t, "2022-11-05T00:00:00Z"), maxAge: year, wantNil: true},
		{name: "maxAge zero disables the age limit", h: rc, version: "1.2.9", now: mustTime(t, "2030-01-01T00:00:00Z"), maxAge: 0,
			wantSilent: 1258, wantLines: []string{"1", "2"}, wantVers: []string{"1.2.9", "1.3.9", "2.3.9"}, wantRemoved: true},
		{name: "version without a publish time", h: rc, version: "9.9.9", now: afterRC, maxAge: year, wantNil: true},
		{name: "empty version", h: rc, version: "", now: afterRC, maxAge: year, wantNil: true},
		{name: "nil history", h: nil, version: "1.2.9", now: afterRC, maxAge: year, wantNil: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DetectDormantBurst(tt.h, tt.version, tt.now, year, tt.maxAge)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("want nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("want a burst, got nil")
			}
			if got.SilentDays != tt.wantSilent {
				t.Errorf("SilentDays = %d, want %d", got.SilentDays, tt.wantSilent)
			}
			if !reflect.DeepEqual(got.Lines, tt.wantLines) {
				t.Errorf("Lines = %v, want %v", got.Lines, tt.wantLines)
			}
			if !reflect.DeepEqual(got.Versions, tt.wantVers) {
				t.Errorf("Versions = %v, want %v", got.Versions, tt.wantVers)
			}
			if got.Removed != tt.wantRemoved {
				t.Errorf("Removed = %v, want %v", got.Removed, tt.wantRemoved)
			}
			if got.Version != tt.version {
				t.Errorf("Version = %q, want %q", got.Version, tt.version)
			}
		})
	}
}

// TestDetectDormantBurst_Boundaries pins the edges of the silence, the burst
// window and the line count.
func TestDetectDormantBurst_Boundaries(t *testing.T) {
	t.Parallel()
	base := mustTime(t, "2020-01-01T00:00:00Z")
	at := func(d time.Duration) string { return base.Add(d).Format(time.RFC3339) }
	now := base.Add(3 * year)

	tests := []struct {
		name    string
		pub     map[string]string
		version string
		want    bool
	}{
		{name: "silence exactly minSilence fires",
			pub:     map[string]string{"1.0.0": at(0), "1.0.1": at(year), "2.0.1": at(year + time.Minute)},
			version: "1.0.1", want: true},
		{name: "silence one minute short does not fire",
			pub:     map[string]string{"1.0.0": at(0), "1.0.1": at(year - time.Minute), "2.0.1": at(year)},
			version: "1.0.1", want: false},
		{name: "second line exactly at the window edge fires",
			pub:     map[string]string{"1.0.0": at(0), "1.0.1": at(2 * year), "2.0.1": at(2*year + DormantBurstWindow)},
			version: "1.0.1", want: true},
		{name: "second line one second past the window does not fire",
			pub:     map[string]string{"1.0.0": at(0), "1.0.1": at(2 * year), "2.0.1": at(2*year + DormantBurstWindow + time.Second)},
			version: "1.0.1", want: false},
		{name: "chained daily releases do not stretch the window",
			pub: map[string]string{"1.0.0": at(0), "1.0.1": at(2 * year), "1.0.2": at(2*year + 20*time.Hour),
				"2.0.0": at(2*year + 40*time.Hour)},
			version: "1.0.1", want: false},
		{name: "a version past the window from the burst start is not in the burst",
			pub: map[string]string{"1.0.0": at(0), "1.0.1": at(2 * year), "2.0.1": at(2*year + time.Hour),
				"1.0.2": at(2*year + 20*time.Hour), "1.0.3": at(2*year + 40*time.Hour)},
			version: "1.0.3", want: false},
		{name: "first ever publication across lines is not a return from silence",
			pub:     map[string]string{"1.0.0": at(0), "2.0.0": at(time.Minute)},
			version: "1.0.0", want: false},
		{name: "two 0.x minors are two lines",
			pub:     map[string]string{"0.7.28": at(0), "0.7.29": at(2 * year), "0.8.0": at(2*year + time.Minute)},
			version: "0.7.29", want: true},
		{name: "two patches on one line are one line",
			pub:     map[string]string{"1.0.0": at(0), "1.0.1": at(2 * year), "1.0.2": at(2*year + time.Minute)},
			version: "1.0.1", want: false},
		{name: "a next-major prerelease opens no line",
			pub:     map[string]string{"1.0.0": at(0), "1.0.1": at(2 * year), "2.0.0-alpha.0": at(2*year + time.Hour)},
			version: "1.0.1", want: false},
		{name: "build metadata is not a prerelease",
			pub:     map[string]string{"1.0.0": at(0), "1.0.1": at(2 * year), "2.0.1+build.5": at(2*year + time.Hour)},
			version: "1.0.1", want: true},
		{name: "non-numeric versions count as no line",
			pub:     map[string]string{"1.0.0": at(0), "1.0.1": at(2 * year), "next": at(2*year + time.Minute)},
			version: "1.0.1", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DetectDormantBurst(history(t, tt.pub), tt.version, now, year, 0)
			if (got != nil) != tt.want {
				t.Fatalf("fired = %v, want %v (%+v)", got != nil, tt.want, got)
			}
		})
	}
}

func TestDetectDormantBurst_RemovedNeedsInstallableSet(t *testing.T) {
	t.Parallel()
	h := history(t, map[string]string{"1.0.0": "2018-01-01T00:00:00Z", "1.0.1": "2021-01-01T00:00:00Z", "2.0.1": "2021-01-01T00:01:00Z"})
	h.Installable = nil
	got := DetectDormantBurst(h, "1.0.1", mustTime(t, "2021-02-01T00:00:00Z"), year, year)
	if got == nil {
		t.Fatal("want a burst")
	}
	if got.Removed {
		t.Error("Removed must be false when the installable set is unknown")
	}
}

func TestReleaseLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"12.0.1", "12", true},
		{"1.0.0-beta.1", "1", true},
		{"v2.3.4", "2", true},
		{"0.7.29", "0.7", true},
		{"0.10.0", "0.10", true},
		{"0.8.0-rc.1", "0.8", true},
		{"0.1-alpha", "0.1", true},
		{"0", "", false},
		{"0.x", "", false},
		{"latest", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := releaseLine(tt.in)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("releaseLine(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestLineLess(t *testing.T) {
	t.Parallel()
	in := []string{"12", "0.10", "9", "1", "0.2"}
	want := []string{"0.2", "0.10", "1", "9", "12"}
	got := append([]string(nil), in...)
	for i := 1; i < len(got); i++ {
		for j := i; j > 0 && lineLess(got[j], got[j-1]); j-- {
			got[j], got[j-1] = got[j-1], got[j]
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

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
