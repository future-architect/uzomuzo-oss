package analysis

import (
	"errors"
	"testing"
	"time"
)

// This file adds test coverage for Analysis helper methods that were not
// previously covered in aggregates_test.go. Focus: PURL presentation, canonical
// key generation, lifecycle label precedence, and simple state accessors.

func TestAnalysis_DisplayPURL(t *testing.T) {
	var nilAnalysis *Analysis
	tests := []struct {
		name string
		a    *Analysis
		want string
	}{
		{name: "nil_receiver", a: nilAnalysis, want: ""},
		{name: "original_preferred", a: &Analysis{OriginalPURL: "pkg:npm/React", EffectivePURL: "pkg:npm/react@18.3.1"}, want: "pkg:npm/React"},
		{name: "only_effective", a: &Analysis{EffectivePURL: "pkg:npm/react@18.3.1"}, want: "pkg:npm/react@18.3.1"},
		{name: "none", a: &Analysis{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.DisplayPURL(); got != tt.want {
				t.Errorf("DisplayPURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnalysis_IsVersionResolved(t *testing.T) {
	var nilAnalysis *Analysis
	tests := []struct {
		name string
		a    *Analysis
		want bool
	}{
		{name: "nil_receiver", a: nilAnalysis, want: false},
		{name: "with_version_simple", a: &Analysis{EffectivePURL: "pkg:npm/react@1.2.3"}, want: true},
		{name: "with_version_qualifiers", a: &Analysis{EffectivePURL: "pkg:npm/react@1.2.3?foo=bar"}, want: true},
		{name: "without_version_with_qualifiers", a: &Analysis{EffectivePURL: "pkg:npm/react?foo=bar"}, want: false},
		{name: "empty_effective", a: &Analysis{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.IsVersionResolved(); got != tt.want {
				t.Errorf("IsVersionResolved() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnalysis_EnsureCanonical(t *testing.T) {
	now := time.Now() // time not used directly; keeps style similar to other tests
	_ = now
	tests := []struct {
		name string
		a    *Analysis
		want string
	}{
		{ // Original preferred over Effective when both present
			name: "from_original_version_stripped",
			a:    &Analysis{OriginalPURL: "pkg:maven/Com.Example/Lib@1.2.3", EffectivePURL: "pkg:maven/com.example/lib@1.2.3"},
			want: "pkg:maven/com.example/lib",
		},
		{ // Qualifiers preserved & lowercased
			name: "qualifiers_preserved",
			a:    &Analysis{OriginalPURL: "pkg:npm/%40scope/Package@1.0.0?foo=bar", EffectivePURL: "pkg:npm/%40scope/package@1.0.0"},
			want: "pkg:npm/%40scope/package?foo=bar",
		},
		{ // Fallback to Effective when Original empty
			name: "fallback_effective",
			a:    &Analysis{EffectivePURL: "pkg:pypi/Django@5.0.0#vuln"},
			want: "pkg:pypi/django#vuln",
		},
		{ // Already set canonical should not change
			name: "canonical_already_set",
			a:    &Analysis{OriginalPURL: "pkg:npm/React@18.0.0", CanonicalKey: "pre-set"},
			want: "pre-set",
		},
		{ // Both empty => remains empty
			name: "empty_inputs",
			a:    &Analysis{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.a.EnsureCanonical()
			if tt.a.CanonicalKey != tt.want {
				t.Errorf("EnsureCanonical() CanonicalKey = %q, want %q", tt.a.CanonicalKey, tt.want)
			}
		})
	}
}

func TestAnalysis_FinalMaintenanceStatus(t *testing.T) {
	// Helper to clone axis result map
	lr := &AssessmentResult{Axis: LifecycleAxis, Label: LabelStalled}
	tests := []struct {
		name string
		a    *Analysis
		want string
	}{
		{name: "nil_receiver", a: nil, want: "Review Needed"},
		{name: "eol_overrides_axis", a: &Analysis{EOL: EOLStatus{State: EOLEndOfLife}, AxisResults: map[AssessmentAxis]*AssessmentResult{LifecycleAxis: lr}}, want: "EOL"},
		{name: "scheduled_eol_over_axis", a: &Analysis{EOL: EOLStatus{State: EOLScheduled}, AxisResults: map[AssessmentAxis]*AssessmentResult{LifecycleAxis: lr}}, want: "Scheduled EOL"},
		{name: "axis_only", a: &Analysis{AxisResults: map[AssessmentAxis]*AssessmentResult{LifecycleAxis: lr}}, want: "Stalled"},
		{name: "none", a: &Analysis{}, want: "Review Needed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.FinalMaintenanceStatus(); got != tt.want {
				t.Errorf("FinalMaintenanceStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnalysis_HasRequestedVersionInfo(t *testing.T) {
	tests := []struct {
		name string
		a    *Analysis
		want bool
	}{
		{name: "present", a: &Analysis{ReleaseInfo: &ReleaseInfo{RequestedVersion: &VersionDetail{Version: "1.0.0"}}}, want: true},
		{name: "empty_version", a: &Analysis{ReleaseInfo: &ReleaseInfo{RequestedVersion: &VersionDetail{Version: ""}}}, want: false},
		{name: "nil_requested", a: &Analysis{ReleaseInfo: &ReleaseInfo{}}, want: false},
		{name: "nil_release_info", a: &Analysis{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.HasRequestedVersionInfo(); got != tt.want {
				t.Errorf("HasRequestedVersionInfo() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnalysis_HasRecentHumanCommit(t *testing.T) {
	now := time.Now()
	twentyDaysAgo := now.AddDate(0, 0, -20)
	sixtyDaysAgo := now.AddDate(0, 0, -60)
	tests := []struct {
		name string
		a    *Analysis
		days int
		want bool
	}{
		{name: "recent", a: &Analysis{RepoState: &RepoState{LatestHumanCommit: &twentyDaysAgo}}, days: 30, want: true},
		{name: "old", a: &Analysis{RepoState: &RepoState{LatestHumanCommit: &sixtyDaysAgo}}, days: 30, want: false},
		{name: "nil_repo_state", a: &Analysis{}, days: 30, want: false},
		{name: "nil_commit_time", a: &Analysis{RepoState: &RepoState{}}, days: 30, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.HasRecentHumanCommit(tt.days); got != tt.want {
				t.Errorf("HasRecentHumanCommit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnalysis_GetLastHumanCommitYears(t *testing.T) {
	now := time.Now()
	oneYearAgo := now.AddDate(-1, 0, 0)
	tests := []struct {
		name string
		a    *Analysis
		min  float64
		max  float64
	}{
		{name: "approx_one_year", a: &Analysis{RepoState: &RepoState{LatestHumanCommit: &oneYearAgo}}, min: 0.9, max: 1.1},
		{name: "no_commit", a: &Analysis{}, min: 900, max: 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.GetLastHumanCommitYears()
			if got < tt.min || got > tt.max {
				t.Errorf("GetLastHumanCommitYears() = %v, want in [%v,%v]", got, tt.min, tt.max)
			}
		})
	}
}

func TestAnalysis_GetBotRatio_IsArchived_IsDisabled(t *testing.T) {
	tests := []struct {
		name        string
		a           *Analysis
		wantBot     float64
		wantArch    bool
		wantDisable bool
	}{
		{name: "all_zero_nil", a: &Analysis{}, wantBot: 0, wantArch: false, wantDisable: false},
		{name: "with_state", a: &Analysis{RepoState: &RepoState{CommitStats: &CommitStats{BotRatio: 0.25}, IsArchived: true, IsDisabled: false}}, wantBot: 0.25, wantArch: true, wantDisable: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.GetBotRatio(); got != tt.wantBot {
				t.Errorf("GetBotRatio() = %v, want %v", got, tt.wantBot)
			}
			if got := tt.a.IsArchived(); got != tt.wantArch {
				t.Errorf("IsArchived() = %v, want %v", got, tt.wantArch)
			}
			if got := tt.a.IsDisabled(); got != tt.wantDisable {
				t.Errorf("IsDisabled() = %v, want %v", got, tt.wantDisable)
			}
		})
	}
}

func TestAnalysis_ErrorHelpers(t *testing.T) {
	testErr := errors.New("boom")
	tests := []struct {
		name    string
		a       *Analysis
		wantHas bool
		wantMsg string
	}{
		{name: "no_error", a: &Analysis{}, wantHas: false, wantMsg: ""},
		{name: "with_error", a: &Analysis{Error: testErr}, wantHas: true, wantMsg: "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.HasError(); got != tt.wantHas {
				t.Errorf("HasError() = %v, want %v", got, tt.wantHas)
			}
			if got := tt.a.GetErrorMessage(); got != tt.wantMsg {
				t.Errorf("GetErrorMessage() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestAnalysis_GetDaysSinceLatestPublish(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		a    *Analysis
		want int
	}{
		{name: "nil_release_info", a: &Analysis{}, want: 9999},
		{name: "empty_release_info", a: &Analysis{ReleaseInfo: &ReleaseInfo{}}, want: 9999},
		{name: "stable_only", a: &Analysis{
			ReleaseInfo: &ReleaseInfo{
				StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: now.AddDate(0, 0, -100)},
			},
		}, want: 100},
		{name: "prerelease_newer_than_stable", a: &Analysis{
			ReleaseInfo: &ReleaseInfo{
				StableVersion:     &VersionDetail{Version: "1.0.0", PublishedAt: now.AddDate(0, 0, -200)},
				PreReleaseVersion: &VersionDetail{Version: "2.0.0-rc1", PublishedAt: now.AddDate(0, 0, -50)},
			},
		}, want: 50},
		{name: "zero_publish_time_ignored", a: &Analysis{
			ReleaseInfo: &ReleaseInfo{
				StableVersion:    &VersionDetail{Version: "1.0.0"},
				MaxSemverVersion: &VersionDetail{Version: "1.0.0", PublishedAt: now.AddDate(0, 0, -300)},
			},
		}, want: 300},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.GetDaysSinceLatestPublish()
			// Allow ±1 day tolerance for time boundary
			if got < tt.want-1 || got > tt.want+1 {
				t.Errorf("GetDaysSinceLatestPublish() = %d, want ~%d", got, tt.want)
			}
		})
	}
}

func TestAnalysis_IsVCSDirectDelivery(t *testing.T) {
	tests := []struct {
		name string
		a    *Analysis
		want bool
	}{
		{name: "golang", a: &Analysis{Package: &Package{Ecosystem: "golang"}}, want: true},
		{name: "composer", a: &Analysis{Package: &Package{Ecosystem: "composer"}}, want: true},
		{name: "npm", a: &Analysis{Package: &Package{Ecosystem: "npm"}}, want: false},
		{name: "pypi", a: &Analysis{Package: &Package{Ecosystem: "pypi"}}, want: false},
		{name: "maven", a: &Analysis{Package: &Package{Ecosystem: "maven"}}, want: false},
		{name: "nuget", a: &Analysis{Package: &Package{Ecosystem: "nuget"}}, want: false},
		{name: "cargo", a: &Analysis{Package: &Package{Ecosystem: "cargo"}}, want: false},
		{name: "gem", a: &Analysis{Package: &Package{Ecosystem: "gem"}}, want: false},
		{name: "nil_package", a: &Analysis{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.IsVCSDirectDelivery(); got != tt.want {
				t.Errorf("IsVCSDirectDelivery() = %v, want %v", got, tt.want)
			}
		})
	}
}
