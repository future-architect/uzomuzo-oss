package analysis

import (
	"testing"
	"time"
)

func TestAnalysis_HasRecentStableRelease(t *testing.T) {
	now := time.Now()
	oldTime := now.AddDate(0, 0, -400)    // 400 days ago
	recentTime := now.AddDate(0, 0, -100) // 100 days ago

	tests := []struct {
		name     string
		analysis *Analysis
		days     int
		want     bool
	}{
		{
			name: "recent_stable_release",
			analysis: &Analysis{
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{
						Version:     "1.0.0",
						PublishedAt: recentTime,
					},
				},
			},
			days: 365,
			want: true,
		},
		{
			name: "old_stable_release",
			analysis: &Analysis{
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{
						Version:     "1.0.0",
						PublishedAt: oldTime,
					},
				},
			},
			days: 365,
			want: false,
		},
		{
			name: "no_stable_release",
			analysis: &Analysis{
				ReleaseInfo: &ReleaseInfo{
					StableVersion: nil,
				},
			},
			days: 365,
			want: false,
		},
		{
			name: "no_release_info",
			analysis: &Analysis{
				ReleaseInfo: nil,
			},
			days: 365,
			want: false,
		},
		{
			name: "zero_time_stable",
			analysis: &Analysis{
				ReleaseInfo: &ReleaseInfo{
					StableVersion: &VersionDetail{
						Version:     "1.0.0",
						PublishedAt: time.Time{},
					},
				},
			},
			days: 365,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.analysis.HasRecentStableRelease(tt.days); got != tt.want {
				t.Errorf("Analysis.HasRecentStableRelease() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnalysis_HasRecentPrereleaseRelease(t *testing.T) {
	now := time.Now()
	oldTime := now.AddDate(0, 0, -200)   // 200 days ago
	recentTime := now.AddDate(0, 0, -50) // 50 days ago

	tests := []struct {
		name     string
		analysis *Analysis
		days     int
		want     bool
	}{
		{
			name: "recent_prerelease",
			analysis: &Analysis{
				ReleaseInfo: &ReleaseInfo{
					PreReleaseVersion: &VersionDetail{
						Version:      "2.0.0-beta.1",
						PublishedAt:  recentTime,
						IsPrerelease: true,
					},
				},
			},
			days: 180,
			want: true,
		},
		{
			name: "old_prerelease",
			analysis: &Analysis{
				ReleaseInfo: &ReleaseInfo{
					PreReleaseVersion: &VersionDetail{
						Version:      "2.0.0-beta.1",
						PublishedAt:  oldTime,
						IsPrerelease: true,
					},
				},
			},
			days: 180,
			want: false,
		},
		{
			name: "no_prerelease",
			analysis: &Analysis{
				ReleaseInfo: &ReleaseInfo{
					PreReleaseVersion: nil,
				},
			},
			days: 180,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.analysis.HasRecentPrereleaseRelease(tt.days); got != tt.want {
				t.Errorf("Analysis.HasRecentPrereleaseRelease() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnalysis_HasRecentCommit(t *testing.T) {
	tests := []struct {
		name     string
		analysis *Analysis
		days     int
		want     bool
	}{
		{
			name: "recent_commit",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 10,
				},
			},
			days: 30,
			want: true,
		},
		{
			name: "old_commit",
			analysis: &Analysis{
				RepoState: &RepoState{
					DaysSinceLastCommit: 50,
				},
			},
			days: 30,
			want: false,
		},
		{
			name: "no_repo_state",
			analysis: &Analysis{
				RepoState: nil,
			},
			days: 30,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.analysis.HasRecentCommit(tt.days); got != tt.want {
				t.Errorf("Analysis.HasRecentCommit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnalysis_GetDaysSinceLastHumanCommit(t *testing.T) {
	now := time.Now()
	humanCommitTime := now.AddDate(0, 0, -20) // 20 days ago

	tests := []struct {
		name     string
		analysis *Analysis
		want     int
	}{
		{
			name: "with_human_commit",
			analysis: &Analysis{
				RepoState: &RepoState{
					LatestHumanCommit: &humanCommitTime,
				},
			},
			want: 20, // approximately, allowing for test execution time
		},
		{
			name: "no_human_commit",
			analysis: &Analysis{
				RepoState: &RepoState{
					LatestHumanCommit: nil,
				},
			},
			want: 9999,
		},
		{
			name: "no_repo_state",
			analysis: &Analysis{
				RepoState: nil,
			},
			want: 9999,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.analysis.GetDaysSinceLastHumanCommit()
			// Allow for some variance in time calculations
			if tt.want == 9999 {
				if got != tt.want {
					t.Errorf("Analysis.GetDaysSinceLastHumanCommit() = %v, want %v", got, tt.want)
				}
			} else {
				// Allow ±2 days variance for test execution timing
				if got < tt.want-2 || got > tt.want+2 {
					t.Errorf("Analysis.GetDaysSinceLastHumanCommit() = %v, want approximately %v", got, tt.want)
				}
			}
		})
	}
}

func TestAnalysis_IsMaintenanceOk(t *testing.T) {
	tests := []struct {
		name     string
		analysis *Analysis
		want     bool
	}{
		{
			name: "good_maintenance",
			analysis: &Analysis{
				Scores: map[string]*ScoreEntity{
					"Maintained": NewScoreEntity("Maintained", 5, 10, "Well maintained"),
				},
			},
			want: true,
		},
		{
			name: "poor_maintenance",
			analysis: &Analysis{
				Scores: map[string]*ScoreEntity{
					"Maintained": NewScoreEntity("Maintained", 2, 10, "Poorly maintained"),
				},
			},
			want: false,
		},
		{
			name: "no_maintenance_score",
			analysis: &Analysis{
				Scores: map[string]*ScoreEntity{
					"Security": NewScoreEntity("Security", 8, 10, "Good security"),
				},
			},
			want: false,
		},
		{
			name: "no_scores",
			analysis: &Analysis{
				Scores: nil,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.analysis.IsMaintenanceOk(); got != tt.want {
				t.Errorf("Analysis.IsMaintenanceOk() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnalysis_GetCheckMap(t *testing.T) {
	tests := []struct {
		name     string
		analysis *Analysis
		want     map[string]float64
	}{
		{
			name: "multiple_scores",
			analysis: &Analysis{
				Scores: map[string]*ScoreEntity{
					"Security":   NewScoreEntity("Security", 8, 10, "Good security"),
					"Maintained": NewScoreEntity("Maintained", 6, 10, "Decent maintenance"),
				},
			},
			want: map[string]float64{
				"Security":   8.0,
				"Maintained": 6.0,
			},
		},
		{
			name: "no_scores",
			analysis: &Analysis{
				Scores: map[string]*ScoreEntity{},
			},
			want: map[string]float64{},
		},
		{
			name: "nil_scores",
			analysis: &Analysis{
				Scores: nil,
			},
			want: map[string]float64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.analysis.GetCheckMap()

			if len(got) != len(tt.want) {
				t.Errorf("Analysis.GetCheckMap() length = %v, want %v", len(got), len(tt.want))
				return
			}

			for key, expectedValue := range tt.want {
				if gotValue, exists := got[key]; !exists {
					t.Errorf("Analysis.GetCheckMap() missing key %v", key)
				} else if gotValue != expectedValue {
					t.Errorf("Analysis.GetCheckMap()[%v] = %v, want %v", key, gotValue, expectedValue)
				}
			}
		})
	}
}

func TestAnalysis_GetScore(t *testing.T) {
	tests := []struct {
		name      string
		analysis  *Analysis
		scoreName string
		want      float64
	}{
		{
			name: "existing_score",
			analysis: &Analysis{
				Scores: map[string]*ScoreEntity{
					"Security": NewScoreEntity("Security", 8, 10, "Good security"),
				},
			},
			scoreName: "Security",
			want:      8.0,
		},
		{
			name: "non_existing_score",
			analysis: &Analysis{
				Scores: map[string]*ScoreEntity{
					"Security": NewScoreEntity("Security", 8, 10, "Good security"),
				},
			},
			scoreName: "NonExisting",
			want:      -1.0,
		},
		{
			name: "nil_scores",
			analysis: &Analysis{
				Scores: nil,
			},
			scoreName: "Security",
			want:      -1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.analysis.GetScore(tt.scoreName); got != tt.want {
				t.Errorf("Analysis.GetScore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnalysis_HasDeprecatedRequestedVersion(t *testing.T) {
	t.Run("deprecated_requested", func(t *testing.T) {
		a := &Analysis{ReleaseInfo: &ReleaseInfo{RequestedVersion: &VersionDetail{Version: "1.0.0", IsDeprecated: true}}}
		if !a.HasDeprecatedRequestedVersion() {
			t.Errorf("expected true for deprecated requested version")
		}
	})
	t.Run("non_deprecated_requested", func(t *testing.T) {
		a := &Analysis{ReleaseInfo: &ReleaseInfo{RequestedVersion: &VersionDetail{Version: "1.0.0", IsDeprecated: false}}}
		if a.HasDeprecatedRequestedVersion() {
			t.Errorf("expected false for non-deprecated requested version")
		}
	})
	t.Run("no_requested", func(t *testing.T) {
		a := &Analysis{ReleaseInfo: &ReleaseInfo{RequestedVersion: nil}}
		if a.HasDeprecatedRequestedVersion() {
			t.Errorf("expected false when no requested version")
		}
	})
	t.Run("no_release_info", func(t *testing.T) {
		a := &Analysis{ReleaseInfo: nil}
		if a.HasDeprecatedRequestedVersion() {
			t.Errorf("expected false when no release info")
		}
	})
}
