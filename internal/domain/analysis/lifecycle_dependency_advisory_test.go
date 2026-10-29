package analysis

import (
	"context"
	"testing"
	"time"

	cfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// TestLifecycleAssessor_DependencyAdvisories is the decision table for
// advisories against dependencies (Relation TRANSITIVE) that were not looked
// up in OSV: they never make a package EOL-Effective, and never move it to
// Active or Legacy-Safe either.
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
			wantTrace:  "scorecard_incomplete residual_vuln_dependency_not_checked",
		},
		{
			name:       "the same with a stable release in the last year: Stalled, not Active",
			analysis:   &Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(recentPublish, depHigh)},
			scores:     maintainedOnly(0),
			wantLabel:  LabelStalled,
			wantReason: "Unmaintained; HIGH advisories only in dependencies",
			wantTrace:  "scorecard_incomplete residual_vuln_dependency_not_checked",
		},
		{
			name:       "no Scorecard at all, only a dependency HIGH advisory: Stalled",
			analysis:   &Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(oldPublish, depHigh)},
			wantLabel:  LabelStalled,
			wantReason: "Unmaintained; HIGH advisories only in dependencies",
			wantTrace:  "scorecard_missing residual_vuln_dependency_not_checked",
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
			wantTrace:  "advisory_db_unmaintained_dependency_not_checked",
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
			wantTrace:  "inactive_no_commit_C2a_low_maint_advisory_dependency_not_checked",
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
			wantTrace:  "inactive_no_commit_C3a_advisory_dependency_not_checked",
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

