package analysis

import (
	"context"
	"testing"
	"time"

	cfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// flagged builds an AdvisoryDBState reporting the package as unmaintained.
func flagged(id, summary string) *AdvisoryDBState {
	return &AdvisoryDBState{
		Unmaintained: true,
		AdvisoryID:   id,
		Summary:      summary,
		Reference:    "https://rustsec.org/advisories/" + id + ".html",
		Published:    time.Now().AddDate(0, 0, -100),
	}
}

// TestLifecycleAssessor_AdvisoryDBUnmaintained is the decision table for branch
// 1.4, the advisory-database maintenance warning (ADR-0025).
func TestLifecycleAssessor_AdvisoryDBUnmaintained(t *testing.T) {
	t.Parallel()
	now := time.Now()
	recent := now.AddDate(0, 0, -10)
	inactivity := cfg.GetDefaultLifecycle().EolInactivityDays
	dormant := now.AddDate(0, 0, -(inactivity + 10))

	activeRepo := func() *RepoState {
		return &RepoState{DaysSinceLastCommit: 5, LatestHumanCommit: &recent, CommitStats: &CommitStats{}}
	}
	dormantRepo := func() *RepoState {
		return &RepoState{DaysSinceLastCommit: inactivity + 10, LatestHumanCommit: &dormant, CommitStats: &CommitStats{}}
	}
	healthyScores := func() map[string]*ScoreEntity {
		return map[string]*ScoreEntity{
			"Maintained":      NewScoreEntity("Maintained", 8, 10, "ok"),
			"Vulnerabilities": NewScoreEntity("Vulnerabilities", 9, 10, "ok"),
		}
	}
	highAdvisory := []Advisory{{ID: "RUSTSEC-2099-0001", Source: "RUSTSEC", CVSS3Score: 9.1,
		URL: "https://rustsec.org/advisories/RUSTSEC-2099-0001.html"}}

	tests := []struct {
		name       string
		analysis   *Analysis
		scores     map[string]*ScoreEntity
		eol        EOLStatus
		wantLabel  MaintenanceStatus
		wantReason string
		wantTrace  string
		wantSignal bool // SignalAdvisoryDBUnmaintained present
	}{
		{
			name: "no advisory-db state means the branch never fires",
			analysis: &Analysis{
				RepoState:   activeRepo(),
				ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: recent}},
			},
			scores:     healthyScores(),
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelActive,
			wantReason: "Actively maintained with recent releases",
			wantTrace:  "active_path",
		},
		{
			name: "database asked, nothing flagged, branch never fires",
			analysis: &Analysis{
				RepoState:       activeRepo(),
				AdvisoryDBState: &AdvisoryDBState{},
				ReleaseInfo:     &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: recent}},
			},
			scores:     healthyScores(),
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelActive,
			wantReason: "Actively maintained with recent releases",
			wantTrace:  "active_path",
		},
		{
			// The correction this feature exists for: an actively-committing repo
			// whose crate RustSec has flagged is not Active.
			name: "flagged alone is Stalled, not Active",
			analysis: &Analysis{
				RepoState:       activeRepo(),
				AdvisoryDBState: flagged("RUSTSEC-2024-0375", "atty is unmaintained"),
				ReleaseInfo:     &ReleaseInfo{StableVersion: &VersionDetail{Version: "0.2.14", PublishedAt: recent}},
			},
			scores:     healthyScores(),
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelStalled,
			wantReason: "Flagged unmaintained by RUSTSEC-2024-0375: atty is unmaintained",
			wantTrace:  "advisory_db_unmaintained",
			wantSignal: true,
		},
		{
			name: "an empty summary leaves the reason bare rather than dangling",
			analysis: &Analysis{
				RepoState:       activeRepo(),
				AdvisoryDBState: flagged("RUSTSEC-2024-0375", ""),
			},
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelStalled,
			wantReason: "Flagged unmaintained by RUSTSEC-2024-0375",
			wantTrace:  "advisory_db_unmaintained",
			wantSignal: true,
		},
		{
			// The other half of severityAwareLabel, matching what every other
			// ecosystem already does with an unmaintained package.
			name: "flagged plus an unpatched HIGH advisory is EOL-Effective",
			analysis: &Analysis{
				RepoState:       activeRepo(),
				AdvisoryDBState: flagged("RUSTSEC-2024-0375", "atty is unmaintained"),
				ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "0.2.14",
					PublishedAt: recent, Advisories: highAdvisory}},
			},
			scores:     healthyScores(),
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelEOLEffective,
			wantReason: "Unmaintained per RUSTSEC-2024-0375, unpatched vulnerabilities",
			wantTrace:  "advisory_db_unmaintained_unpatched_vulns",
			wantSignal: true,
		},
		{
			// An advisory of unknown severity is treated as potentially high by
			// hasHighSeverityAdvisories; the branch must inherit that, not
			// re-decide it.
			name: "flagged plus an unknown-severity advisory is EOL-Effective",
			analysis: &Analysis{
				RepoState:       activeRepo(),
				AdvisoryDBState: flagged("RUSTSEC-2024-0375", "atty is unmaintained"),
				ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "0.2.14", PublishedAt: recent,
					Advisories: []Advisory{{ID: "GHSA-XXX", Source: "GHSA"}}}},
			},
			scores:     healthyScores(),
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelEOLEffective,
			wantReason: "Unmaintained per RUSTSEC-2024-0375, unpatched vulnerabilities",
			wantTrace:  "advisory_db_unmaintained_unpatched_vulns",
			wantSignal: true,
		},
		{
			// RustSec files the marker as an advisory, and deps.dev lists it on
			// the version with no CVSS score. Counting it would feed the branch
			// its own evidence back as a vulnerability and make Stalled
			// unreachable — term_size@0.3.2 has exactly this shape in production.
			name: "the marker's own advisory is not a vulnerability",
			analysis: &Analysis{
				RepoState:       activeRepo(),
				AdvisoryDBState: flagged("RUSTSEC-2020-0163", "term_size is unmaintained"),
				ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "0.3.2", PublishedAt: recent,
					Advisories: []Advisory{{ID: "RUSTSEC-2020-0163", Source: "RUSTSEC"}}}},
			},
			scores:     healthyScores(),
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelStalled,
			wantReason: "Flagged unmaintained by RUSTSEC-2020-0163: term_size is unmaintained",
			wantTrace:  "advisory_db_unmaintained",
			wantSignal: true,
		},
		{
			// Exclusion is scoped to the branch's own evidence: a real
			// vulnerability alongside the marker still reaches EOL-Effective.
			name: "the marker alongside a real advisory is still EOL-Effective",
			analysis: &Analysis{
				RepoState:       activeRepo(),
				AdvisoryDBState: flagged("RUSTSEC-2020-0163", "term_size is unmaintained"),
				ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "0.3.2", PublishedAt: recent,
					Advisories: []Advisory{
						{ID: "RUSTSEC-2020-0163", Source: "RUSTSEC"},
						{ID: "GHSA-XXX", Source: "GHSA", CVSS3Score: 9.1},
					}}},
			},
			scores:     healthyScores(),
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelEOLEffective,
			wantReason: "Unmaintained per RUSTSEC-2020-0163, unpatched vulnerabilities",
			wantTrace:  "advisory_db_unmaintained_unpatched_vulns",
			wantSignal: true,
		},
		{
			// The Legacy-Safe correction: "no known vulnerabilities" and "nobody
			// is looking" are not the same claim.
			name: "flagged and dormant with zero advisories is Stalled, not Legacy-Safe",
			analysis: &Analysis{
				RepoState:       dormantRepo(),
				AdvisoryDBState: flagged("RUSTSEC-2024-0375", "atty is unmaintained"),
				ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "0.2.14",
					PublishedAt: now.AddDate(-4, 0, 0)}},
			},
			scores:     map[string]*ScoreEntity{},
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelStalled,
			wantReason: "Flagged unmaintained by RUSTSEC-2024-0375: atty is unmaintained",
			wantTrace:  "advisory_db_unmaintained",
			wantSignal: true,
		},
		{
			// The ordering collision the branch placement exists to resolve. The
			// archive branch returns Stalled unconditionally, so sitting after it
			// would hide this EOL-Effective verdict.
			name: "an archived repository does not mask the EOL-Effective verdict",
			analysis: &Analysis{
				RepoState:       &RepoState{DaysSinceLastCommit: 5, LatestHumanCommit: &recent, CommitStats: &CommitStats{}, IsArchived: true},
				AdvisoryDBState: flagged("RUSTSEC-2024-0375", "atty is unmaintained"),
				ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "0.2.14",
					PublishedAt: recent, Advisories: highAdvisory}},
			},
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelEOLEffective,
			wantReason: "Unmaintained per RUSTSEC-2024-0375, unpatched vulnerabilities",
			wantTrace:  "advisory_db_unmaintained_unpatched_vulns",
			wantSignal: true,
		},
		{
			// A primary source outranks a curator: the flag must not downgrade a
			// real EOL declaration to Stalled.
			name: "a primary-source EOL still wins over the flag",
			analysis: &Analysis{
				RepoState:       activeRepo(),
				AdvisoryDBState: flagged("RUSTSEC-2024-0375", "atty is unmaintained"),
			},
			eol:        EOLStatus{State: EOLEndOfLife, Reason: "Deprecated on crates.io"},
			wantLabel:  LabelEOLConfirmed,
			wantReason: "Deprecated on crates.io",
			wantTrace:  "primary_source_eol override",
		},
		{
			name: "a scheduled EOL still wins over the flag",
			analysis: &Analysis{
				RepoState:       activeRepo(),
				AdvisoryDBState: flagged("RUSTSEC-2024-0375", "atty is unmaintained"),
			},
			eol:        EOLStatus{State: EOLScheduled},
			wantLabel:  LabelEOLScheduled,
			wantReason: "Scheduled EOL",
			wantTrace:  "planned_eol override",
		},
		{
			// Distribution withdrawal is the stronger statement and is checked
			// first: the package is not installable at all.
			name: "an all-yanked package stays Review Needed",
			analysis: &Analysis{
				RepoState:       activeRepo(),
				RegistryState:   withdrawn(RegistryCrates, ""),
				AdvisoryDBState: flagged("RUSTSEC-2024-0375", "atty is unmaintained"),
			},
			eol:        EOLStatus{State: EOLNotEOL},
			wantLabel:  LabelReviewNeeded,
			wantReason: "All releases yanked on crates.io",
			wantTrace:  "all_releases_yanked_review_needed",
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
			if res.Reason != tt.wantReason {
				t.Errorf("Reason: got %q, want %q", res.Reason, tt.wantReason)
			}
			if !containsString(res.Trace, tt.wantTrace) {
				t.Errorf("Trace: got %v, want it to contain %q", res.Trace, tt.wantTrace)
			}
			if got := hasSignal(res.Signals, SignalAdvisoryDBUnmaintained); got != tt.wantSignal {
				t.Errorf("SignalAdvisoryDBUnmaintained present: got %v, want %v (signals %+v)",
					got, tt.wantSignal, res.Signals)
			}
			// The property protecting downstream human judgements: this path must
			// never assert an EOL state of its own.
			if tt.eol.State == EOLNotEOL && res.Label != string(LabelEOLConfirmed) {
				if got := tt.analysis.EOL.State; got != "" && got != EOLNotEOL {
					t.Errorf("EOLStatus.State was mutated to %q by the assessment", got)
				}
			}
		})
	}
}

