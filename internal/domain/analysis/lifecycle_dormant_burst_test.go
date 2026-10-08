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

// TestLifecycleAssessor_DormantBurst is the decision table for branch 1.2
// (ADR-0026): which version is analysed × what else is true of the package.
func TestLifecycleAssessor_DormantBurst(t *testing.T) {
	t.Parallel()
	now := time.Now()
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
		wantTrace  string
		wantReason string
	}{
		{
			name:       "burst version on an active project is Review Needed, not Active",
			analysis:   &Analysis{Package: pkg("1.0.1"), RepoState: activeRepo, ReleaseInfo: stable, ReleaseHistory: burstHistory(now)},
			wantLabel:  LabelReviewNeeded,
			wantTrace:  "dormant_release_burst_review_needed",
			wantReason: "Released after 800 days without a release, in one burst across release lines 1, 2; this version has since been removed from the registry",
		},
		{
			name:       "the other version of the same burst, still installable",
			analysis:   &Analysis{Package: pkg("2.0.1"), RepoState: activeRepo, ReleaseInfo: stable, ReleaseHistory: burstHistory(now)},
			wantLabel:  LabelReviewNeeded,
			wantTrace:  "dormant_release_burst_review_needed",
			wantReason: "Released after 800 days without a release, in one burst across release lines 1, 2",
		},
		{
			name:      "the release before the silence is untouched",
			analysis:  &Analysis{Package: pkg("1.0.0"), RepoState: activeRepo, ReleaseInfo: stable, ReleaseHistory: burstHistory(now)},
			wantLabel: LabelActive,
		},
		{
			name:      "no history means the branch never fires",
			analysis:  &Analysis{Package: pkg("1.0.1"), RepoState: activeRepo, ReleaseInfo: stable},
			wantLabel: LabelActive,
		},
		{
			name: "takes precedence over an archived repository",
			analysis: &Analysis{Package: pkg("1.0.1"), ReleaseInfo: stable, ReleaseHistory: burstHistory(now),
				RepoState: &RepoState{IsArchived: true, DaysSinceLastCommit: 5, LatestHumanCommit: &recent, CommitStats: &CommitStats{}}},
			wantLabel: LabelReviewNeeded,
			wantTrace: "dormant_release_burst_review_needed",
		},
		{
			name:      "a primary-source EOL still wins",
			analysis:  &Analysis{Package: pkg("1.0.1"), RepoState: activeRepo, ReleaseInfo: stable, ReleaseHistory: burstHistory(now)},
			eol:       EOLStatus{State: EOLEndOfLife, Evidences: []EOLEvidence{{Source: "npmjs", Summary: "Deprecated in npm registry"}}},
			wantLabel: LabelEOLConfirmed,
		},
	}
	svc := NewLifecycleAssessorService()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := svc.Assess(context.Background(), AssessmentInput{Analysis: tt.analysis, Scores: healthy, EOL: tt.eol})
			if err != nil {
				t.Fatalf("Assess: %v", err)
			}
			if MaintenanceStatus(res.Label) != tt.wantLabel {
				t.Fatalf("label = %q (%s), want %q", res.Label, res.Reason, tt.wantLabel)
			}
			if tt.wantTrace != "" && !strings.Contains(strings.Join(res.Trace, " "), tt.wantTrace) {
				t.Errorf("trace %v lacks %q", res.Trace, tt.wantTrace)
			}
			if tt.wantReason != "" && res.Reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", res.Reason, tt.wantReason)
			}
			if tt.wantTrace == "dormant_release_burst_review_needed" {
				if !hasSignal(res.Signals, SignalDormantReleaseBurst) || !hasSignal(res.Signals, SignalDaysSilentBeforeRelease) {
					t.Errorf("signals %v lack the burst signals", res.Signals)
				}
			}
		})
	}
}

