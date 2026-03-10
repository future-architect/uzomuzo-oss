package analysis

import (
	"context"
	"testing"
	"time"

	cfg "github.com/future-architect/uzomuzo/internal/domain/config"
)

// ptrTime returns a pointer to the provided time.Time (test helper)
func ptrTime(t time.Time) *time.Time { return &t }

// Constructor test simplified (legacy type field removed)
func TestLifecycleAssessorService_Constructor(t *testing.T) {
	service := NewLifecycleAssessorService()
	if service == nil {
		t.Fatalf("expected service instance")
	}
}
func TestLifecycleAssessorService_Assess(t *testing.T) {
	now := time.Now()
	recentTime := now.AddDate(0, 0, -10)
	oldTime := now.AddDate(0, 0, -400)

	tests := []struct {
		name     string
		analysis *Analysis
		scores   map[string]*ScoreEntity
		want     LifecycleLabel
		wantErr  bool
	}{
		{
			name: "active_healthy_project",
			analysis: &Analysis{
				Scores: map[string]*ScoreEntity{
					"Maintained":      NewScoreEntity("Maintained", 8, 10, "Well maintained"),
					"Vulnerabilities": NewScoreEntity("Vulnerabilities", 9, 10, "Few vulnerabilities"),
				},
				RepoState: &RepoState{
					DaysSinceLastCommit: 5,
					LatestHumanCommit:   &recentTime,
					IsArchived:          false,
					IsDisabled:          false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: recentTime},
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 8, 10, "Well maintained"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 9, 10, "Few vulnerabilities"),
			},
			want:    LabelActive,
			wantErr: false,
		},
		{
			name: "override_to_eol_effective_when_scorecard_missing_and_open_advisory_and_old_commits",
			analysis: func() *Analysis {
				jc := cfg.GetDefaultLifecycle()
				inactivity := jc.EolInactivityDays
				return &Analysis{
					RepoState: &RepoState{
						DaysSinceLastCommit: inactivity + 10,
						LatestHumanCommit:   ptrTime(time.Now().AddDate(0, 0, -(inactivity + 10))),
						IsArchived:          false,
						IsDisabled:          false,
					},
					ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.2.3", PublishedAt: time.Now().AddDate(-2, 0, 0), Advisories: []Advisory{{ID: "GHSA-XXX", Source: "GHSA", URL: "https://github.com/advisories/GHSA-XXX"}}}},
				}
			}(),
			scores:  map[string]*ScoreEntity{},
			want:    LabelEOLEffective,
			wantErr: false,
		},
		{
			name: "override_not_triggered_when_no_residual_advisory",
			analysis: func() *Analysis {
				jc := cfg.GetDefaultLifecycle()
				inactivity := jc.EolInactivityDays
				return &Analysis{
					RepoState: &RepoState{
						DaysSinceLastCommit: inactivity + 10,
						LatestHumanCommit:   ptrTime(time.Now().AddDate(0, 0, -(inactivity + 10))),
						IsArchived:          false,
						IsDisabled:          false,
					},
					ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.2.3", PublishedAt: time.Now().AddDate(-2, 0, 0)}},
				}
			}(),
			scores:  map[string]*ScoreEntity{},
			want:    LabelReviewNeeded,
			wantErr: false,
		},
		{
			name: "override_uses_maxsemver_when_no_stable",
			analysis: func() *Analysis {
				jc := cfg.GetDefaultLifecycle()
				inactivity := jc.EolInactivityDays
				return &Analysis{
					RepoState: &RepoState{
						DaysSinceLastCommit: inactivity + 20,
						LatestHumanCommit:   ptrTime(time.Now().AddDate(0, 0, -(inactivity + 20))),
						IsArchived:          false,
						IsDisabled:          false,
					},
					ReleaseInfo: &ReleaseInfo{MaxSemverVersion: &VersionDetail{Version: "2.0.0", PublishedAt: time.Now().AddDate(-2, 0, 0), Advisories: []Advisory{{ID: "GHSA-YYY", Source: "GHSA", URL: "https://github.com/advisories/GHSA-YYY"}}}},
				}
			}(),
			scores:  map[string]*ScoreEntity{},
			want:    LabelEOLEffective,
			wantErr: false,
		},
		{
			name: "archived_project",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 200,
					LatestHumanCommit:   &oldTime,
					IsArchived:          true,
					IsDisabled:          false,
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 6, 10, "Maintained"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 8, 10, "Few vulnerabilities"),
			},
			want:    LabelEOLConfirmed,
			wantErr: false,
		},
		{
			name: "stalled_project",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 800,
					LatestHumanCommit:   &oldTime,
					IsArchived:          false,
					IsDisabled:          false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{
						Version:     "0.9.0",
						PublishedAt: oldTime,
					},
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 2, 10, "Poorly maintained"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 5, 10, "Some vulnerabilities"),
			},
			want:    LabelStalled,
			wantErr: false,
		},
		{
			name: "no_scores_data",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 10,
					LatestHumanCommit:   &recentTime,
					IsArchived:          false,
					IsDisabled:          false,
				},
			},
			scores:  map[string]*ScoreEntity{},
			want:    LabelStalled, // proceed to activity evaluation without Scorecard; recent commits but no maintenance score => Stalled
			wantErr: false,
		},
		{
			name: "disabled_project",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 30,
					LatestHumanCommit:   &recentTime,
					IsArchived:          false,
					IsDisabled:          true,
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained": NewScoreEntity("Maintained", 6, 10, "Maintained"),
			},
			want:    LabelEOLConfirmed,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewLifecycleAssessorService()
			ctx := context.Background()

			in := AssessmentInput{Analysis: tt.analysis, Scores: tt.scores, EOL: EOLStatus{State: EOLNotEOL}}
			res, err := service.Assess(ctx, in)

			if (err != nil) != tt.wantErr {
				t.Errorf("LifecycleAssessorService.Assess() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if res == nil {
				t.Errorf("LifecycleAssessorService.Assess() returned nil result")
				return
			}

			if res.Label != tt.want {
				t.Errorf("LifecycleAssessorService.Assess() label = %v, want %v", res.Label, tt.want)
			}

			// Verify that reason is not empty
			if res.Reason == "" {
				t.Errorf("LifecycleAssessorService.Assess() returned empty reason")
			}
		})
	}
}