// TestLifecycleAssessor_AdvisoryDBUnmaintainedCarriesEvidence pins the signals
// the branch emits, so a later edit cannot quietly drop the advisory ID that
// makes the verdict auditable.
func TestLifecycleAssessor_AdvisoryDBUnmaintainedCarriesEvidence(t *testing.T) {
	t.Parallel()
	recent := time.Now().AddDate(0, 0, -10)
	a := &Analysis{
		RepoState:       &RepoState{DaysSinceLastCommit: 5, LatestHumanCommit: &recent, CommitStats: &CommitStats{}, IsArchived: true},
		AdvisoryDBState: flagged("RUSTSEC-2024-0375", "atty is unmaintained"),
		ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "0.2.14", PublishedAt: recent,
			Advisories: []Advisory{{ID: "RUSTSEC-2099-0001", Source: "RUSTSEC", CVSS3Score: 9.1}}}},
	}
	res, err := NewLifecycleAssessorService().Assess(context.Background(),
		AssessmentInput{Analysis: a, EOL: EOLStatus{State: EOLNotEOL}})
	if err != nil {
		t.Fatalf("Assess failed: %v", err)
	}
	var advisoryDBValue string
	for _, s := range res.Signals {
		if s.Name == SignalAdvisoryDBUnmaintained {
			advisoryDBValue = s.Value
		}
	}
	if advisoryDBValue != "RUSTSEC-2024-0375" {
		t.Errorf("SignalAdvisoryDBUnmaintained value: got %q, want RUSTSEC-2024-0375", advisoryDBValue)
	}
	if !hasSignal(res.Signals, SignalRepoArchived) {
		t.Errorf("expected the archived signal to be retained, got %+v", res.Signals)
	}
	if !hasSignal(res.Signals, SignalAdvisoryCount) {
		t.Errorf("expected the advisory signals to be collected, got %+v", res.Signals)
	}
}

// TestAnalysis_AdvisoryDBUnmaintained pins the nil-means-not-asked contract the
// assessor relies on.
func TestAnalysis_AdvisoryDBUnmaintained(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a    *Analysis
		want bool
	}{
		{"nil analysis", nil, false},
		{"not asked", &Analysis{}, false},
		{"asked, not flagged", &Analysis{AdvisoryDBState: &AdvisoryDBState{}}, false},
		{"flagged", &Analysis{AdvisoryDBState: &AdvisoryDBState{Unmaintained: true}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.a.AdvisoryDBUnmaintained(); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
