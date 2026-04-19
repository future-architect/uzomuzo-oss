package analysis

import (
	"testing"
	"time"
)

func TestRepository(t *testing.T) {
	tests := []struct {
		name       string
		repository Repository
		expected   Repository
	}{
		{
			name: "valid_repository",
			repository: Repository{
				URL:         "https://github.com/owner/repo",
				Owner:       "owner",
				Name:        "repo",
				StarsCount:  100,
				ForksCount:  20,
				Language:    "Go",
				Description: "Test repository",
				Summary:     "Test repository",
				Topics:      []string{"go", "library"},
				LastCommit:  time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			expected: Repository{
				URL:         "https://github.com/owner/repo",
				Owner:       "owner",
				Name:        "repo",
				StarsCount:  100,
				ForksCount:  20,
				Language:    "Go",
				Description: "Test repository",
				Summary:     "Test repository",
				Topics:      []string{"go", "library"},
				LastCommit:  time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "empty_repository",
			repository: Repository{
				URL:         "",
				Owner:       "",
				Name:        "",
				StarsCount:  0,
				ForksCount:  0,
				Language:    "",
				Description: "",
				Summary:     "",
				Topics:      nil,
				LastCommit:  time.Time{},
			},
			expected: Repository{
				URL:         "",
				Owner:       "",
				Name:        "",
				StarsCount:  0,
				ForksCount:  0,
				Language:    "",
				Description: "",
				Summary:     "",
				Topics:      nil,
				LastCommit:  time.Time{},
			},
		},
		{
			name: "fetched_repository_zero_topics",
			repository: Repository{
				URL:    "https://github.com/owner/quiet",
				Topics: []string{},
			},
			expected: Repository{
				URL:    "https://github.com/owner/quiet",
				Topics: []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.repository.URL != tt.expected.URL {
				t.Errorf("Repository.URL = %v, want %v", tt.repository.URL, tt.expected.URL)
			}
			if tt.repository.Owner != tt.expected.Owner {
				t.Errorf("Repository.Owner = %v, want %v", tt.repository.Owner, tt.expected.Owner)
			}
			if tt.repository.Name != tt.expected.Name {
				t.Errorf("Repository.Name = %v, want %v", tt.repository.Name, tt.expected.Name)
			}
			if tt.repository.StarsCount != tt.expected.StarsCount {
				t.Errorf("Repository.StarsCount = %v, want %v", tt.repository.StarsCount, tt.expected.StarsCount)
			}
			if tt.repository.ForksCount != tt.expected.ForksCount {
				t.Errorf("Repository.ForksCount = %v, want %v", tt.repository.ForksCount, tt.expected.ForksCount)
			}
			if tt.repository.Language != tt.expected.Language {
				t.Errorf("Repository.Language = %v, want %v", tt.repository.Language, tt.expected.Language)
			}
			if tt.repository.Description != tt.expected.Description {
				t.Errorf("Repository.Description = %v, want %v", tt.repository.Description, tt.expected.Description)
			}
			if tt.repository.Summary != tt.expected.Summary {
				t.Errorf("Repository.Summary = %v, want %v", tt.repository.Summary, tt.expected.Summary)
			}
			// Distinguish nil vs []string{} (empty) sentinels — both compare as len==0.
			if (tt.repository.Topics == nil) != (tt.expected.Topics == nil) {
				t.Errorf("Repository.Topics nil mismatch: got nil=%v, want nil=%v",
					tt.repository.Topics == nil, tt.expected.Topics == nil)
			}
			if len(tt.repository.Topics) != len(tt.expected.Topics) {
				t.Errorf("Repository.Topics length = %d, want %d", len(tt.repository.Topics), len(tt.expected.Topics))
			}
			for i := range tt.repository.Topics {
				if tt.repository.Topics[i] != tt.expected.Topics[i] {
					t.Errorf("Repository.Topics[%d] = %v, want %v", i, tt.repository.Topics[i], tt.expected.Topics[i])
				}
			}
			if !tt.repository.LastCommit.Equal(tt.expected.LastCommit) {
				t.Errorf("Repository.LastCommit = %v, want %v", tt.repository.LastCommit, tt.expected.LastCommit)
			}
		})
	}
}

func TestPackage(t *testing.T) {
	tests := []struct {
		name     string
		pkg      Package
		expected Package
	}{
		{
			name: "valid_package",
			pkg: Package{
				PURL:      "pkg:npm/lodash@4.17.21",
				Ecosystem: "npm",
				Version:   "4.17.21",
			},
			expected: Package{
				PURL:      "pkg:npm/lodash@4.17.21",
				Ecosystem: "npm",
				Version:   "4.17.21",
			},
		},
		{
			name: "go_package",
			pkg: Package{
				PURL:      "pkg:golang/github.com/gin-gonic/gin@v1.8.1",
				Ecosystem: "golang",
				Version:   "v1.8.1",
			},
			expected: Package{
				PURL:      "pkg:golang/github.com/gin-gonic/gin@v1.8.1",
				Ecosystem: "golang",
				Version:   "v1.8.1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.pkg.PURL != tt.expected.PURL {
				t.Errorf("Package.PURL = %v, want %v", tt.pkg.PURL, tt.expected.PURL)
			}
			if tt.pkg.Ecosystem != tt.expected.Ecosystem {
				t.Errorf("Package.Ecosystem = %v, want %v", tt.pkg.Ecosystem, tt.expected.Ecosystem)
			}
			if tt.pkg.Version != tt.expected.Version {
				t.Errorf("Package.Version = %v, want %v", tt.pkg.Version, tt.expected.Version)
			}
		})
	}
}

func TestVersionDetail(t *testing.T) {
	tests := []struct {
		name    string
		version VersionDetail
		want    VersionDetail
	}{
		{
			name: "stable_version",
			version: VersionDetail{
				Version:      "1.0.0",
				PublishedAt:  time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
				IsPrerelease: false,
			},
			want: VersionDetail{
				Version:      "1.0.0",
				PublishedAt:  time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
				IsPrerelease: false,
			},
		},
		{
			name: "prerelease_version",
			version: VersionDetail{
				Version:      "2.0.0-beta.1",
				PublishedAt:  time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC),
				IsPrerelease: true,
			},
			want: VersionDetail{
				Version:      "2.0.0-beta.1",
				PublishedAt:  time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC),
				IsPrerelease: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.version.Version != tt.want.Version {
				t.Errorf("VersionDetail.Version = %v, want %v", tt.version.Version, tt.want.Version)
			}
			if !tt.version.PublishedAt.Equal(tt.want.PublishedAt) {
				t.Errorf("VersionDetail.PublishedAt = %v, want %v", tt.version.PublishedAt, tt.want.PublishedAt)
			}
			if tt.version.IsPrerelease != tt.want.IsPrerelease {
				t.Errorf("VersionDetail.IsPrerelease = %v, want %v", tt.version.IsPrerelease, tt.want.IsPrerelease)
			}
		})
	}
}