// TestLifecycleAssessor_AgedDependencyAdvisory pins the aged-advisory rule: a HIGH
// dependency advisory public for longer than EolInactivityDays makes a dormant
// package EOL-Effective again, unless the database rates it below HIGH or
// withdrew it. Every other outcome is Stalled with its own reason, and the
// result names the deciding advisory. See ADR-0032.
func TestLifecycleAssessor_AgedDependencyAdvisory(t *testing.T) {
	t.Parallel()
	// 09:00 UTC is 18:00 in Tokyo: the day boundary must not depend on it.
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	rules := cfg.GetDefaultLifecycle()
	eol := rules.EolInactivityDays
	dormant := now.AddDate(0, 0, -(eol + 10))
	agedDate := now.AddDate(0, 0, -(eol + 1))
	olderDate := now.AddDate(0, 0, -(eol + 400))
	freshDate := now.AddDate(0, 0, -eol) // exactly EolInactivityDays: not aged yet

	dep := func(id string, score float64, lookup AdvisoryRecordLookup, published time.Time, rating string) Advisory {
		return Advisory{ID: id, Relation: AdvisoryRelationTransitive, CVSS3Score: score, DependencyName: "dep@1.0.0",
			RecordLookup: lookup, Published: published, ReviewedSeverity: rating}
	}
	aged := dep("GHSA-aged", 9.8, AdvisoryRecordFound, agedDate, "HIGH")
	older := dep("GHSA-older", 7.5, AdvisoryRecordFound, olderDate, "")
	fresh := dep("GHSA-fresh", 7.5, AdvisoryRecordFound, freshDate, "HIGH")
	agedModerate := dep("GHSA-w5hq-like", 7.5, AdvisoryRecordFound, agedDate, "MODERATE")
	agedOddRating := dep("GHSA-odd", 7.5, AdvisoryRecordFound, agedDate, "IMPORTANT")
	withdrawnOld := dep("GHSA-hp3w-like", 9.8, AdvisoryRecordWithdrawn, olderDate, "HIGH")
	failed := dep("GHSA-failed", 9.8, AdvisoryRecordLookupFailed, time.Time{}, "")
	notFound := dep("GHSA-missing", 9.8, AdvisoryRecordNotFound, time.Time{}, "")
	undated := dep("GHSA-undated", 9.8, AdvisoryRecordFound, time.Time{}, "")
	notLooked := dep("GHSA-not-looked", 9.8, AdvisoryRecordNotLookedUp, agedDate, "")
	agedMedium := dep("GHSA-aged-medium", 5.3, AdvisoryRecordFound, agedDate, "")

	dormantRepo := func() *RepoState {
		return &RepoState{DaysSinceLastCommit: eol + 10, LatestHumanCommit: &dormant, CommitStats: &CommitStats{}}
	}
	noCommitRepo := func() *RepoState { return &RepoState{DaysSinceLastCommit: 100} }
	stable := func(advs ...Advisory) *ReleaseInfo {
		return &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: dormant, Advisories: advs}}
	}
	maintained0 := map[string]*ScoreEntity{"Maintained": NewScoreEntity("Maintained", 0, 10, "")}
	residual := func(advs ...Advisory) *Analysis {
		return &Analysis{RepoState: dormantRepo(), ReleaseInfo: stable(advs...)}
	}
	day := func(t time.Time) string { return t.Format("2006-01-02") }

	tests := []struct {
		name       string
		analysis   *Analysis
		scores     map[string]*ScoreEntity
		wantLabel  MaintenanceStatus
		wantReason string
		wantTrace  string
		wantStatus DependencyAdvisoryAgeStatus
		wantID     string
	}{
		{"override: aged HIGH dependency advisory is EOL-Effective", residual(fresh, aged), maintained0,
			LabelEOLEffective, "Unmaintained; HIGH dependency advisory published " + day(agedDate) + ", over 730 days ago (GHSA-aged in dep@1.0.0)",
			"scorecard_incomplete residual_vuln_dependency_aged", DependencyAdvisoryAged, "GHSA-aged"},
		{"override: the oldest aged advisory is named", residual(aged, older), maintained0,
			LabelEOLEffective, "Unmaintained; HIGH dependency advisory published " + day(olderDate) + ", over 730 days ago (GHSA-older in dep@1.0.0)",
			"scorecard_incomplete residual_vuln_dependency_aged", DependencyAdvisoryAged, "GHSA-older"},
		{"override: exactly EolInactivityDays old stays Stalled and says when it counts", residual(fresh), maintained0,
			LabelStalled, "Unmaintained; HIGH advisories only in dependencies; oldest published " + day(freshDate) + ", counts from " + day(freshDate.AddDate(0, 0, eol+1)) + " (GHSA-fresh in dep@1.0.0)",
			"scorecard_incomplete residual_vuln_dependency_not_aged", DependencyAdvisoryNotAged, "GHSA-fresh"},
		{"override: rated MODERATE by the database stays Stalled (exceljs today)", residual(agedModerate), maintained0,
			LabelStalled, "Unmaintained; dependency advisories HIGH by CVSS3 only, rated MODERATE by the advisory database (GHSA-w5hq-like in dep@1.0.0)",
			"scorecard_incomplete residual_vuln_dependency_rated_below_high", DependencyAdvisoryRatedBelowHigh, "GHSA-w5hq-like"},
		{"override: an unrecognized rating is not called below HIGH", residual(agedOddRating), maintained0,
			LabelStalled, "Unmaintained; HIGH advisories only in dependencies; unrecognized advisory database rating (GHSA-odd in dep@1.0.0)",
			"scorecard_incomplete residual_vuln_dependency_rating_unrecognized", DependencyAdvisoryRatingUnrecognized, "GHSA-odd"},
		{"override: withdrawn and old never ages (json-refs)", residual(withdrawnOld), maintained0,
			LabelStalled, "Unmaintained; dependency advisory withdrawn by its database (GHSA-hp3w-like in dep@1.0.0)",
			"scorecard_incomplete residual_vuln_dependency_withdrawn", DependencyAdvisoryWithdrawn, "GHSA-hp3w-like"},
		{"override: a failed lookup is unknown, not called undated", residual(failed), maintained0,
			LabelStalled, "Unmaintained; HIGH advisories only in dependencies; age not verified, OSV lookup failed (GHSA-failed in dep@1.0.0)",
			"scorecard_incomplete residual_vuln_dependency_unknown", DependencyAdvisoryUnknown, "GHSA-failed"},
		{"override: one failed lookup next to an aged one stays EOL-Effective", residual(failed, aged), maintained0,
			LabelEOLEffective, "Unmaintained; HIGH dependency advisory published " + day(agedDate) + ", over 730 days ago (GHSA-aged in dep@1.0.0)",
			"scorecard_incomplete residual_vuln_dependency_aged", DependencyAdvisoryAged, "GHSA-aged"},
		{"override: a failed lookup outranks a not-aged one", residual(fresh, failed), maintained0,
			LabelStalled, "Unmaintained; HIGH advisories only in dependencies; age not verified, OSV lookup failed (GHSA-failed in dep@1.0.0)",
			"scorecard_incomplete residual_vuln_dependency_unknown", DependencyAdvisoryUnknown, "GHSA-failed"},
		{"override: not in OSV is undated", residual(notFound), maintained0,
			LabelStalled, "Unmaintained; HIGH advisories only in dependencies; no publication date in OSV (GHSA-missing in dep@1.0.0)",
			"scorecard_incomplete residual_vuln_dependency_undated", DependencyAdvisoryUndated, "GHSA-missing"},
		{"override: a record without a date is undated", residual(undated), maintained0,
			LabelStalled, "Unmaintained; HIGH advisories only in dependencies; no publication date in OSV (GHSA-undated in dep@1.0.0)",
			"scorecard_incomplete residual_vuln_dependency_undated", DependencyAdvisoryUndated, "GHSA-undated"},
		{"override: a date without a lookup never ages (hand-built library input)", residual(notLooked), maintained0,
			LabelStalled, "Unmaintained; HIGH advisories only in dependencies",
			"scorecard_incomplete residual_vuln_dependency_not_checked", DependencyAdvisoryNotChecked, "GHSA-not-looked"},
		{"override: aged MEDIUM next to a fresh HIGH stays Stalled", residual(fresh, agedMedium), maintained0,
			LabelStalled, "Unmaintained; HIGH advisories only in dependencies; oldest published " + day(freshDate) + ", counts from " + day(freshDate.AddDate(0, 0, eol+1)) + " (GHSA-fresh in dep@1.0.0)",
			"scorecard_incomplete residual_vuln_dependency_not_aged", DependencyAdvisoryNotAged, "GHSA-fresh"},
		{"RustSec marker: aged HIGH dependency advisory is EOL-Effective",
			&Analysis{RepoState: dormantRepo(), AdvisoryDBState: flagged("RUSTSEC-2020-0095", "difference is unmaintained"), ReleaseInfo: stable(aged)}, nil,
			LabelEOLEffective, "Flagged unmaintained by RUSTSEC-2020-0095: difference is unmaintained; HIGH dependency advisory published " + day(agedDate) + ", over 730 days ago (GHSA-aged in dep@1.0.0)",
			"advisory_db_unmaintained_dependency_aged", DependencyAdvisoryAged, "GHSA-aged"},
		{"RustSec marker: rated MODERATE stays Stalled",
			&Analysis{RepoState: dormantRepo(), AdvisoryDBState: flagged("RUSTSEC-2020-0095", "difference is unmaintained"), ReleaseInfo: stable(agedModerate)}, nil,
			LabelStalled, "Flagged unmaintained by RUSTSEC-2020-0095: difference is unmaintained; dependency advisories HIGH by CVSS3 only, rated MODERATE by the advisory database (GHSA-w5hq-like in dep@1.0.0)",
			"advisory_db_unmaintained_dependency_rated_below_high", DependencyAdvisoryRatedBelowHigh, "GHSA-w5hq-like"},
		{"no commit data, C2a: aged HIGH dependency advisory is EOL-Effective",
			&Analysis{RepoState: noCommitRepo(), ReleaseInfo: stable(aged)}, maintained0,
			LabelEOLEffective, "Unmaintained; HIGH dependency advisory published " + day(agedDate) + ", over 730 days ago (GHSA-aged in dep@1.0.0)",
			"inactive_no_commit_C2a_low_maint_advisory_dependency_aged", DependencyAdvisoryAged, "GHSA-aged"},
		{"no commit data, C2a: not aged stays Stalled",
			&Analysis{RepoState: noCommitRepo(), ReleaseInfo: stable(fresh)}, maintained0,
			LabelStalled, "Unmaintained; HIGH advisories only in dependencies; oldest published " + day(freshDate) + ", counts from " + day(freshDate.AddDate(0, 0, eol+1)) + " (GHSA-fresh in dep@1.0.0)",
			"inactive_no_commit_C2a_low_maint_advisory_dependency_not_aged", DependencyAdvisoryNotAged, "GHSA-fresh"},
		{"no commit data, C3a: aged HIGH dependency advisory is EOL-Effective",
			&Analysis{RepoState: noCommitRepo(), ReleaseInfo: stable(aged)}, nil,
			LabelEOLEffective, "No recent releases; HIGH dependency advisory published " + day(agedDate) + ", over 730 days ago (GHSA-aged in dep@1.0.0)",
			"inactive_no_commit_C3a_advisory_dependency_aged", DependencyAdvisoryAged, "GHSA-aged"},
		{"no commit data, C3a: a failed lookup is unknown",
			&Analysis{RepoState: noCommitRepo(), ReleaseInfo: stable(failed)}, nil,
			LabelStalled, "No recent releases; HIGH advisories only in dependencies; age not verified, OSV lookup failed (GHSA-failed in dep@1.0.0)",
			"inactive_no_commit_C3a_advisory_dependency_unknown", DependencyAdvisoryUnknown, "GHSA-failed"},
	}
	svc := NewLifecycleAssessorService()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := svc.Assess(context.Background(), AssessmentInput{Analysis: tt.analysis, Scores: tt.scores, EOL: EOLStatus{State: EOLNotEOL}, Now: now})
			if err != nil {
				t.Fatalf("Assess failed: %v", err)
			}
			if MaintenanceStatus(res.Label) != tt.wantLabel {
				t.Fatalf("Label: got %q, want %q (reason %q, trace %v)", res.Label, tt.wantLabel, res.Reason, res.Trace)
			}
			if res.Reason != tt.wantReason {
				t.Errorf("Reason:\n got %q\nwant %q", res.Reason, tt.wantReason)
			}
			if !containsString(res.Trace, tt.wantTrace) {
				t.Errorf("Trace: got %v, want it to contain %q", res.Trace, tt.wantTrace)
			}
			if res.DependencyAdvisoryAge == nil {
				t.Fatalf("DependencyAdvisoryAge: got nil, want status %q", tt.wantStatus)
			}
			if got := res.DependencyAdvisoryAge; got.Status != tt.wantStatus || got.AdvisoryID != tt.wantID {
				t.Errorf("DependencyAdvisoryAge: got %q %q, want %q %q", got.Status, got.AdvisoryID, tt.wantStatus, tt.wantID)
			}
			wantSignal := tt.wantLabel == LabelEOLEffective
			if got := hasSignal(res.Signals, SignalAgedDependencyAdvisory); got != wantSignal {
				t.Errorf("SignalAgedDependencyAdvisory present: got %v, want %v", got, wantSignal)
			}
		})
	}
}

