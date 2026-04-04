// Package analysis implements lifecycle assessment domain service logic.
package analysis

import (
	"context"
	"fmt"
	"strings"

	cfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// LifecycleAssessmentRules holds runtime thresholds (config-driven) for lifecycle assessment decisions.
type LifecycleAssessmentRules struct {
	RecentStableWindowDays     int
	RecentPrereleaseWindowDays int
	MaxHumanCommitGapDays      int
	LegacyFrozenYears          int
	EolInactivityDays          int
	MaintenanceScoreMin        float64
	VulnerabilityScoreGoodMin  float64
	VulnerabilityScorePoorMax  float64
	ResidualAdvisoryThreshold  int
	HighSeverityCVSSThreshold  float64
}

// LifecycleAssessorService implements lifecycle/activity assessment logic.
// Focuses on repository lifecycle signals (activity, maintenance, vulnerability residuals, EOL overrides).
type LifecycleAssessorService struct{ rules LifecycleAssessmentRules }

// NewLifecycleAssessorService creates a new lifecycle assessor service with normalized default config.
func NewLifecycleAssessorService() *LifecycleAssessorService {
	c := cfg.GetDefaultLifecycle()
	cfg.NormalizeLifecycleConfig(&c)
	return NewLifecycleAssessorServiceWithConfig(c)
}

// NewLifecycleAssessorServiceWithConfig creates a new assessor service using injected LifecycleAssessmentConfig.
func NewLifecycleAssessorServiceWithConfig(c cfg.LifecycleAssessmentConfig) *LifecycleAssessorService {
	cfg.NormalizeLifecycleConfig(&c)
	rules := LifecycleAssessmentRules{
		RecentStableWindowDays:     c.RecentStableWindowDays,
		RecentPrereleaseWindowDays: c.RecentPrereleaseWindowDays,
		MaxHumanCommitGapDays:      c.MaxHumanCommitGapDays,
		LegacyFrozenYears:          c.LegacyFrozenYears,
		EolInactivityDays:          c.EolInactivityDays,
		MaintenanceScoreMin:        c.MaintenanceScoreMin,
		VulnerabilityScoreGoodMin:  c.VulnerabilityScoreGoodMin,
		VulnerabilityScorePoorMax:  c.VulnerabilityScorePoorMax,
		ResidualAdvisoryThreshold:  c.ResidualAdvisoryThreshold,
		HighSeverityCVSSThreshold:  c.HighSeverityCVSSThreshold,
	}
	return &LifecycleAssessorService{rules: rules}
}

// Assess performs lifecycle assessment and returns an AssessmentResult using the lifecycle decision tree logic.
func (s *LifecycleAssessorService) Assess(ctx context.Context, in AssessmentInput) (*AssessmentResult, error) {
	return s.assessInternal(ctx, in)
}

// sig creates a Signal with Role=SignalUsed.
func sig(name, value string) Signal { return Signal{Name: name, Value: value, Role: SignalUsed} }

// sigAbsent creates a Signal with Role=SignalAbsent.
func sigAbsent(name string) Signal { return Signal{Name: name, Role: SignalAbsent} }

// assessInternal contains the decision tree producing an AssessmentResult for the lifecycle axis with trace.
func (s *LifecycleAssessorService) assessInternal(ctx context.Context, in AssessmentInput) (*AssessmentResult, error) {
	analysis := in.Analysis
	scores := in.Scores
	trace := []string{"start lifecycle assessment"}
	// 0. Scheduled EOL (advance notice)
	if in.EOL.IsPlannedEOL() {
		reason := "Scheduled EOL"
		signals := []Signal{sig(SignalEOLSource, "scheduled")}
		if in.EOL.ScheduledAt != nil {
			reason = "Scheduled EOL"
			signals = append(signals, sig(SignalEOLScheduledDate, in.EOL.ScheduledAt.Format("2006-01-02")))
		}
		if in.EOL.Successor != "" {
			reason += "; successor: " + in.EOL.Successor
		}
		trace = append(trace, "planned_eol override")
		return &AssessmentResult{Axis: LifecycleAxis, Label: LabelEOLScheduled, Reason: reason, Trace: trace, Signals: signals}, nil
	}
	// 1. Archive/disable check
	if analysis != nil && (analysis.IsArchived() || analysis.IsDisabled()) {
		signals := []Signal{sig(SignalRepoArchived, fmt.Sprintf("%v", analysis.IsArchived()))}
		if analysis.IsDisabled() {
			signals = append(signals, sig(SignalRepoDisabled, "true"))
		}
		trace = append(trace, "repo archived_or_disabled")
		return &AssessmentResult{Axis: LifecycleAxis, Label: LabelEOLConfirmed, Reason: "Repository archived or disabled", Trace: trace, Signals: signals}, nil
	}

	// 1.5 Primary-source EOL status override
	if in.EOL.IsEOL() {
		reason := in.EOL.FinalReason()
		if reason == "" {
			reason = "End of life"
		}
		if in.EOL.Successor != "" {
			reason += "; successor: " + in.EOL.Successor
		}
		signalSource := eolEvidenceSource(in.EOL)
		trace = append(trace, "primary_source_eol override")
		return &AssessmentResult{Axis: LifecycleAxis, Label: LabelEOLConfirmed, Reason: reason, Trace: trace, Signals: []Signal{sig(SignalEOLSource, signalSource)}}, nil
	}

	// 2. Data validity check — residual vulnerability override
	if len(scores) == 0 {
		if analysis != nil && s.shouldOverrideToEOLDueToResidualVulns(analysis) {
			trace = append(trace, "scorecard_missing residual_vuln_override")
			signals := []Signal{commitSignal(analysis), sigAbsent(SignalMaintainedScore)}
			signals = append(signals, s.collectAdvisorySignals(analysis)...)
			return &AssessmentResult{Axis: LifecycleAxis, Label: LabelEOLEffective,
				Reason: "Unmaintained with unpatched vulnerabilities", Trace: trace, Signals: signals}, nil
		}
	}

	maintainedScore := s.getScoreValue(scores, "Maintained")
	vulnScore := s.getScoreValue(scores, "Vulnerabilities")

	if maintainedScore < 0 || vulnScore < 0 {
		if analysis != nil && s.shouldOverrideToEOLDueToResidualVulns(analysis) {
			trace = append(trace, "scorecard_incomplete residual_vuln_override")
			signals := []Signal{commitSignal(analysis), maintainedSignal(scores)}
			signals = append(signals, s.collectAdvisorySignals(analysis)...)
			return &AssessmentResult{Axis: LifecycleAxis, Label: LabelEOLEffective,
				Reason: "Unmaintained with unpatched vulnerabilities", Trace: trace, Signals: signals}, nil
		}
	}

	// 3. Activity level determination
	if analysis != nil {
		hasRecentStable := analysis.HasRecentStableRelease(s.rules.RecentStableWindowDays)
		hasRecentPrerelease := analysis.HasRecentPrereleaseRelease(s.rules.RecentPrereleaseWindowDays)
		hasRecentHumanCommit := analysis.HasRecentHumanCommit(s.rules.MaxHumanCommitGapDays)

		if hasRecentStable || hasRecentPrerelease || hasRecentHumanCommit {
			trace = append(trace, "active_path")
			res, _ := s.assessActiveState(analysis, scores)
			if res != nil {
				res.Trace = append(trace, res.Trace...)
			}
			return res, nil
		}

		// 3.5. Commit data validity check
		threshold := s.rules.RecentStableWindowDays * s.rules.LegacyFrozenYears * 10
		if analysis.GetDaysSinceLastCommit() >= threshold {
			trace = append(trace, "commit_data_missing_threshold")
			return &AssessmentResult{Axis: LifecycleAxis, Label: LabelReviewNeeded, Reason: "Commit data missing", Trace: trace, Signals: []Signal{sigAbsent(SignalLastHumanCommit)}}, nil
		}

		// 4. Detailed lifecycle classification of inactive state
		trace = append(trace, "inactive_path")
		res, _ := s.assessInactiveState(analysis, scores)
		if res != nil {
			res.Trace = append(trace, res.Trace...)
		}
		return res, nil
	}

	// Fallback when no analysis data available
	trace = append(trace, "no_analysis_data")
	return &AssessmentResult{Axis: LifecycleAxis, Label: LabelReviewNeeded, Reason: "Insufficient data for assessment", Trace: trace, Signals: []Signal{sigAbsent(SignalLastHumanCommit), sigAbsent(SignalMaintainedScore)}}, nil
}

// shouldOverrideToEOLDueToResidualVulns returns true when Scorecard data is missing/incomplete
// AND we have evidence of long-term dormancy plus unresolved advisories on the
// latest stable version (or MaxSemver fallback when stable is absent).
// Conditions:
// - analysis not nil
// - Days since last human commit > EolDays
// - advisory count >= ResidualAdvisoryThreshold
func (s *LifecycleAssessorService) shouldOverrideToEOLDueToResidualVulns(a *Analysis) bool {
	if a == nil || a.RepoState == nil {
		return false
	}
	// Require actual commit data to prove dormancy. When LatestHumanCommit is nil
	// (e.g., no GITHUB_TOKEN), GetDaysSinceLastHumanCommit returns 9999 which
	// would falsely satisfy the dormancy threshold — that is absence of evidence,
	// not evidence of inactivity.
	if a.RepoState.LatestHumanCommit == nil {
		return false
	}
	// Commit dormancy check
	if a.GetDaysSinceLastHumanCommit() <= s.rules.EolInactivityDays {
		return false
	}
	count, _ := s.getStableOrMaxAdvisory(a)
	if count < s.rules.ResidualAdvisoryThreshold || count == 0 {
		return false
	}
	// Only use severity-based override logic when all advisories have known severity.
	// If any advisory severity is unknown, fall back to the existing count-based logic
	// because unknown advisories may still be HIGH/CRITICAL.
	vd := s.getStableOrMaxVersionDetail(a)
	if vd != nil && vd.UnknownSeverityAdvisoryCount() == 0 {
		return vd.HighSeverityAdvisoryCount(s.rules.HighSeverityCVSSThreshold) > 0
	}
	return true
}

// getStableOrMaxAdvisory returns the advisory count and slice choosing Stable over MaxSemver fallback.
// Order: Stable > MaxSemver > PreRelease > Requested (mirrors ReleaseInfo.LatestAdvisories logic
// but we explicitly only use Stable or MaxSemver for override rationale text).
func (s *LifecycleAssessorService) getStableOrMaxAdvisory(a *Analysis) (int, []Advisory) {
	if a == nil || a.ReleaseInfo == nil {
		return 0, nil
	}
	if a.ReleaseInfo.StableVersion != nil {
		return len(a.ReleaseInfo.StableVersion.Advisories), a.ReleaseInfo.StableVersion.Advisories
	}
	if a.ReleaseInfo.MaxSemverVersion != nil {
		return len(a.ReleaseInfo.MaxSemverVersion.Advisories), a.ReleaseInfo.MaxSemverVersion.Advisories
	}
	return 0, nil
}

// getStableOrMaxVersionDetail returns the VersionDetail used for advisory analysis,
// choosing Stable over MaxSemver fallback.
func (s *LifecycleAssessorService) getStableOrMaxVersionDetail(a *Analysis) *VersionDetail {
	if a == nil || a.ReleaseInfo == nil {
		return nil
	}
	if a.ReleaseInfo.StableVersion != nil {
		return a.ReleaseInfo.StableVersion
	}
	return a.ReleaseInfo.MaxSemverVersion
}

// hasHighSeverityAdvisories returns true if the analysis has any advisory with CVSS3 >= threshold,
// or if any advisory severity is unavailable and advisories exist (conservative fallback).
func (s *LifecycleAssessorService) hasHighSeverityAdvisories(a *Analysis) bool {
	vd := s.getStableOrMaxVersionDetail(a)
	if vd == nil || len(vd.Advisories) == 0 {
		return false
	}
	unknownCount := vd.UnknownSeverityAdvisoryCount()
	if unknownCount > 0 {
		// Any unknown severity triggers conservative fallback (treated as potentially high).
		return true
	}
	return vd.HighSeverityAdvisoryCount(s.rules.HighSeverityCVSSThreshold) > 0
}

// severityAwareLabel returns the appropriate label and trace based on whether HIGH+ advisories exist.
// When severity data shows only LOW/MEDIUM, the lowLabel is used instead of highLabel.
func (s *LifecycleAssessorService) severityAwareLabel(hasHigh bool,
	highLabel MaintenanceStatus, highTrace string, lowLabel MaintenanceStatus, lowTrace string,
) (MaintenanceStatus, string) {
	if hasHigh {
		return highLabel, highTrace
	}
	return lowLabel, lowTrace
}

// assessActiveState handles active repository states using domain models
func (s *LifecycleAssessorService) assessActiveState(analysis *Analysis, scores map[string]*ScoreEntity) (*AssessmentResult, error) {
	hasRecentStable := analysis.HasRecentStableRelease(s.rules.RecentStableWindowDays)
	hasRecentPrerelease := analysis.HasRecentPrereleaseRelease(s.rules.RecentPrereleaseWindowDays)
	isMaintenanceOk := analysis.IsMaintenanceOk()

	// A recent stable/prerelease publish is the strongest activity signal.
	if hasRecentStable {
		signals := []Signal{sig(SignalRecentStableRelease, "true"), commitSignal(analysis), maintainedSignal(scores)}
		return &AssessmentResult{Axis: LifecycleAxis, Label: LabelActive, Reason: "Actively maintained with recent releases", Trace: []string{"active_stable"}, Signals: signals}, nil
	} else if hasRecentPrerelease {
		signals := []Signal{sig(SignalRecentPreRelease, "true"), commitSignal(analysis), maintainedSignal(scores)}
		return &AssessmentResult{Axis: LifecycleAxis, Label: LabelActive, Reason: "Actively maintained with recent pre-release", Trace: []string{"active_prerelease"}, Signals: signals}, nil
	} else { // hasRecentCommit only
		if analysis.IsVCSDirectDelivery() {
			signals := []Signal{commitSignal(analysis), sig(SignalEcosystemDelivery, "vcs-direct"), maintainedSignal(scores)}
			return &AssessmentResult{Axis: LifecycleAxis, Label: LabelActive, Reason: "Active commits (VCS-direct delivery)", Trace: []string{"active_commits_only_vcs_direct"}, Signals: signals}, nil
		}
		hasMaintainedScore := s.getScoreValue(scores, "Maintained") >= 0
		if isMaintenanceOk {
			return &AssessmentResult{Axis: LifecycleAxis, Label: LabelActive, Reason: "Active commits, no recent publish", Trace: []string{"active_commits_only_maintenance_ok"}, Signals: []Signal{commitSignal(analysis), maintainedSignal(scores)}}, nil
		}
		if !hasMaintainedScore {
			return &AssessmentResult{Axis: LifecycleAxis, Label: LabelActive, Reason: "Active commits, scorecard unavailable", Trace: []string{"active_commits_only_maintenance_unknown"}, Signals: []Signal{commitSignal(analysis), sigAbsent(SignalMaintainedScore)}}, nil
		}
		return &AssessmentResult{Axis: LifecycleAxis, Label: LabelStalled, Reason: "Active commits but low maintenance", Trace: []string{"active_commits_only_maintenance_low"}, Signals: []Signal{commitSignal(analysis), maintainedSignal(scores)}}, nil
	}
}

// assessInactiveState performs detailed lifecycle classification of inactive states using domain models.
// The function branches on HasCommitData() to prevent sentinel values (9999/999.0) from
// GetDaysSinceLastHumanCommit/GetLastHumanCommitYears leaking into commit-based comparisons
// when GITHUB_TOKEN is absent.
func (s *LifecycleAssessorService) assessInactiveState(analysis *Analysis, scores map[string]*ScoreEntity) (*AssessmentResult, error) {
	vulnScore := s.getScoreValue(scores, "Vulnerabilities")
	maintainedScore := s.getScoreValue(scores, "Maintained")
	hasVulnScore := vulnScore >= 0
	hasMaintainedScore := maintainedScore >= 0
	isMaintenanceOk := analysis.IsMaintenanceOk()
	// Note: Primary-source EOL is handled at entry (in.EOL). Do not re-evaluate here to avoid duplication.

	// ── Path A: Commit data available (GITHUB_TOKEN set) ──
	if analysis.HasCommitData() {
		daysSinceLastHumanCommit := analysis.GetDaysSinceLastHumanCommit()
		lastHumanCommitYears := analysis.GetLastHumanCommitYears()
		cSig := commitSignal(analysis)
		mSig := maintainedSignal(scores)

		// High vulnerability score (≥8): prioritize safety classification
		if hasVulnScore && vulnScore >= s.rules.VulnerabilityScoreGoodMin {
			advSignals := s.collectAdvisorySignals(analysis)
			if lastHumanCommitYears >= float64(s.rules.LegacyFrozenYears) {
				signals := append([]Signal{cSig}, advSignals...)
				return &AssessmentResult{Axis: LifecycleAxis, Label: LabelLegacySafe, Reason: "Dormant but no known vulnerabilities", Trace: []string{"inactive_legacy_safe_vuln_score_high"}, Signals: signals}, nil
			}
			signals := append([]Signal{cSig}, advSignals...)
			return &AssessmentResult{Axis: LifecycleAxis, Label: LabelStalled, Reason: "Low vulnerability risk but inactive", Trace: []string{"inactive_stalled_vuln_score_high_recent"}, Signals: signals}, nil
		}

		advisoryCount, _ := s.getStableOrMaxAdvisory(analysis)
		if advisoryCount == 0 && daysSinceLastHumanCommit > s.rules.EolInactivityDays {
			return &AssessmentResult{Axis: LifecycleAxis, Label: LabelLegacySafe,
				Reason:  "Dormant but no known vulnerabilities",
				Trace:   []string{"inactive_legacy_safe_no_advisories_dormant"},
				Signals: []Signal{cSig, sig(SignalAdvisoryCount, "0")}}, nil
		}

		// Low maintenance score (<3): branch based on EOL_DAYS (2 years)
		if hasMaintainedScore && !isMaintenanceOk {
			if daysSinceLastHumanCommit > s.rules.EolInactivityDays {
				if hasVulnScore && vulnScore < s.rules.VulnerabilityScorePoorMax {
					signals := []Signal{cSig, mSig}
					signals = append(signals, s.collectAdvisorySignals(analysis)...)
					return &AssessmentResult{Axis: LifecycleAxis, Label: LabelEOLEffective, Reason: "Unmaintained with unpatched vulnerabilities", Signals: signals}, nil
				}
				return &AssessmentResult{Axis: LifecycleAxis, Label: LabelStalled, Reason: "Low maintenance, long-term inactive", Signals: []Signal{cSig, mSig}}, nil
			}
			return &AssessmentResult{Axis: LifecycleAxis, Label: LabelStalled, Reason: "Low maintenance", Signals: []Signal{cSig, mSig}}, nil
		}

		if hasMaintainedScore && isMaintenanceOk {
			return &AssessmentResult{Axis: LifecycleAxis, Label: LabelStalled,
				Reason:  "Maintained but no recent activity",
				Trace:   []string{"inactive_commit_maintenance_ok_partial_scores"},
				Signals: []Signal{cSig, mSig}}, nil
		}
		if analysis.HasPublishData() {
			daysSincePublish := analysis.GetDaysSinceLatestPublish()
			publishSig := sig(SignalDaysSinceRelease, fmt.Sprintf("%d", daysSincePublish))
			if daysSincePublish <= s.rules.EolInactivityDays {
				return &AssessmentResult{Axis: LifecycleAxis, Label: LabelStalled,
					Reason:  "No recent activity, recent publish",
					Trace:   []string{"inactive_commit_no_scores_recent_publish"},
					Signals: []Signal{cSig, publishSig}}, nil
			}
			if daysSinceLastHumanCommit > s.rules.EolInactivityDays && daysSincePublish > s.rules.EolInactivityDays {
				return &AssessmentResult{Axis: LifecycleAxis, Label: LabelStalled,
					Reason:  "Long-term inactive, no recent releases",
					Trace:   []string{"inactive_commit_no_scores_old_publish"},
					Signals: []Signal{cSig, publishSig}}, nil
			}
		}
		if !analysis.HasPublishData() && !hasMaintainedScore {
			return &AssessmentResult{Axis: LifecycleAxis, Label: LabelStalled,
				Reason:  "No recent activity, no registry data",
				Trace:   []string{"inactive_github_only_stalled"},
				Signals: []Signal{cSig, sigAbsent(SignalMaintainedScore)}}, nil
		}
		return &AssessmentResult{Axis: LifecycleAxis, Label: LabelReviewNeeded, Reason: "Insufficient data for assessment", Trace: []string{"inactive_commit_data_scores_inconclusive"}, Signals: []Signal{cSig, mSig}}, nil
	}

	// ── Path B: No commit data (no GITHUB_TOKEN) ──
	return s.assessInactiveNoCommitData(analysis, scores, maintainedScore, hasMaintainedScore, isMaintenanceOk)
}

// assessInactiveNoCommitData classifies inactive packages when commit data is unavailable.
// Decision tree (C1-C3) uses scorecard, publish recency, and advisory data from deps.dev.
func (s *LifecycleAssessorService) assessInactiveNoCommitData(
	analysis *Analysis,
	scores map[string]*ScoreEntity,
	maintainedScore float64,
	hasMaintainedScore, isMaintenanceOk bool,
) (*AssessmentResult, error) {
	daysSincePublish := analysis.GetDaysSinceLatestPublish()
	advisoryCount, _ := s.getStableOrMaxAdvisory(analysis)
	hasAdvisories := advisoryCount >= s.rules.ResidualAdvisoryThreshold && advisoryCount > 0
	hasHighSeverity := s.hasHighSeverityAdvisories(analysis)
	cSig := sigAbsent(SignalLastHumanCommit) // no commit data in this path
	mSig := maintainedSignal(scores)
	dSig := sigAbsent(SignalDaysSinceRelease)
	if analysis.HasPublishData() {
		dSig = sig(SignalDaysSinceRelease, fmt.Sprintf("%d", daysSincePublish))
	}

	// C1: Scorecard Maintained ≥ threshold
	if hasMaintainedScore && isMaintenanceOk {
		return &AssessmentResult{Axis: LifecycleAxis, Label: LabelStalled,
			Reason:  "Maintained but no commit data",
			Trace:   []string{"inactive_no_commit_C1_maintenance_ok"},
			Signals: []Signal{cSig, mSig}}, nil
	}

	// C2: Scorecard Maintained < threshold
	if hasMaintainedScore && !isMaintenanceOk {
		if hasAdvisories && analysis.HasPublishData() && daysSincePublish > s.rules.EolInactivityDays {
			label, trace := s.severityAwareLabel(hasHighSeverity,
				LabelEOLEffective, "inactive_no_commit_C2a_low_maint_advisory_old_publish",
				LabelStalled, "inactive_no_commit_C2a_low_maint_advisory_low_severity")
			signals := append([]Signal{cSig, mSig, dSig}, s.collectAdvisorySignals(analysis)...)
			return &AssessmentResult{Axis: LifecycleAxis, Label: label,
				Reason:  "Unmaintained with unpatched vulnerabilities",
				Trace:   []string{trace},
				Signals: signals}, nil
		}
		return &AssessmentResult{Axis: LifecycleAxis, Label: LabelStalled,
			Reason:  "Low maintenance, no commit data",
			Trace:   []string{"inactive_no_commit_C2b_low_maint"},
			Signals: []Signal{cSig, mSig}}, nil
	}

	// C3: No scorecard — deps.dev signals only
	if hasAdvisories {
		if analysis.HasPublishData() && daysSincePublish > s.rules.EolInactivityDays {
			label, trace := s.severityAwareLabel(hasHighSeverity,
				LabelEOLEffective, "inactive_no_commit_C3a_advisory_old_publish",
				LabelStalled, "inactive_no_commit_C3a_advisory_low_severity")
			signals := append([]Signal{cSig, mSig, dSig}, s.collectAdvisorySignals(analysis)...)
			return &AssessmentResult{Axis: LifecycleAxis, Label: label,
				Reason:  "Unpatched vulnerabilities, no recent releases",
				Trace:   []string{trace},
				Signals: signals}, nil
		}
		signals := append([]Signal{cSig, mSig, dSig}, s.collectAdvisorySignals(analysis)...)
		return &AssessmentResult{Axis: LifecycleAxis, Label: LabelStalled,
			Reason:  "Unpatched vulnerabilities despite recent publish",
			Trace:   []string{"inactive_no_commit_C3b_advisory_recent_publish"},
			Signals: signals}, nil
	}

	// No advisories path — only use publish-age thresholds when publish data exists
	if analysis.HasPublishData() {
		if daysSincePublish <= s.rules.RecentStableWindowDays {
			return &AssessmentResult{Axis: LifecycleAxis, Label: LabelActive,
				Reason:  "Recently published, no known vulnerabilities",
				Trace:   []string{"inactive_no_commit_C3c_no_advisory_recent_publish"},
				Signals: []Signal{dSig, sig(SignalAdvisoryCount, "0")}}, nil
		}
		if daysSincePublish <= s.rules.EolInactivityDays {
			return &AssessmentResult{Axis: LifecycleAxis, Label: LabelStalled,
				Reason:  "No vulnerabilities but no recent releases",
				Trace:   []string{"inactive_no_commit_C3d_no_advisory_mid_publish"},
				Signals: []Signal{dSig, sig(SignalAdvisoryCount, "0")}}, nil
		}
		return &AssessmentResult{Axis: LifecycleAxis, Label: LabelLegacySafe,
			Reason:  "Dormant but no known vulnerabilities",
			Trace:   []string{"inactive_no_commit_C3e_no_advisory_old_publish"},
			Signals: []Signal{dSig, sig(SignalAdvisoryCount, "0")}}, nil
	}

	return &AssessmentResult{Axis: LifecycleAxis, Label: LabelReviewNeeded, Reason: "Insufficient data for assessment", Trace: []string{"inactive_no_commit_fallback_review_needed"}, Signals: []Signal{cSig, mSig}}, nil
}

// collectAdvisorySignals returns advisory-related signals for the analysis.
func (s *LifecycleAssessorService) collectAdvisorySignals(a *Analysis) []Signal {
	count, _ := s.getStableOrMaxAdvisory(a)
	if count == 0 {
		return []Signal{sig(SignalAdvisoryCount, "0")}
	}
	signals := []Signal{sig(SignalAdvisoryCount, fmt.Sprintf("%d", count))}
	vd := s.getStableOrMaxVersionDetail(a)
	if vd != nil {
		maxScore := vd.MaxCVSS3()
		if maxScore > 0 {
			signals = append(signals, sig(SignalMaxAdvisorySeverity, fmt.Sprintf("%s %.1f", SeverityFromCVSS3(maxScore), maxScore)))
		}
	}
	return signals
}

// commitSignal returns a signal for last human commit date.
func commitSignal(a *Analysis) Signal {
	if a != nil && a.HasCommitData() && a.RepoState != nil && a.RepoState.LatestHumanCommit != nil {
		return sig(SignalLastHumanCommit, a.RepoState.LatestHumanCommit.Format("2006-01-02"))
	}
	return sigAbsent(SignalLastHumanCommit)
}

// maintainedSignal returns a signal for the Maintained scorecard score.
// Returns absent when score is missing or negative (not available).
func maintainedSignal(scores map[string]*ScoreEntity) Signal {
	if score, ok := scores["Maintained"]; ok && score != nil && score.Value() >= 0 {
		return sig(SignalMaintainedScore, fmt.Sprintf("%d/10", score.Value()))
	}
	return sigAbsent(SignalMaintainedScore)
}

// getScoreValue safely gets a score value by name
func (s *LifecycleAssessorService) getScoreValue(scores map[string]*ScoreEntity, name string) float64 {
	if score, exists := scores[name]; exists && score != nil {
		return float64(score.Value())
	}
	return -1.0
}

// eolEvidenceSource derives a signal value from the EOL evidence source.
// Falls back to "primary-source" when no evidence is available.
func eolEvidenceSource(eol EOLStatus) string {
	if len(eol.Evidences) > 0 {
		src := eol.Evidences[0].Source
		if src != "" {
			return strings.ToLower(src)
		}
	}
	return "primary-source"
}

// severitySummary and buildReviewNeededReason removed — Reason text is now
// concise one-line summaries; detailed data is in Signals.
