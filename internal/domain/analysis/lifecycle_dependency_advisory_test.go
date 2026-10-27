package analysis

import (
	"context"
	"testing"
	"time"

	cfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// TestLifecycleAssessor_DependencyAdvisories is the decision table for
// advisories against dependencies (Relation TRANSITIVE): they never make a
// package EOL-Effective, and never move it to Active or Legacy-Safe either.
// See ADR-0032.
func TestLifecycleAssessor_DependencyAdvisories(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rules := cfg.GetDefaultLifecycle()
	dormant := now.AddDate(0, 0, -(rules.EolInactivityDays + 10))
	oldPublish := now.AddDate(0, 0, -(rules.EolInactivityDays + 10))
	recentPublish := now.AddDate(0, 0, -10)

	depHigh := Advisory{ID: "GHSA-dep-high", Relation: AdvisoryRelationTransitive, CVSS3Score: 7.5}
	depUnknown := Advisory{ID: "GHSA-dep-unknown", Relation: AdvisoryRelationTransitive}
	ownHigh := Advisory{ID: "GHSA-own-high", Relation: AdvisoryRelationDirect, CVSS3Score: 9.8}
	ownLow := Advisory{ID: "GHSA-own-low", Relation: AdvisoryRelationDirect, CVSS3Score: 3.1}

	dormantRepo := func() *RepoState {
		return &RepoState{DaysSinceLastCommit: rules.EolInactivityDays + 10, LatestHumanCommit: &dormant, CommitStats: &CommitStats{}}
	}
	// noCommitRepo is the GITHUB_TOKEN-unset shape: no human-commit data.
	noCommitRepo := func() *RepoState { return &RepoState{DaysSinceLastCommit: 100} }
	stable := func(published time.Time, advs ...Advisory) *ReleaseInfo {
		return &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: published, Advisories: advs}}
	}
	// exceljs shape: Maintained 0, no Vulnerabilities check.
	maintainedOnly := func(v int) map[string]*ScoreEntity {
		return map[string]*ScoreEntity{"Maintained": NewScoreEntity("Maintained", v, 10, "")}
	}
	full := func(maintained, vulns int) map[string]*ScoreEntity {
		return map[string]*ScoreEntity{
			"Maintained":      NewScoreEntity("Maintained", maintained, 10, ""),
			"Vulnerabilities": NewScoreEntity("Vulnerabilities", vulns, 10, ""),
		}
	}

	tests := []struct {
		name       string
		analysis   *Analysis
		scores     map[string]*ScoreEntity
		wantLabel  MaintenanceStatus
		wantReason string
		wantTrace  string
	}{
		{
			name:       "only a dependency HIGH advisory, Maintained 0, no Vulnerabilities score, dormant: Stalled",
			analysis:   &Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(oldPublish, depHigh)},
			scores:     maintainedOnly(0),
			wantLabel:  LabelStalled,
			wantReason: "Unmaintained; HIGH advisories only in dependencies",
			wantTrace:  "scorecard_incomplete residual_vuln_dependency_only",
		},
		{
			name:       "the same with a stable release in the last year: Stalled, not Active",
			analysis:   &Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(recentPublish, depHigh)},
			scores:     maintainedOnly(0),
			wantLabel:  LabelStalled,
			wantReason: "Unmaintained; HIGH advisories only in dependencies",
			wantTrace:  "scorecard_incomplete residual_vuln_dependency_only",
		},
		{
			name:       "no Scorecard at all, only a dependency HIGH advisory: Stalled",
			analysis:   &Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(oldPublish, depHigh)},
			wantLabel:  LabelStalled,
			wantReason: "Unmaintained; HIGH advisories only in dependencies",
			wantTrace:  "scorecard_missing residual_vuln_dependency_only",
		},
		{
			name:       "own LOW advisory plus a dependency advisory without CVSS3: Stalled",
			analysis:   &Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(oldPublish, ownLow, depUnknown)},
			scores:     maintainedOnly(0),
			wantLabel:  LabelStalled,
			wantReason: "Unmaintained; dependency advisories of unknown severity",
			wantTrace:  "scorecard_incomplete residual_vuln_dependency_only",
		},
		{
			name:       "own HIGH advisory next to a dependency one: EOL-Effective (unchanged)",
			analysis:   &Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(oldPublish, ownHigh, depUnknown)},
			scores:     maintainedOnly(0),
			wantLabel:  LabelEOLEffective,
			wantReason: "Unmaintained, unpatched vulnerabilities",
			wantTrace:  "scorecard_incomplete residual_vuln_override",
		},
		{
			// Full Scorecard with a middling Vulnerabilities score skips the
			// override and both score branches, and must not reach the
			// advisoryCount == 0 Legacy-Safe exit.
			name:       "only a dependency advisory, full Scorecard: not Legacy-Safe",
			analysis:   &Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(oldPublish, depHigh)},
			scores:     full(0, 5),
			wantLabel:  LabelStalled,
			wantReason: "Low maintenance, long-term inactive",
		},
		{
			name:       "no advisories at all: Legacy-Safe (unchanged, ADR-0001)",
			analysis:   &Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(oldPublish)},
			scores:     full(0, 5),
			wantLabel:  LabelLegacySafe,
			wantReason: "Dormant but no known vulnerabilities",
			wantTrace:  "inactive_legacy_safe_no_advisories_dormant",
		},
		{
			name:       "Vulnerabilities score 0-2 still decides on all advisories (out of scope)",
			analysis:   &Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(oldPublish, depHigh)},
			scores:     full(0, 1),
			wantLabel:  LabelEOLEffective,
			wantReason: "Unmaintained, unpatched vulnerabilities",
		},
		{
			name:       "RustSec marker plus only a dependency HIGH advisory: Stalled",
			analysis:   &Analysis{RepoState: dormantRepo(), AdvisoryDBState: flagged("RUSTSEC-2024-0375", "atty is unmaintained"), ReleaseInfo: stable(oldPublish, depHigh)},
			wantLabel:  LabelStalled,
			wantReason: "Flagged unmaintained by RUSTSEC-2024-0375: atty is unmaintained; HIGH advisories only in dependencies",
			wantTrace:  "advisory_db_unmaintained_no_direct_high_severity",
		},
		{
			name:       "RustSec marker plus an own HIGH advisory: EOL-Effective",
			analysis:   &Analysis{RepoState: dormantRepo(), AdvisoryDBState: flagged("RUSTSEC-2024-0375", "atty is unmaintained"), ReleaseInfo: stable(oldPublish, ownHigh, depHigh)},
			wantLabel:  LabelEOLEffective,
			wantReason: "Unmaintained per RUSTSEC-2024-0375, unpatched vulnerabilities",
			wantTrace:  "advisory_db_unmaintained_unpatched_vulns",
		},
		{
			name:       "no commit data, C2a, only a dependency HIGH advisory: Stalled",
			analysis:   &Analysis{RepoState: noCommitRepo(), ReleaseInfo: stable(oldPublish, depHigh)},
			scores:     maintainedOnly(0),
			wantLabel:  LabelStalled,
			wantReason: "Unmaintained; HIGH advisories only in dependencies",
			wantTrace:  "inactive_no_commit_C2a_low_maint_advisory_no_direct_high_severity",
		},
		{
			name:       "no commit data, C2a, own HIGH advisory: EOL-Effective (unchanged)",
			analysis:   &Analysis{RepoState: noCommitRepo(), ReleaseInfo: stable(oldPublish, ownHigh)},
			scores:     maintainedOnly(0),
			wantLabel:  LabelEOLEffective,
			wantReason: "Unmaintained, unpatched vulnerabilities",
			wantTrace:  "inactive_no_commit_C2a_low_maint_advisory_old_publish",
		},
		{
			name:       "no commit data, C2a, own LOW advisory only: Stalled with a reason that says so",
			analysis:   &Analysis{RepoState: noCommitRepo(), ReleaseInfo: stable(oldPublish, ownLow)},
			scores:     maintainedOnly(0),
			wantLabel:  LabelStalled,
			wantReason: "Unmaintained; only lower-severity advisories",
			wantTrace:  "inactive_no_commit_C2a_low_maint_advisory_low_severity",
		},
		{
			name:       "no commit data, C3a, only a dependency HIGH advisory: Stalled",
			analysis:   &Analysis{RepoState: noCommitRepo(), ReleaseInfo: stable(oldPublish, depHigh)},
			wantLabel:  LabelStalled,
			wantReason: "No recent releases; HIGH advisories only in dependencies",
			wantTrace:  "inactive_no_commit_C3a_advisory_no_direct_high_severity",
		},
		{
			// A dependency's RustSec unmaintained marker has no CVSS3 score; the
			// reason must not call it HIGH.
			name:       "RustSec marker plus only an unrated dependency advisory: Stalled, not called HIGH",
			analysis:   &Analysis{RepoState: dormantRepo(), AdvisoryDBState: flagged("RUSTSEC-2025-0091", "`unic-utils` is unmaintained"), ReleaseInfo: stable(oldPublish, depUnknown)},
			wantLabel:  LabelStalled,
			wantReason: "Flagged unmaintained by RUSTSEC-2025-0091: `unic-utils` is unmaintained; dependency advisories of unknown severity",
			wantTrace:  "advisory_db_unmaintained_no_direct_high_severity",
		},
		{
			name:       "no commit data, C3a, only an unrated dependency advisory: Stalled, not called HIGH",
			analysis:   &Analysis{RepoState: noCommitRepo(), ReleaseInfo: stable(oldPublish, depUnknown)},
			wantLabel:  LabelStalled,
			wantReason: "No recent releases; dependency advisories of unknown severity",
			wantTrace:  "inactive_no_commit_C3a_advisory_no_direct_high_severity",
		},
		{
			name:       "no commit data, C3b, only a dependency advisory and a mid-age publish: Stalled, not Active",
			analysis:   &Analysis{RepoState: noCommitRepo(), ReleaseInfo: stable(now.AddDate(0, 0, -400), depHigh)},
			wantLabel:  LabelStalled,
			wantReason: "Unpatched vulnerabilities, recent publish",
			wantTrace:  "inactive_no_commit_C3b_advisory_recent_publish",
		},
	}

	svc := NewLifecycleAssessorService()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := svc.Assess(context.Background(), AssessmentInput{Analysis: tt.analysis, Scores: tt.scores, EOL: EOLStatus{State: EOLNotEOL}})
			if err != nil {
				t.Fatalf("Assess failed: %v", err)
			}
			if res == nil {
				t.Fatalf("expected a result")
			}
			if MaintenanceStatus(res.Label) != tt.wantLabel {
				t.Fatalf("Label: got %q, want %q (reason %q, trace %v)", res.Label, tt.wantLabel, res.Reason, res.Trace)
			}
			if res.Reason != tt.wantReason {
				t.Errorf("Reason: got %q, want %q", res.Reason, tt.wantReason)
			}
			if tt.wantTrace != "" && !containsString(res.Trace, tt.wantTrace) {
				t.Errorf("Trace: got %v, want it to contain %q", res.Trace, tt.wantTrace)
			}
		})
	}
}

