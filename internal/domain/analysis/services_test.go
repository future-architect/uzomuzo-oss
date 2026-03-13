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
					CommitStats:         &CommitStats{},
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
						CommitStats:         &CommitStats{},
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
						CommitStats:         &CommitStats{},
						IsArchived:          false,
						IsDisabled:          false,
					},
					ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.2.3", PublishedAt: time.Now().AddDate(-2, 0, 0)}},
				}
			}(),
			scores:  map[string]*ScoreEntity{},
			want:    LabelLegacySafe, // zero advisories + dormant > EolInactivityDays → Legacy-Safe
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
						CommitStats:         &CommitStats{},
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
					CommitStats:         &CommitStats{},
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
					CommitStats:         &CommitStats{},
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
					CommitStats:         &CommitStats{},
					IsArchived:          false,
					IsDisabled:          false,
				},
			},
			scores:  map[string]*ScoreEntity{},
			want:    LabelActive, // recent commits + Scorecard unavailable → Active (maintenance unknown, not proven low)
			wantErr: false,
		},
		{
			name: "disabled_project",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 30,
					LatestHumanCommit:   &recentTime,
					CommitStats:         &CommitStats{},
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
					CommitStats:         &CommitStats{},
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
			wantLabel:   LabelActive,
			description: "Recent stable publish is the strongest activity signal regardless of maintenance score",
		},
		{
			name: "good_scores_but_old_activity",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 800, // Old commits
					LatestHumanCommit:   &oldTime,
					CommitStats:         &CommitStats{},
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
					CommitStats:         &CommitStats{},
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
			wantLabel:   LabelActive,
			description: "Recent prerelease publish is a strong activity signal",
		},
		{
			name: "golang_commits_only_no_publish_active",
			analysis: &Analysis{
				Package: &Package{Ecosystem: "golang"},
				RepoState: &RepoState{
					DaysSinceLastCommit: 30,
					LatestHumanCommit:   &recentTime,
					CommitStats:         &CommitStats{},
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: oldTime}, // old publish
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 1, 10, "Poorly maintained"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 5, 10, "Some vulnerabilities"),
			},
			wantLabel:   LabelActive,
			description: "Go modules: commits deliver updates via go get; low maintenance score does not downgrade to Stalled",
		},
		{
			name: "npm_commits_only_no_publish_low_maintenance_stalled",
			analysis: &Analysis{
				Package: &Package{Ecosystem: "npm"},
				RepoState: &RepoState{
					DaysSinceLastCommit: 30,
					LatestHumanCommit:   &recentTime,
					CommitStats:         &CommitStats{},
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: oldTime},
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 1, 10, "Poorly maintained"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 5, 10, "Some vulnerabilities"),
			},
			wantLabel:   LabelStalled,
			description: "npm: commits without publish do not reach consumers; low maintenance → Stalled",
		},
		{
			name: "composer_commits_only_no_publish_active",
			analysis: &Analysis{
				Package: &Package{Ecosystem: "composer"},
				RepoState: &RepoState{
					DaysSinceLastCommit: 30,
					LatestHumanCommit:   &recentTime,
					CommitStats:         &CommitStats{},
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: oldTime},
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 1, 10, "Poorly maintained"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 5, 10, "Some vulnerabilities"),
			},
			wantLabel:   LabelActive,
			description: "Composer/Packagist: VCS-direct delivery; commits are sufficient for Active",
		},
		{
			name: "npm_commits_only_no_scorecard_active",
			analysis: &Analysis{
				Package: &Package{Ecosystem: "npm"},
				RepoState: &RepoState{
					DaysSinceLastCommit: 30,
					LatestHumanCommit:   &recentTime,
					CommitStats:         &CommitStats{},
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: oldTime},
				},
			},
			scores:      map[string]*ScoreEntity{}, // no Scorecard at all
			wantLabel:   LabelActive,
			description: "npm: commits + no Scorecard → maintenance unknown → Active (not penalized for missing metrics)",
		},
		{
			name: "npm_commits_only_maintained_explicitly_low_stalled",
			analysis: &Analysis{
				Package: &Package{Ecosystem: "npm"},
				RepoState: &RepoState{
					DaysSinceLastCommit: 30,
					LatestHumanCommit:   &recentTime,
					CommitStats:         &CommitStats{},
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: oldTime},
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 0, 10, "Not maintained"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 5, 10, "Some vulnerabilities"),
			},
			wantLabel:   LabelStalled,
			description: "npm: commits + Scorecard confirms Maintained=0 → Stalled (proven low, not unknown)",
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
	oldTime := now.AddDate(0, 0, -400)

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
					CommitStats:         &CommitStats{},
					IsArchived:          false,
					IsDisabled:          false,
				},
			},
			scores: map[string]*ScoreEntity{
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 9, 10, "Few vulnerabilities"),
			},
			want: LabelActive, // recent commits + Maintained score absent → Active (maintenance unknown, not proven low)
		},
		{
			name: "missing_vulnerabilities_score",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 5,
					LatestHumanCommit:   &recentTime,
					CommitStats:         &CommitStats{},
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
			name: "commit_data_no_scorecard_recent_publish_stalled",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 250,
					LatestHumanCommit:   ptrTime(now.AddDate(0, 0, -250)),
					CommitStats:         &CommitStats{},
					IsArchived:          false,
					IsDisabled:          false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: now.AddDate(0, 0, -400)}, // 400 days ≤ 730
				},
			},
			scores: map[string]*ScoreEntity{},
			want:   LabelStalled, // Path A: commit data + no scorecard + publish within 730 days → Stalled
		},
		{
			name: "commit_data_partial_scorecard_maintained_ok_stalled",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 250,
					LatestHumanCommit:   ptrTime(now.AddDate(0, 0, -250)),
					CommitStats:         &CommitStats{},
					IsArchived:          false,
					IsDisabled:          false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: oldTime},
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained": NewScoreEntity("Maintained", 5, 10, "Medium maintenance"),
				// Missing "Vulnerabilities" → partial scorecard
			},
			want: LabelStalled, // Path A: commit data + Maintained ≥ 3 but missing Vuln → Stalled (not ReviewNeeded)
		},
		{
			name: "commit_data_no_scorecard_old_publish_old_commits_legacy_safe",
			analysis: func() *Analysis {
				jc := cfg.GetDefaultLifecycle()
				inactivity := jc.EolInactivityDays
				return &Analysis{
					RepoState: &RepoState{
						DaysSinceLastCommit: inactivity + 100,
						LatestHumanCommit:   ptrTime(now.AddDate(0, 0, -(inactivity + 100))),
						CommitStats:         &CommitStats{},
						IsArchived:          false,
						IsDisabled:          false,
					},
					ReleaseInfo: &ReleaseInfo{
						StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: now.AddDate(-3, 0, 0)}, // ~1095 > 730
					},
				}
			}(),
			scores: map[string]*ScoreEntity{},
			want:   LabelLegacySafe, // Path A: zero advisories + dormant > EolInactivityDays → Legacy-Safe
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
			want: LabelStalled, // No commit data (RepoState nil) → Path B C1: Maintained ≥ 3 → Stalled
		},
		{
			name: "no_commit_data_good_maintained_stalled_not_review",
			analysis: &Analysis{
				RepoState: &RepoState{
					IsArchived: false,
					IsDisabled: false,
					// CommitStats nil = no GitHub token scenario
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: oldTime},
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 5, 10, "Medium maintenance"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 6, 10, "Some vulnerabilities"),
			},
			want: LabelStalled, // Maintained ≥ 3 + no commit data → Stalled instead of ReviewNeeded
		},
		{
			name: "no_commit_data_no_scorecard_recent_publish_stalled",
			analysis: &Analysis{
				RepoState: &RepoState{
					IsArchived: false,
					IsDisabled: false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "2.0.0", PublishedAt: now.AddDate(0, 0, -400)}, // 400 days: 365 < 400 ≤ 730 → C3d Stalled
				},
			},
			scores: map[string]*ScoreEntity{},
			want:   LabelStalled, // C3d: no advisories, 366-730 days → Stalled
		},
		{
			name: "no_repo_state_no_scorecard_recent_publish_with_advisories_stalled",
			analysis: &Analysis{
				// RepoState nil = GitHub repo not identified
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{
						Version:     "2.0.0",
						PublishedAt: now.AddDate(0, 0, -400), // 400 days ≤ 730
						Advisories:  []Advisory{{ID: "GHSA-AAA", Source: "GHSA", URL: "https://github.com/advisories/GHSA-AAA"}},
					},
				},
			},
			scores: map[string]*ScoreEntity{},
			want:   LabelStalled, // C3b: advisories + publish ≤ 730 days → Stalled
		},
		{
			name: "no_commit_data_with_repo_state_no_scorecard_advisories_stalled",
			analysis: &Analysis{
				RepoState: &RepoState{
					IsArchived: false,
					IsDisabled: false,
					// LatestHumanCommit nil = GITHUB_TOKEN not set; absence of evidence, not dormancy proof
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{
						Version:     "2.0.0",
						PublishedAt: now.AddDate(0, 0, -400), // 400 days ≤ 730
						Advisories:  []Advisory{{ID: "GHSA-BBB", Source: "GHSA", URL: "https://github.com/advisories/GHSA-BBB"}},
					},
				},
			},
			scores: map[string]*ScoreEntity{},
			want:   LabelStalled, // C3b: advisories + publish ≤ 730 days → Stalled (not EOL-Effective)
		},
		{
			name: "no_commit_data_no_scorecard_old_publish_legacy_safe",
			analysis: &Analysis{
				RepoState: &RepoState{
					IsArchived: false,
					IsDisabled: false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: now.AddDate(-3, 0, 0)}, // 1095 days > 730
				},
			},
			scores: map[string]*ScoreEntity{},
			want:   LabelLegacySafe, // C3e: no advisories + publish > 730 days → Legacy Safe
		},
		// ── New C1-C3 decision tree tests ──
		{
			name: "no_commit_no_scorecard_no_advisory_recent_publish_active",
			analysis: &Analysis{
				RepoState: &RepoState{
					IsArchived: false,
					IsDisabled: false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "3.0.0", PublishedAt: now.AddDate(0, 0, -200)}, // 200 days ≤ 365
				},
			},
			scores: map[string]*ScoreEntity{},
			want:   LabelActive, // C3c: no advisories + publish ≤ 365 days → Active
		},
		{
			name: "no_commit_no_scorecard_no_advisory_mid_publish_stalled",
			analysis: &Analysis{
				RepoState: &RepoState{
					IsArchived: false,
					IsDisabled: false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "2.5.0", PublishedAt: now.AddDate(0, 0, -500)}, // 500 days: 365 < 500 ≤ 730
				},
			},
			scores: map[string]*ScoreEntity{},
			want:   LabelStalled, // C3d: no advisories + 366-730 days → Stalled
		},
		{
			name: "no_commit_no_scorecard_advisory_old_publish_eol_effective",
			analysis: &Analysis{
				RepoState: &RepoState{
					IsArchived: false,
					IsDisabled: false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{
						Version:     "1.0.0",
						PublishedAt: now.AddDate(-3, 0, 0), // ~1095 days > 730
						Advisories:  []Advisory{{ID: "GHSA-CCC", Source: "GHSA", URL: "https://github.com/advisories/GHSA-CCC"}},
					},
				},
			},
			scores: map[string]*ScoreEntity{},
			want:   LabelEOLEffective, // C3a: advisories + publish > 730 days → EOL-Effective
		},
		{
			name: "no_commit_no_scorecard_advisory_recent_publish_stalled",
			analysis: &Analysis{
				RepoState: &RepoState{
					IsArchived: false,
					IsDisabled: false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{
						Version:     "3.0.0",
						PublishedAt: now.AddDate(0, 0, -400), // 400 days: > 365 (enters inactive path) but ≤ 730
						Advisories:  []Advisory{{ID: "GHSA-DDD", Source: "GHSA", URL: "https://github.com/advisories/GHSA-DDD"}},
					},
				},
			},
			scores: map[string]*ScoreEntity{},
			want:   LabelStalled, // C3b: advisories + publish ≤ 730 days → Stalled
		},
		{
			name: "no_commit_no_scorecard_advisory_mid_publish_stalled",
			analysis: &Analysis{
				RepoState: &RepoState{
					IsArchived: false,
					IsDisabled: false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{
						Version:     "2.5.0",
						PublishedAt: now.AddDate(0, 0, -500), // 500 days: 365 < 500 ≤ 730
						Advisories:  []Advisory{{ID: "GHSA-EEE", Source: "GHSA", URL: "https://github.com/advisories/GHSA-EEE"}},
					},
				},
			},
			scores: map[string]*ScoreEntity{},
			want:   LabelStalled, // C3b2: advisories + 366-730 days → Stalled
		},
		{
			name: "no_commit_low_scorecard_advisory_old_publish_eol_effective",
			analysis: &Analysis{
				RepoState: &RepoState{
					IsArchived: false,
					IsDisabled: false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{
						Version:     "1.0.0",
						PublishedAt: now.AddDate(-3, 0, 0), // ~1095 days > 730
						Advisories:  []Advisory{{ID: "GHSA-FFF", Source: "GHSA", URL: "https://github.com/advisories/GHSA-FFF"}},
					},
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained": NewScoreEntity("Maintained", 1, 10, "Poorly maintained"),
			},
			want: LabelEOLEffective, // C2a: low maintenance + advisories + old publish → EOL-Effective
		},
		{
			name: "no_commit_low_scorecard_no_advisory_stalled",
			analysis: &Analysis{
				RepoState: &RepoState{
					IsArchived: false,
					IsDisabled: false,
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: now.AddDate(-3, 0, 0)},
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained": NewScoreEntity("Maintained", 1, 10, "Poorly maintained"),
			},
			want: LabelStalled, // C2b: low maintenance, no advisories → Stalled
		},
		{
			name: "no_commit_high_vuln_scorecard_sentinel_guard",
			analysis: &Analysis{
				RepoState: &RepoState{
					IsArchived: false,
					IsDisabled: false,
					// CommitStats nil = no GITHUB_TOKEN
				},
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: now.AddDate(-3, 0, 0)},
				},
			},
			scores: map[string]*ScoreEntity{
				"Maintained":      NewScoreEntity("Maintained", 5, 10, "Medium maintenance"),
				"Vulnerabilities": NewScoreEntity("Vulnerabilities", 9, 10, "Few vulnerabilities"),
			},
			want: LabelStalled, // C1: Maintained ≥ 3 → Stalled (NOT LegacySafe from sentinel 999.0)
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
