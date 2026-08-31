package analysis

import (
	"context"
	"strings"
	"testing"
	"time"

	cfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// withdrawn builds an Analysis whose registry reports every release as yanked.
func withdrawn(registry, reason string) *RegistryState {
	return &RegistryState{AllReleasesYanked: true, Registry: registry, Reason: reason,
		Reference: "https://example.test/pkg"}
}

// TestLifecycleAssessor_AllReleasesYanked is the decision table for the
// package-level distribution-withdrawal branch (ADR-0022).
func TestLifecycleAssessor_AllReleasesYanked(t *testing.T) {
	t.Parallel()
	now := time.Now()
	recent := now.AddDate(0, 0, -10)
	inactivity := cfg.GetDefaultLifecycle().EolInactivityDays
	dormant := now.AddDate(0, 0, -(inactivity + 10))

	activeRepo := func() *RepoState {
		return &RepoState{DaysSinceLastCommit: 5, LatestHumanCommit: &recent, CommitStats: &CommitStats{}}
	}

	tests := []struct {
		name       string
		analysis   *Analysis
		scores     map[string]*ScoreEntity
		eol        EOLStatus
		wantLabel  MaintenanceStatus
		wantReason string
		wantTrace  string
		wantSignal bool // SignalAllReleasesYanked present
	}{
		{
			name: "no registry state means the branch never fires",
			analysis: &Analysis{
				RepoState:   activeRepo(),
				ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: recent}},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 8, 10, "ok"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 9, 10, "ok"),
			},
			eol:       EOLStatus{State: EOLNotEOL},
			wantLabel: LabelActive,
		},
		{
			name: "registry asked, nothing yanked, branch never fires",
			analysis: &Analysis{
				RepoState:     activeRepo(),
				RegistryState: &RegistryState{Registry: "crates.io"},
				ReleaseInfo:   &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: recent}},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 8, 10, "ok"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 9, 10, "ok"),
			},
			eol:       EOLStatus{State: EOLNotEOL},
			wantLabel: LabelActive,
		},
		{
			name: "every release yanked with a reason",
			analysis: &Analysis{
				RepoState:     activeRepo(),
				RegistryState: withdrawn("PyPI", "Unmaintained"),
			},
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelReviewNeeded,
			wantReason: "All releases yanked on PyPI: Unmaintained",
			wantTrace:  "all_releases_yanked_review_needed",
			wantSignal: true,
		},
		{
			name: "every release yanked without a reason",
			analysis: &Analysis{
				RepoState:     activeRepo(),
				RegistryState: withdrawn("crates.io", ""),
			},
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelReviewNeeded,
			wantReason: "All releases yanked on crates.io",
			wantTrace:  "all_releases_yanked_review_needed",
			wantSignal: true,
		},
		{
			name: "an explicit primary-source EOL outranks the withdrawal fact",
			analysis: &Analysis{
				RepoState:     activeRepo(),
				RegistryState: withdrawn("PyPI", "Unmaintained"),
			},
			eol: EOLStatus{State: EOLEndOfLife, Evidences: []EOLEvidence{
				{Source: "PyPI", Summary: "Classifier: Development Status :: 7 - Inactive", Confidence: 1.0},
			}},
			wantLabel: LabelEOLConfirmed,
		},
		{
			name: "archived repository still yields Review Needed, not Stalled",
			analysis: &Analysis{
				RepoState:     &RepoState{DaysSinceLastCommit: 5, LatestHumanCommit: &recent, CommitStats: &CommitStats{}, IsArchived: true},
				RegistryState: withdrawn("PyPI", "Unmaintained"),
			},
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelReviewNeeded,
			wantTrace:  "all_releases_yanked_review_needed",
			wantSignal: true,
		},
		{
			name: "disabled repository still yields Review Needed",
			analysis: &Analysis{
				RepoState:     &RepoState{DaysSinceLastCommit: 5, LatestHumanCommit: &recent, CommitStats: &CommitStats{}, IsDisabled: true},
				RegistryState: withdrawn("crates.io", ""),
			},
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelReviewNeeded,
			wantSignal: true,
		},
		{
			name: "an actively developed project whose distribution is withdrawn (conda)",
			analysis: &Analysis{
				RepoState:     activeRepo(),
				RegistryState: withdrawn("PyPI", "Pip installing conda leads to broken UX"),
				ReleaseInfo:   &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: recent}},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 8, 10, "ok"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 9, 10, "ok"),
			},
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelReviewNeeded,
			wantSignal: true,
		},
		{
			// Deliberate: the branch also suppresses the residual-vulnerability
			// override, exactly as the archived branch already does. ADR-0022.
			name: "dormant with unpatched advisories is Review Needed, not EOL-Effective",
			analysis: &Analysis{
				RepoState:     &RepoState{DaysSinceLastCommit: inactivity + 10, LatestHumanCommit: &dormant, CommitStats: &CommitStats{}},
				RegistryState: withdrawn("PyPI", "Unmaintained"),
				ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.2.3", PublishedAt: now.AddDate(-2, 0, 0),
					Advisories: []Advisory{{ID: "GHSA-XXX", Source: "GHSA", URL: "https://github.com/advisories/GHSA-XXX"}}}},
			},
			scores:     map[string]*ScoreEntity{},
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelReviewNeeded,
			wantSignal: true,
		},
	}

	svc := NewLifecycleAssessorService()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := svc.Assess(context.Background(), AssessmentInput{Analysis: tt.analysis, Scores: tt.scores, EOL: tt.eol})
			if err != nil {
				t.Fatalf("Assess failed: %v", err)
			}
			if res == nil {
				t.Fatalf("expected a result")
			}
			if MaintenanceStatus(res.Label) != tt.wantLabel {
				t.Fatalf("Label: got %q, want %q (reason %q)", res.Label, tt.wantLabel, res.Reason)
			}
			if tt.wantReason != "" && res.Reason != tt.wantReason {
				t.Errorf("Reason: got %q, want %q", res.Reason, tt.wantReason)
			}
			if tt.wantTrace != "" && !containsString(res.Trace, tt.wantTrace) {
				t.Errorf("Trace: got %v, want it to contain %q", res.Trace, tt.wantTrace)
			}
			if got := hasSignal(res.Signals, SignalAllReleasesYanked); got != tt.wantSignal {
				t.Errorf("SignalAllReleasesYanked present: got %v, want %v (signals %+v)", got, tt.wantSignal, res.Signals)
			}
		})
	}
}

// TestLifecycleAssessor_AllReleasesYankedKeepsArchiveSignal pins that the
// archive evidence is not lost when the withdrawal branch wins.
func TestLifecycleAssessor_AllReleasesYankedKeepsArchiveSignal(t *testing.T) {
	t.Parallel()
	recent := time.Now().AddDate(0, 0, -10)
	a := &Analysis{
		RepoState:     &RepoState{DaysSinceLastCommit: 5, LatestHumanCommit: &recent, CommitStats: &CommitStats{}, IsArchived: true},
		RegistryState: withdrawn("PyPI", "Unmaintained"),
	}
	res, err := NewLifecycleAssessorService().Assess(context.Background(), AssessmentInput{Analysis: a, EOL: EOLStatus{State: EOLNotEOL}})
	if err != nil {
		t.Fatalf("Assess failed: %v", err)
	}
	if !hasSignal(res.Signals, SignalRepoArchived) {
		t.Errorf("expected the archived signal to be retained, got %+v", res.Signals)
	}
}

func hasSignal(signals []Signal, name string) bool {
	for _, s := range signals {
		if s.Name == name {
			return true
		}
	}
	return false
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
