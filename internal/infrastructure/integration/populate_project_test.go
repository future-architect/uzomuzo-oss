package integration

import (
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
)

func TestPopulateProjectScorecard_ArchivedDetection(t *testing.T) {
	tests := []struct {
		name           string
		checks         []depsdev.ScorecardCheckSet
		wantIsArchived bool
	}{
		{
			name: "archived repo detected from Maintained reason",
			checks: []depsdev.ScorecardCheckSet{
				{Name: "Maintained", Score: 0, Reason: "project is archived"},
			},
			wantIsArchived: true,
		},
		{
			name: "archived repo detected case-insensitively",
			checks: []depsdev.ScorecardCheckSet{
				{Name: "Maintained", Score: 0, Reason: "Project is Archived"},
			},
			wantIsArchived: true,
		},
		{
			name: "archived embedded in longer reason",
			checks: []depsdev.ScorecardCheckSet{
				{Name: "Maintained", Score: 0, Reason: "0 commit(s) and 0 issue activity found in the last 90 days -- project is archived"},
			},
			wantIsArchived: true,
		},
		{
			name: "non-archived repo",
			checks: []depsdev.ScorecardCheckSet{
				{Name: "Maintained", Score: 5, Reason: "12 commit(s) and 3 issue activity found in the last 90 days"},
			},
			wantIsArchived: false,
		},
		{
			name: "no Maintained check present",
			checks: []depsdev.ScorecardCheckSet{
				{Name: "Code-Review", Score: 8, Reason: "all changesets reviewed"},
			},
			wantIsArchived: false,
		},
		{
			name: "empty checks",
			checks: nil,
			wantIsArchived: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &IntegrationService{}
			analysis := &domain.Analysis{
				OriginalPURL:  "pkg:golang/github.com/mitchellh/copystructure@1.0.0",
				EffectivePURL: "pkg:golang/github.com/mitchellh/copystructure@1.0.0",
				RepoURL:       "https://github.com/mitchellh/copystructure",
			}
			batch := &depsdev.BatchResult{
				Project: &depsdev.Project{
					Scorecard: depsdev.ScorecardData{
						Checks: tt.checks,
					},
				},
			}

			svc.populateProjectScorecard(analysis, batch)

			gotArchived := analysis.RepoState != nil && analysis.RepoState.IsArchived
			if gotArchived != tt.wantIsArchived {
				t.Errorf("IsArchived = %v, want %v", gotArchived, tt.wantIsArchived)
			}
		})
	}
}