// TestLifecycleAssessor_DependencyAdvisoryAgeOnlyOnDependencyPaths checks that
// the deciding-advisory field stays nil when the package's own advisories
// decide, so a failed lookup is never reported for those.
func TestLifecycleAssessor_DependencyAdvisoryAgeOnlyOnDependencyPaths(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	rules := cfg.GetDefaultLifecycle()
	dormant := now.AddDate(0, 0, -(rules.EolInactivityDays + 10))
	a := &Analysis{
		RepoState: &RepoState{DaysSinceLastCommit: rules.EolInactivityDays + 10, LatestHumanCommit: &dormant, CommitStats: &CommitStats{}},
		ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Version: "1.0.0", PublishedAt: dormant, Advisories: []Advisory{
			{ID: "GHSA-own", Relation: AdvisoryRelationDirect, CVSS3Score: 9.8},
			{ID: "GHSA-failed", Relation: AdvisoryRelationTransitive, CVSS3Score: 9.8, RecordLookup: AdvisoryRecordLookupFailed},
		}}},
	}
	res, err := NewLifecycleAssessorService().Assess(context.Background(), AssessmentInput{Analysis: a, EOL: EOLStatus{State: EOLNotEOL}, Now: now,
		Scores: map[string]*ScoreEntity{"Maintained": NewScoreEntity("Maintained", 0, 10, "")}})
	if err != nil {
		t.Fatalf("Assess failed: %v", err)
	}
	if res.Label != string(LabelEOLEffective) {
		t.Fatalf("Label: got %q, want EOL-Effective", res.Label)
	}
	if res.DependencyAdvisoryAge != nil {
		t.Errorf("DependencyAdvisoryAge: got %+v, want nil", res.DependencyAdvisoryAge)
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
