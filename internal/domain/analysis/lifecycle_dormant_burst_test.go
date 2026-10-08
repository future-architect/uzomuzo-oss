package analysis

import (
	"context"
	"strings"
	"testing"
	"time"
)

// burstHistory returns a history where "1.0.1" and "2.0.1" were published
// minutes apart, 800 days after "1.0.0", and 10 days before now. "1.0.1" has
// since been removed.
func burstHistory(now time.Time) *ReleaseHistory {
	burst := now.AddDate(0, 0, -10)
	return &ReleaseHistory{
		Registry: RegistryNpm,
		PublishedAt: map[string]time.Time{
			"1.0.0": burst.AddDate(0, 0, -800),
			"1.0.1": burst,
			"2.0.1": burst.Add(time.Minute),
		},
		Installable: map[string]struct{}{"1.0.0": {}, "2.0.1": {}},
	}
}

// TestLifecycleAssessor_DormantBurst is the end-to-end decision table for the
// dormant-burst rule (ADR-0026): which version is analysed × what else is true
// of the package.
func TestLifecycleAssessor_DormantBurst(t *testing.T) {
	t.Parallel()
	// A fixed date years in the past: every age the assessor checks reads
	// AssessmentInput.Now, so this table must not depend on the wall clock. UTC,
	// so AddDate never crosses a DST change.
	now := time.Date(2021, 11, 5, 0, 0, 0, 0, time.UTC)
	recent := now.AddDate(0, 0, -10)
	activeRepo := &RepoState{DaysSinceLastCommit: 5, LatestHumanCommit: &recent, CommitStats: &CommitStats{}}
	healthy := map[string]*ScoreEntity{
		"Maintained":      NewScoreEntity("Maintained", 10, 10, "ok"),
		"Vulnerabilities": NewScoreEntity("Vulnerabilities", 10, 10, "ok"),
	}
	pkg := func(v string) *Package {
		return &Package{PURL: "pkg:npm/example@" + v, Ecosystem: "npm", Version: v}
	}
	stable := &ReleaseInfo{StableVersion: &VersionDetail{Version: "2.0.1", PublishedAt: recent}}

	tests := []struct {
		name       string
		analysis   *Analysis
		eol        EOLStatus
		wantLabel  MaintenanceStatus
		wantBurst  bool
		wantReason string
	}{
		{
			name:       "burst version on an active project is Review Needed, not Active",
			analysis:   &Analysis{Package: pkg("1.0.1"), RepoState: activeRepo, ReleaseInfo: stable, ReleaseHistory: burstHistory(now)},
			wantLabel:  LabelReviewNeeded,
			wantBurst:  true,
			wantReason: "Released after 800 days without a release, in one burst across release lines 1, 2; this version has since been removed from the registry",
		},
		{
			name:       "the other version of the same burst, still installable",
			analysis:   &Analysis{Package: pkg("2.0.1"), RepoState: activeRepo, ReleaseInfo: stable, ReleaseHistory: burstHistory(now)},
			wantLabel:  LabelReviewNeeded,
			wantBurst:  true,
			wantReason: "Released after 800 days without a release, in one burst across release lines 1, 2",
		},
		{
			name:       "the release before the silence is untouched",
			analysis:   &Analysis{Package: pkg("1.0.0"), RepoState: activeRepo, ReleaseInfo: stable, ReleaseHistory: burstHistory(now)},
			wantLabel:  LabelActive,
			wantReason: "Actively maintained with recent releases",
		},
		{
			name:       "no history means the branch never fires",
			analysis:   &Analysis{Package: pkg("1.0.1"), RepoState: activeRepo, ReleaseInfo: stable},
			wantLabel:  LabelActive,
			wantReason: "Actively maintained with recent releases",
		},
		{
			name: "an archived repository stays Stalled, so a --fail-on stalled gate keeps firing",
			analysis: &Analysis{Package: pkg("1.0.1"), ReleaseInfo: stable, ReleaseHistory: burstHistory(now),
				RepoState: &RepoState{IsArchived: true, DaysSinceLastCommit: 5, LatestHumanCommit: &recent, CommitStats: &CommitStats{}}},
			wantLabel:  LabelStalled,
			wantReason: "Repository archived/disabled but not declared end-of-life",
		},
		{
			name:       "a primary-source EOL still wins",
			analysis:   &Analysis{Package: pkg("1.0.1"), RepoState: activeRepo, ReleaseInfo: stable, ReleaseHistory: burstHistory(now)},
			eol:        EOLStatus{State: EOLEndOfLife, Evidences: []EOLEvidence{{Source: "npmjs", Summary: "Deprecated in npm registry"}}},
			wantLabel:  LabelEOLConfirmed,
			wantReason: "Deprecated in npm registry",
		},
	}
	svc := NewLifecycleAssessorService()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := svc.Assess(context.Background(), AssessmentInput{Analysis: tt.analysis, Scores: healthy, EOL: tt.eol, Now: now})
			if err != nil {
				t.Fatalf("Assess: %v", err)
			}
			if MaintenanceStatus(res.Label) != tt.wantLabel {
				t.Fatalf("label = %q (%s), want %q", res.Label, res.Reason, tt.wantLabel)
			}
			if res.Reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", res.Reason, tt.wantReason)
			}
			gotBurstTrace := strings.Contains(strings.Join(res.Trace, " "), "dormant_release_burst_review_needed")
			if gotBurstTrace != tt.wantBurst {
				t.Errorf("burst trace present = %v, want %v (trace %v)", gotBurstTrace, tt.wantBurst, res.Trace)
			}
			gotSignals := hasSignal(res.Signals, SignalDormantReleaseBurst) && hasSignal(res.Signals, SignalDaysSilentBeforeRelease)
			if gotSignals != tt.wantBurst {
				t.Errorf("burst signals present = %v, want %v (%v)", gotSignals, tt.wantBurst, res.Signals)
			}
			if tt.wantBurst && !hasSignal(res.Signals, SignalRecentStableRelease) {
				t.Errorf("the replaced Active label's signals were dropped: %v", res.Signals)
			}
		})
	}
}