// TestLifecycleAssessor_TransitiveAdvisorySignal pins the signal that shows a
// Stalled label is not ok because of dependency advisories.
func TestLifecycleAssessor_TransitiveAdvisorySignal(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rules := cfg.GetDefaultLifecycle()
	dormant := now.AddDate(0, 0, -(rules.EolInactivityDays + 10))
	a := &Analysis{
		RepoState: &RepoState{DaysSinceLastCommit: rules.EolInactivityDays + 10, LatestHumanCommit: &dormant, CommitStats: &CommitStats{}},
		ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "4.4.0", PublishedAt: dormant, Advisories: []Advisory{
			{ID: "GHSA-w5hq-g745-h8pq", Relation: AdvisoryRelationTransitive, CVSS3Score: 7.5},
		}}},
	}
	scores := map[string]*ScoreEntity{"Maintained": NewScoreEntity("Maintained", 0, 10, "")}
	res, err := NewLifecycleAssessorService().Assess(context.Background(), AssessmentInput{Analysis: a, Scores: scores, EOL: EOLStatus{State: EOLNotEOL}})
	if err != nil {
		t.Fatalf("Assess failed: %v", err)
	}
	want := map[string]string{SignalAdvisoryCount: "1", SignalTransitiveAdvisoryCount: "1", SignalMaxAdvisorySeverity: "HIGH 7.5"}
	for name, value := range want {
		found := false
		for _, s := range res.Signals {
			if s.Name == name {
				found = true
				if s.Value != value {
					t.Errorf("signal %s: got %q, want %q", name, s.Value, value)
				}
			}
		}
		if !found {
			t.Errorf("signal %s missing (signals %+v)", name, res.Signals)
		}
	}
}