func TestScore(t *testing.T) {
	tests := []struct {
		name  string
		score Score
		want  Score
	}{
		{
			name: "valid_score",
			score: Score{
				Name:       "Security-Policy",
				Value:      8,
				Reason:     "Security policy is present",
				Details:    []string{"Found SECURITY.md", "Contact info available"},
				ComputedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			want: Score{
				Name:       "Security-Policy",
				Value:      8,
				Reason:     "Security policy is present",
				Details:    []string{"Found SECURITY.md", "Contact info available"},
				ComputedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "zero_score",
			score: Score{
				Name:       "Binary-Artifacts",
				Value:      0,
				Reason:     "Binary artifacts found",
				Details:    []string{"Found: vendor/binary.exe"},
				ComputedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			want: Score{
				Name:       "Binary-Artifacts",
				Value:      0,
				Reason:     "Binary artifacts found",
				Details:    []string{"Found: vendor/binary.exe"},
				ComputedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.score.Name != tt.want.Name {
				t.Errorf("Score.Name = %v, want %v", tt.score.Name, tt.want.Name)
			}
			if tt.score.Value != tt.want.Value {
				t.Errorf("Score.Value = %v, want %v", tt.score.Value, tt.want.Value)
			}
			if tt.score.Reason != tt.want.Reason {
				t.Errorf("Score.Reason = %v, want %v", tt.score.Reason, tt.want.Reason)
			}
			if len(tt.score.Details) != len(tt.want.Details) {
				t.Errorf("Score.Details length = %v, want %v", len(tt.score.Details), len(tt.want.Details))
			}
			for i, detail := range tt.score.Details {
				if detail != tt.want.Details[i] {
					t.Errorf("Score.Details[%d] = %v, want %v", i, detail, tt.want.Details[i])
				}
			}
			if !tt.score.ComputedAt.Equal(tt.want.ComputedAt) {
				t.Errorf("Score.ComputedAt = %v, want %v", tt.score.ComputedAt, tt.want.ComputedAt)
			}
		})
	}
}

func TestCommitStats(t *testing.T) {
	tests := []struct {
		name  string
		stats CommitStats
		want  CommitStats
	}{
		{
			name: "balanced_commits",
			stats: CommitStats{
				Total:       100,
				BotCommits:  30,
				UserCommits: 70,
				BotRatio:    0.3,
				UserRatio:   0.7,
			},
			want: CommitStats{
				Total:       100,
				BotCommits:  30,
				UserCommits: 70,
				BotRatio:    0.3,
				UserRatio:   0.7,
			},
		},
		{
			name: "mostly_bot_commits",
			stats: CommitStats{
				Total:       50,
				BotCommits:  45,
				UserCommits: 5,
				BotRatio:    0.9,
				UserRatio:   0.1,
			},
			want: CommitStats{
				Total:       50,
				BotCommits:  45,
				UserCommits: 5,
				BotRatio:    0.9,
				UserRatio:   0.1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.stats.Total != tt.want.Total {
				t.Errorf("CommitStats.Total = %v, want %v", tt.stats.Total, tt.want.Total)
			}
			if tt.stats.BotCommits != tt.want.BotCommits {
				t.Errorf("CommitStats.BotCommits = %v, want %v", tt.stats.BotCommits, tt.want.BotCommits)
			}
			if tt.stats.UserCommits != tt.want.UserCommits {
				t.Errorf("CommitStats.UserCommits = %v, want %v", tt.stats.UserCommits, tt.want.UserCommits)
			}
			if tt.stats.BotRatio != tt.want.BotRatio {
				t.Errorf("CommitStats.BotRatio = %v, want %v", tt.stats.BotRatio, tt.want.BotRatio)
			}
			if tt.stats.UserRatio != tt.want.UserRatio {
				t.Errorf("CommitStats.UserRatio = %v, want %v", tt.stats.UserRatio, tt.want.UserRatio)
			}
		})
	}
}

func TestRepoState(t *testing.T) {
	humanCommitTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		state RepoState
		want  RepoState
	}{
		{
			name: "active_repository",
			state: RepoState{
				LatestHumanCommit:   &humanCommitTime,
				DaysSinceLastCommit: 5,
				CommitStats: &CommitStats{
					Total:       100,
					BotCommits:  20,
					UserCommits: 80,
					BotRatio:    0.2,
					UserRatio:   0.8,
				},
				IsArchived: false,
				IsDisabled: false,
				IsFork:     false,
			},
			want: RepoState{
				LatestHumanCommit:   &humanCommitTime,
				DaysSinceLastCommit: 5,
				CommitStats: &CommitStats{
					Total:       100,
					BotCommits:  20,
					UserCommits: 80,
					BotRatio:    0.2,
					UserRatio:   0.8,
				},
				IsArchived: false,
				IsDisabled: false,
				IsFork:     false,
			},
		},
		{
			name: "archived_repository",
			state: RepoState{
				LatestHumanCommit:   nil,
				DaysSinceLastCommit: 365,
				CommitStats:         nil,
				IsArchived:          true,
				IsDisabled:          false,
				IsFork:              false,
			},
			want: RepoState{
				LatestHumanCommit:   nil,
				DaysSinceLastCommit: 365,
				CommitStats:         nil,
				IsArchived:          true,
				IsDisabled:          false,
				IsFork:              false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.state.LatestHumanCommit == nil && tt.want.LatestHumanCommit != nil {
				t.Errorf("RepoState.LatestHumanCommit = nil, want %v", tt.want.LatestHumanCommit)
			} else if tt.state.LatestHumanCommit != nil && tt.want.LatestHumanCommit == nil {
				t.Errorf("RepoState.LatestHumanCommit = %v, want nil", tt.state.LatestHumanCommit)
			} else if tt.state.LatestHumanCommit != nil && tt.want.LatestHumanCommit != nil {
				if !tt.state.LatestHumanCommit.Equal(*tt.want.LatestHumanCommit) {
					t.Errorf("RepoState.LatestHumanCommit = %v, want %v", tt.state.LatestHumanCommit, tt.want.LatestHumanCommit)
				}
			}

			if tt.state.DaysSinceLastCommit != tt.want.DaysSinceLastCommit {
				t.Errorf("RepoState.DaysSinceLastCommit = %v, want %v", tt.state.DaysSinceLastCommit, tt.want.DaysSinceLastCommit)
			}
			if tt.state.IsArchived != tt.want.IsArchived {
				t.Errorf("RepoState.IsArchived = %v, want %v", tt.state.IsArchived, tt.want.IsArchived)
			}
			if tt.state.IsDisabled != tt.want.IsDisabled {
				t.Errorf("RepoState.IsDisabled = %v, want %v", tt.state.IsDisabled, tt.want.IsDisabled)
			}
			if tt.state.IsFork != tt.want.IsFork {
				t.Errorf("RepoState.IsFork = %v, want %v", tt.state.IsFork, tt.want.IsFork)
			}
		})
	}
}

// --- ResolvedLicense helpers ---

func TestResolvedLicense_IsZero(t *testing.T) {
	tests := []struct {
		name string
		in   ResolvedLicense
		want bool
	}{
		{name: "zero_value", in: ResolvedLicense{}, want: true},
		{name: "spdx_license", in: ResolvedLicense{Identifier: "MIT", Raw: "MIT", IsSPDX: true, Source: LicenseSourceDepsDevProjectSPDX}, want: false},
		{name: "nonstandard_project", in: ResolvedLicense{Identifier: "", Raw: "Custom License", Source: LicenseSourceDepsDevProjectNonStandard}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v (input=%+v)", got, tt.want, tt.in)
			}
		})
	}
}