// TestApplyDormantBurst pins that the burst only replaces an ok outcome: every
// other label passes through unchanged, so a --fail-on gate on it still fires.
func TestApplyDormantBurst(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	a := &Analysis{Package: &Package{PURL: "pkg:npm/example@1.0.1", Ecosystem: "npm", Version: "1.0.1"}, ReleaseHistory: burstHistory(now)}
	svc := NewLifecycleAssessorService()
	for _, tc := range []struct {
		in   MaintenanceStatus
		want MaintenanceStatus
	}{
		{LabelActive, LabelReviewNeeded},
		{LabelLegacySafe, LabelReviewNeeded},
		{LabelStalled, LabelStalled},
		{LabelEOLEffective, LabelEOLEffective},
		{LabelEOLConfirmed, LabelEOLConfirmed},
		{LabelEOLScheduled, LabelEOLScheduled},
		{LabelReviewNeeded, LabelReviewNeeded},
	} {
		orig := &AssessmentResult{Axis: LifecycleAxis, Label: string(tc.in), Reason: "orig", Trace: []string{"x"},
			Signals: []Signal{sig(SignalRecentStableRelease, "true")}}
		got := svc.applyDormantBurst(AssessmentInput{Analysis: a, Now: now}, orig)
		if MaintenanceStatus(got.Label) != tc.want {
			t.Errorf("%s -> %s, want %s", tc.in, got.Label, tc.want)
		}
		if tc.in == tc.want && got != orig {
			t.Errorf("%s: result must be returned unchanged", tc.in)
		}
		if tc.in == LabelActive && !strings.Contains(strings.Join(got.Trace, " "), "was Active") {
			t.Errorf("trace %v does not record the replaced label", got.Trace)
		}
		if tc.want == LabelReviewNeeded && tc.in != tc.want {
			if len(got.Signals) == 0 || got.Signals[0].Name != SignalDormantReleaseBurst || !hasSignal(got.Signals, SignalRecentStableRelease) {
				t.Errorf("%s: signals = %v, want the burst first and the original signals kept", tc.in, got.Signals)
			}
		}
	}
	// The same burst a year and a day later is past the age limit.
	late := svc.applyDormantBurst(AssessmentInput{Analysis: a, Now: now.AddDate(1, 0, 1)}, &AssessmentResult{Label: string(LabelActive)})
	if MaintenanceStatus(late.Label) != LabelActive {
		t.Errorf("past the age limit: %s, want Active", late.Label)
	}
}