// TestLifecycleAssessor_DependencyAdvisoriesThreshold covers a
// ResidualAdvisoryThreshold above 1, where own advisories severe enough for the
// override can still be too few: the reason must not claim the HIGH one is in
// a dependency.
func TestLifecycleAssessor_DependencyAdvisoriesThreshold(t *testing.T) {
	t.Parallel()
	now := time.Now()
	c := cfg.GetDefaultLifecycle()
	c.ResidualAdvisoryThreshold = 2
	dormant := now.AddDate(0, 0, -(c.EolInactivityDays + 10))
	tests := []struct {
		name       string
		advs       []Advisory
		wantLabel  MaintenanceStatus
		wantReason string
	}{
		{
			name: "one own HIGH and one dependency LOW: Stalled, own HIGH named as too few",
			advs: []Advisory{
				{ID: "GHSA-own-high", Relation: AdvisoryRelationDirect, CVSS3Score: 9.8},
				{ID: "GHSA-dep-low", Relation: AdvisoryRelationTransitive, CVSS3Score: 3.1},
			},
			wantLabel:  LabelStalled,
			wantReason: "Unmaintained; too few own advisories; dependency advisories reach the threshold",
		},
		{
			name: "two own HIGH: EOL-Effective",
			advs: []Advisory{
				{ID: "GHSA-own-1", Relation: AdvisoryRelationDirect, CVSS3Score: 9.8},
				{ID: "GHSA-own-2", Relation: AdvisoryRelationDirect, CVSS3Score: 7.5},
			},
			wantLabel:  LabelEOLEffective,
			wantReason: "Unmaintained, unpatched vulnerabilities",
		},
		{
			name: "two dependency HIGH: Stalled, HIGH only in dependencies",
			advs: []Advisory{
				{ID: "GHSA-dep-1", Relation: AdvisoryRelationTransitive, CVSS3Score: 9.8},
				{ID: "GHSA-dep-2", Relation: AdvisoryRelationTransitive, CVSS3Score: 7.5},
			},
			wantLabel:  LabelStalled,
			wantReason: "Unmaintained; HIGH advisories only in dependencies",
		},
	}
	svc := NewLifecycleAssessorServiceWithConfig(c)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &Analysis{
				RepoState:   &RepoState{DaysSinceLastCommit: c.EolInactivityDays + 10, LatestHumanCommit: &dormant, CommitStats: &CommitStats{}},
				ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: dormant, Advisories: tt.advs}},
			}
			scores := map[string]*ScoreEntity{"Maintained": NewScoreEntity("Maintained", 0, 10, "")}
			res, err := svc.Assess(context.Background(), AssessmentInput{Analysis: a, Scores: scores, EOL: EOLStatus{State: EOLNotEOL}})
			if err != nil {
				t.Fatalf("Assess failed: %v", err)
			}
			if MaintenanceStatus(res.Label) != tt.wantLabel {
				t.Fatalf("Label: got %q, want %q (reason %q)", res.Label, tt.wantLabel, res.Reason)
			}
			if res.Reason != tt.wantReason {
				t.Errorf("Reason: got %q, want %q", res.Reason, tt.wantReason)
			}
		})
	}
}