func TestResolvedLicense_IsNonStandard(t *testing.T) {
	tests := []struct {
		name string
		in   ResolvedLicense
		want bool
	}{
		{name: "zero_value", in: ResolvedLicense{}, want: false},
		{name: "spdx_project", in: ResolvedLicense{Identifier: "Apache-2.0", Raw: "Apache-2.0", IsSPDX: true, Source: LicenseSourceDepsDevProjectSPDX}, want: false},
		{name: "project_nonstandard", in: ResolvedLicense{Identifier: "", Raw: "Custom License", Source: LicenseSourceDepsDevProjectNonStandard}, want: true},
		{name: "github_nonstandard", in: ResolvedLicense{Identifier: "", Raw: "See LICENSE", Source: LicenseSourceGitHubProjectNonStandard}, want: true},
		{name: "version_raw", in: ResolvedLicense{Identifier: "Proprietary", Raw: "Proprietary", Source: LicenseSourceDepsDevVersionRaw}, want: true},
		{name: "fallback_from_project", in: ResolvedLicense{Identifier: "MIT", Raw: "MIT", IsSPDX: true, Source: LicenseSourceProjectFallback}, want: false},
		{name: "derived_from_version", in: ResolvedLicense{Identifier: "BSD-3-Clause", Raw: "BSD-3-Clause", IsSPDX: true, Source: LicenseSourceDerivedFromVersion}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.IsNonStandard(); got != tt.want {
				t.Errorf("IsNonStandard() = %v, want %v (input=%+v)", got, tt.want, tt.in)
			}
		})
	}
}
