package analysis

import (
	"reflect"
	"strings"
	"testing"
	"time"

	cfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// TestDetectFreshDormantRelease covers the single-line rule: a stable release
// that returns from silence is flagged for freshFor, follow-ups in the same
// burst inherit the silence, and prereleases and first publications are not
// flagged. See ADR-0028.
func TestDetectFreshDormantRelease(t *testing.T) {
	t.Parallel()

	const week = 7 * 24 * time.Hour
	// ansi-styles-like: one release on one line after almost three years.
	quiet := history(t, map[string]string{
		"6.2.1": "2022-11-01T00:00:00Z",
		"6.2.2": "2025-09-08T13:16:00Z",
		"6.2.3": "2025-09-08T20:00:00Z",
		"6.2.4": "2025-09-20T00:00:00Z",
	}, "6.2.2")
	pre := history(t, map[string]string{
		"1.0.0":       "2020-01-01T00:00:00Z",
		"2.0.0-rc.1":  "2025-01-01T00:00:00Z",
		"2.0.0":       "2025-01-01T01:00:00Z",
		"2.0.0+build": "2025-01-01T02:00:00Z",
	})
	first := history(t, map[string]string{"1.0.0": "2025-01-01T00:00:00Z"})

	tests := []struct {
		name        string
		h           *ReleaseHistory
		version     string
		now         string
		wantSilent  int
		wantVers    []string
		wantRemoved bool
	}{
		{"first after silence", quiet, "6.2.2", "2025-09-09T00:00:00Z", 1042, []string{"6.2.2", "6.2.3"}, true},
		{"follow-up in the same burst inherits the silence", quiet, "6.2.3", "2025-09-09T00:00:00Z", 1042, []string{"6.2.2", "6.2.3"}, false},
		{"exactly freshFor old", quiet, "6.2.2", "2025-09-15T13:16:00Z", 1042, []string{"6.2.2", "6.2.3"}, true},
		{"older than freshFor", quiet, "6.2.2", "2025-09-15T13:16:01Z", 0, nil, false},
		{"next release after the burst is not a return", quiet, "6.2.4", "2025-09-21T00:00:00Z", 0, nil, false},
		{"seen before the follow-up existed", quiet, "6.2.2", "2025-09-08T14:00:00Z", 1042, []string{"6.2.2"}, true},
		{"prerelease target", pre, "2.0.0-rc.1", "2025-01-02T00:00:00Z", 0, nil, false},
		{"stable after its own prerelease", pre, "2.0.0", "2025-01-02T00:00:00Z", 1827, []string{"2.0.0-rc.1", "2.0.0", "2.0.0+build"}, false},
		{"build metadata is stable", pre, "2.0.0+build", "2025-01-02T00:00:00Z", 1827, []string{"2.0.0-rc.1", "2.0.0", "2.0.0+build"}, false},
		{"first publication", first, "1.0.0", "2025-01-02T00:00:00Z", 0, nil, false},
		{"nil history", nil, "1.0.0", "2025-01-02T00:00:00Z", 0, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DetectFreshDormantRelease(tt.h, tt.version, mustTime(t, tt.now), year, week)
			if tt.wantVers == nil {
				if got != nil {
					t.Fatalf("got %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("got nil, want a release")
			}
			if got.SilentDays != tt.wantSilent {
				t.Errorf("SilentDays = %d, want %d", got.SilentDays, tt.wantSilent)
			}
			if !reflect.DeepEqual(got.Versions, tt.wantVers) {
				t.Errorf("Versions = %v, want %v", got.Versions, tt.wantVers)
			}
			if got.Removed != tt.wantRemoved {
				t.Errorf("Removed = %v, want %v", got.Removed, tt.wantRemoved)
			}
		})
	}
}

// TestEvaluateDormantRelease pins which rule wins, the clearing time, and that
// the marker does not depend on the lifecycle label. See ADR-0028.
func TestEvaluateDormantRelease(t *testing.T) {
	t.Parallel()

	c := cfg.GetDefaultLifecycle()
	nodeIPC := history(t, map[string]string{
		"12.0.0": "2024-08-12T16:28:35Z",
		"12.0.1": "2026-05-14T14:25:30Z",
		"9.2.3":  "2026-05-14T14:26:01Z",
	})
	quiet := history(t, map[string]string{
		"6.2.1": "2022-11-01T00:00:00Z",
		"6.2.2": "2025-09-08T13:16:00Z",
	})
	tests := []struct {
		name      string
		h         *ReleaseHistory
		version   string
		now       string
		wantKind  DormantReleaseKind
		wantUntil string
	}{
		{"burst across lines wins over fresh", nodeIPC, "12.0.1", "2026-05-15T00:00:00Z", DormantReleaseBurstAcrossLines, "2027-05-14T14:25:30Z"},
		{"burst stays after the fresh period", nodeIPC, "12.0.1", "2026-10-08T00:00:00Z", DormantReleaseBurstAcrossLines, "2027-05-14T14:25:30Z"},
		{"single line is fresh", quiet, "6.2.2", "2025-09-09T00:00:00Z", DormantReleaseFresh, "2025-09-15T13:16:00Z"},
		{"single line after the fresh period", quiet, "6.2.2", "2025-10-01T00:00:00Z", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &Analysis{Package: &Package{Version: tt.version}, ReleaseHistory: tt.h,
				AxisResults: map[AssessmentAxis]*AssessmentResult{LifecycleAxis: {Label: string(LabelStalled)}}}
			got := a.EvaluateDormantRelease(mustTime(t, tt.now), c)
			if tt.wantKind == "" {
				if got != nil {
					t.Fatalf("got %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("got nil")
			}
			if got.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, tt.wantKind)
			}
			if want := mustTime(t, tt.wantUntil); !got.Until.Equal(want) {
				t.Errorf("Until = %v, want %v", got.Until, want)
			}
		})
	}
	if got := (&Analysis{}).EvaluateDormantRelease(time.Now(), c); got != nil {
		t.Errorf("no package: got %+v, want nil", got)
	}
}

// TestDormantReleaseNotice pins when the marker text is shown and how it
// reflects the OSV malicious check. See ADR-0028.
func TestDormantReleaseNotice(t *testing.T) {
	t.Parallel()

	published := time.Date(2025, 9, 8, 13, 16, 0, 0, time.UTC)
	fresh := func(removed bool) *DormantRelease {
		return &DormantRelease{Kind: DormantReleaseFresh, Until: published.Add(7 * 24 * time.Hour),
			Burst: &DormantBurst{Version: "6.2.2", PublishedAt: published, SilentDays: 1042, Versions: []string{"6.2.2"}, Lines: []string{"6"}, Removed: removed}}
	}
	burst := &DormantRelease{Kind: DormantReleaseBurstAcrossLines,
		Burst: &DormantBurst{Version: "12.0.1", PublishedAt: published, SilentDays: 639, Versions: []string{"12.0.1", "9.2.3"}, Lines: []string{"9", "12"}}}
	overridden := map[AssessmentAxis]*AssessmentResult{LifecycleAxis: {Label: string(LabelReviewNeeded),
		Signals: []Signal{sig(SignalDormantReleaseBurst, "12.0.1, 9.2.3")}}}
	stalled := map[AssessmentAxis]*AssessmentResult{LifecycleAxis: {Label: string(LabelStalled)}}

	tests := []struct {
		name     string
		a        *Analysis
		contains []string
		empty    bool
	}{
		{"no marker", &Analysis{}, nil, true},
		{"malicious takes priority", &Analysis{DormantRelease: fresh(false), MaliciousState: &MaliciousState{Status: MaliciousStatusFlagged}}, nil, true},
		{"burst already explained by the label", &Analysis{DormantRelease: burst, AxisResults: overridden}, nil, true},
		{"burst under Stalled is shown", &Analysis{DormantRelease: burst, AxisResults: stalled}, []string{"639 days", "9.x, 12.x"}, false},
		{"fresh, clean OSV check", &Analysis{DormantRelease: fresh(false), MaliciousState: &MaliciousState{Status: MaliciousStatusClean}},
			[]string{"2025-09-08", "1042 days", "No OSV malicious-package record names it yet.", "_npmUser", "clears on 2025-09-15"}, false},
		{"fresh, OSV lookup failed", &Analysis{DormantRelease: fresh(false), MaliciousState: &MaliciousState{Status: MaliciousStatusLookupFailed}},
			[]string{"did not complete"}, false},
		{"fresh, removed", &Analysis{DormantRelease: fresh(true)}, []string{"npm has since removed this version."}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.a.DormantReleaseNotice()
			if tt.empty != (got == "") {
				t.Fatalf("notice = %q, want empty=%v", got, tt.empty)
			}
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("notice %q missing %q", got, want)
				}
			}
		})
	}
}