// TestLifecycleAssessor_AgedDependencyAdvisory pins option B of #591: a HIGH
// dependency advisory public for longer than EolInactivityDays makes a dormant
// package EOL-Effective again. See ADR-0032.
func TestLifecycleAssessor_AgedDependencyAdvisory(t *testing.T) {
	t.Parallel()
	now := time.Now()
	rules := cfg.GetDefaultLifecycle()
	dormant := now.AddDate(0, 0, -(rules.EolInactivityDays + 10))
	agedDate := now.AddDate(0, 0, -(rules.EolInactivityDays + 1))
	freshDate := now.AddDate(0, 0, -(rules.EolInactivityDays - 1))
	aged := Advisory{ID: "GHSA-aged", Relation: AdvisoryRelationTransitive, CVSS3Score: 9.8, DependencyName: "hoek@2.16.3", Published: agedDate}
	fresh := Advisory{ID: "GHSA-fresh", Relation: AdvisoryRelationTransitive, CVSS3Score: 7.5, DependencyName: "uuid@8.3.2", Published: freshDate}
	agedUnknown := Advisory{ID: "RUSTSEC-aged-marker", Relation: AdvisoryRelationTransitive, Published: agedDate}
	agedMedium := Advisory{ID: "GHSA-aged-medium", Relation: AdvisoryRelationTransitive, CVSS3Score: 5.3, Published: agedDate}
	undated := Advisory{ID: "GHSA-undated", Relation: AdvisoryRelationTransitive, CVSS3Score: 9.8}
	agedModerate := Advisory{ID: "GHSA-w5hq-like", Relation: AdvisoryRelationTransitive, CVSS3Score: 7.5, ReviewedSeverity: "MODERATE", Published: agedDate}
	agedReason := "HIGH dependency advisory unfixed since " + agedDate.Format("2006-01-02") + " (GHSA-aged in hoek@2.16.3)"

	dormantRepo := func() *RepoState {
		return &RepoState{DaysSinceLastCommit: rules.EolInactivityDays + 10, LatestHumanCommit: &dormant, CommitStats: &CommitStats{}}
	}
	noCommitRepo := func() *RepoState { return &RepoState{DaysSinceLastCommit: 100} }
	stable := func(advs ...Advisory) *ReleaseInfo {
		return &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: dormant, Advisories: advs}}
	}
	maintained0 := map[string]*ScoreEntity{"Maintained": NewScoreEntity("Maintained", 0, 10, "")}

	tests := []struct {
		name       string
		analysis   *Analysis
		scores     map[string]*ScoreEntity
		wantLabel  MaintenanceStatus
		wantReason string
		wantTrace  string
	}{
		{"override: aged HIGH dependency advisory is EOL-Effective",
			&Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(fresh, aged)}, maintained0,
			LabelEOLEffective, "Unmaintained; " + agedReason, "scorecard_incomplete residual_vuln_dependency_aged"},
		{"override: one day short of the period stays Stalled (exceljs today)",
			&Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(fresh)}, maintained0,
			LabelStalled, "Unmaintained; HIGH advisories only in dependencies", "scorecard_incomplete residual_vuln_dependency_only"},
		{"override: aged but unknown severity never qualifies",
			&Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(agedUnknown)}, maintained0,
			LabelStalled, "Unmaintained; dependency advisories of unknown severity", "scorecard_incomplete residual_vuln_dependency_only"},
		{"override: aged MEDIUM next to a fresh HIGH stays Stalled",
			&Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(fresh, agedMedium)}, maintained0,
			LabelStalled, "Unmaintained; HIGH advisories only in dependencies", "scorecard_incomplete residual_vuln_dependency_only"},
		{"override: CVSS3 HIGH but reviewed MODERATE never qualifies",
			&Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(agedModerate)}, maintained0,
			LabelStalled, "Unmaintained; HIGH advisories only in dependencies", "scorecard_incomplete residual_vuln_dependency_only"},
		{"override: HIGH with no publication date stays Stalled",
			&Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(undated)}, maintained0,
			LabelStalled, "Unmaintained; HIGH advisories only in dependencies", "scorecard_incomplete residual_vuln_dependency_only"},
		{"RustSec marker: aged HIGH dependency advisory is EOL-Effective",
			&Analysis{RepoState: dormantRepo(), AdvisoryDBState: flagged("RUSTSEC-2020-0095", "difference is unmaintained"), ReleaseInfo: stable(aged)}, nil,
			LabelEOLEffective, "Unmaintained per RUSTSEC-2020-0095; " + agedReason, "advisory_db_unmaintained_dependency_aged"},
		{"no commit data, C2a: aged HIGH dependency advisory is EOL-Effective",
			&Analysis{RepoState: noCommitRepo(), ReleaseInfo: stable(aged)}, maintained0,
			LabelEOLEffective, "Unmaintained; " + agedReason, "inactive_no_commit_C2a_low_maint_advisory_dependency_aged"},
		{"no commit data, C3a: aged HIGH dependency advisory is EOL-Effective",
			&Analysis{RepoState: noCommitRepo(), ReleaseInfo: stable(aged)}, nil,
			LabelEOLEffective, "No recent releases; " + agedReason, "inactive_no_commit_C3a_advisory_dependency_aged"},
	}
	svc := NewLifecycleAssessorService()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := svc.Assess(context.Background(), AssessmentInput{Analysis: tt.analysis, Scores: tt.scores, EOL: EOLStatus{State: EOLNotEOL}})
			if err != nil {
				t.Fatalf("Assess failed: %v", err)
			}
			if MaintenanceStatus(res.Label) != tt.wantLabel {
				t.Fatalf("Label: got %q, want %q (reason %q, trace %v)", res.Label, tt.wantLabel, res.Reason, res.Trace)
			}
			if res.Reason != tt.wantReason {
				t.Errorf("Reason: got %q, want %q", res.Reason, tt.wantReason)
			}
			if !containsString(res.Trace, tt.wantTrace) {
				t.Errorf("Trace: got %v, want it to contain %q", res.Trace, tt.wantTrace)
			}
			wantSignal := tt.wantLabel == LabelEOLEffective
			if got := hasSignal(res.Signals, SignalAgedDependencyAdvisory); got != wantSignal {
				t.Errorf("SignalAgedDependencyAdvisory present: got %v, want %v", got, wantSignal)
			}
		})
	}
}

func TestVersionDetail_DirectOnly(t *testing.T) {
	t.Parallel()
	var nilVD *VersionDetail
	if nilVD.DirectOnly() != nil {
		t.Fatalf("nil receiver: want nil")
	}
	vd := &VersionDetail{Version: "1.0.0", Advisories: []Advisory{
		{ID: "A", Relation: AdvisoryRelationDirect},
		{ID: "B", Relation: AdvisoryRelationTransitive},
		{ID: "C"},
	}}
	got := vd.DirectOnly()
	if len(got.Advisories) != 2 || got.Advisories[0].ID != "A" || got.Advisories[1].ID != "C" {
		t.Fatalf("DirectOnly: got %+v, want A and C", got.Advisories)
	}
	if got.Version != "1.0.0" {
		t.Errorf("Version: got %q, want 1.0.0", got.Version)
	}
	if len(vd.Advisories) != 3 {
		t.Errorf("receiver mutated: %+v", vd.Advisories)
	}
}