func TestLifecycleAssessorService_Assess_Complex_Cases(t *testing.T) {
	now := time.Now()
	recentTime := now.AddDate(0, 0, -30)  // 30 days ago
	mediumTime := now.AddDate(0, 0, -120) // 120 days ago
	oldTime := now.AddDate(0, 0, -400)    // 400 days ago

	tests := []struct {
		name        string
		analysis    *Analysis
		scores      map[string]*ScoreEntity
		wantLabel   LifecycleLabel
		description string
	}{
		{
			name: "active_but_poor_maintenance",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 5, // Recent commits
					LatestHumanCommit:   &recentTime,
					IsArchived:          false,
					IsDisabled:          false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{
						Version:     "1.0.0",
						PublishedAt: recentTime,
					},
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 2, 10, "Poorly maintained"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 8, 10, "Few vulnerabilities"),
			},
			wantLabel:   LabelStalled,
			description: "Should be stalled due to poor maintenance despite recent activity",
		},
		{
			name: "good_scores_but_old_activity",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 800, // Old commits
					LatestHumanCommit:   &oldTime,
					IsArchived:          false,
					IsDisabled:          false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{
						Version:     "1.0.0",
						PublishedAt: oldTime,
					},
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 8, 10, "Well maintained"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 9, 10, "Few vulnerabilities"),
			},
			wantLabel:   LabelStalled,
			description: "Should be stalled due to old activity despite good scores",
		},
		{
			name: "mixed_signals_recent_prerelease",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 100,
					LatestHumanCommit:   &mediumTime,
					IsArchived:          false,
					IsDisabled:          false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{
						Version:     "1.0.0",
						PublishedAt: oldTime,
					},
					PreReleaseVersion: &VersionDetail{
						Version:      "2.0.0-beta.1",
						PublishedAt:  recentTime,
						IsPrerelease: true,
					},
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 5, 10, "Medium maintenance"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 7, 10, "Some vulnerabilities"),
			},
			wantLabel:   LabelStalled,
			description: "Should consider both stable and prerelease activity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewLifecycleAssessorService()
			ctx := context.Background()

			in := AssessmentInput{Analysis: tt.analysis, Scores: tt.scores, EOL: EOLStatus{State: EOLNotEOL}}
			gotRes, err := service.Assess(ctx, in)

			if err != nil {
				t.Errorf("LifecycleAssessorService.Assess() error = %v", err)
				return
			}

			if gotRes == nil {
				t.Errorf("LifecycleAssessorService.Assess() returned nil result")
				return
			}

			if gotRes.Label != tt.wantLabel {
				t.Errorf("LifecycleAssessorService.Assess() label = %v, want %v. %s",
					gotRes.Label, tt.wantLabel, tt.description)
			}
		})
	}
}

// (constructor typed variant tests removed)

func TestLifecycleAssessorService_EdgeCases(t *testing.T) {
	now := time.Now()
	recentTime := now.AddDate(0, 0, -10)

	tests := []struct {
		name     string
		analysis *Analysis
		scores   map[string]*ScoreEntity
		want     LifecycleLabel
	}{
		{
			name:     "nil_analysis",
			analysis: nil,
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 8, 10, "Well maintained"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 9, 10, "Few vulnerabilities"),
			},
			want: LabelReviewNeeded,
		},
		{
			name: "missing_maintained_score",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 5,
					LatestHumanCommit:   &recentTime,
					IsArchived:          false,
					IsDisabled:          false,
				},
			},
			scores: map[string]*ScoreEntity{
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 9, 10, "Few vulnerabilities"),
			},
			want: LabelStalled, // proceed to activity evaluation with partial scores; recent commits, maintenance unknown -> Stalled
		},
		{
			name: "missing_vulnerabilities_score",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 5,
					LatestHumanCommit:   &recentTime,
					IsArchived:          false,
					IsDisabled:          false,
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained": NewScoreEntity("Maintained", 8, 10, "Well maintained"),
			},
			want: LabelStalled, // proceed to activity evaluation with partial scores; recent commits, vuln unknown -> Stalled
		},
		{
			name: "nil_repo_state",
			analysis: &Analysis{
				RepoState: nil,
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 8, 10, "Well maintained"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 9, 10, "Few vulnerabilities"),
			},
			want: LabelLegacySafe, // When analysis.GetDaysSinceLastCommit() returns 9999 for nil RepoState, this triggers LegacySafe logic
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewLifecycleAssessorService()
			ctx := context.Background()

			in := AssessmentInput{Analysis: tt.analysis, Scores: tt.scores, EOL: EOLStatus{State: EOLNotEOL}}
			gotRes, err := service.Assess(ctx, in)

			if err != nil {
				t.Errorf("LifecycleAssessorService.Assess() error = %v", err)
				return
			}

			if gotRes == nil {
				t.Errorf("LifecycleAssessorService.Assess() returned nil result")
				return
			}

			if gotRes.Label != tt.want {
				t.Errorf("LifecycleAssessorService.Assess() label = %v, want %v", gotRes.Label, tt.want)
			}
		})
	}
}
