// Package analysis implements lifecycle assessment domain service logic.
package analysis

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	DormantSilence             time.Duration
	DormantFlag                time.Duration
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
	rules.DormantSilence, rules.DormantFlag = c.DormantWindows()
	return &LifecycleAssessorService{rules: rules}
}

// Assess performs lifecycle assessment and returns an AssessmentResult using the lifecycle decision tree logic.
func (s *LifecycleAssessorService) Assess(ctx context.Context, in AssessmentInput) (*AssessmentResult, error) {
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	res, err := s.assessInternal(ctx, in)
	if err != nil || res == nil {
		return res, err
	}
	return s.applyDormantBurst(in, res), nil
}

// applyDormantBurst turns an ok outcome (Active or Legacy-Safe) into Review
// Needed when the analysed version came out in a burst across release lines
// after the DormantSilence window without a release, and is no older than the
// DormantFlag window. Every other label is returned unchanged: replacing
// Stalled or EOL-* would stop a --fail-on gate on those labels from firing.
// See ADR-0026 and ADR-0028.
func (s *LifecycleAssessorService) applyDormantBurst(in AssessmentInput, res *AssessmentResult) *AssessmentResult {
	switch MaintenanceStatus(res.Label) {
	case LabelActive, LabelLegacySafe:
	default:
		return res
	}
	b := in.Analysis.DormantBurst(in.Now, s.rules.DormantSilence, s.rules.DormantFlag)
	if b == nil {
		return res
	}
	trace := append(append([]string(nil), res.Trace...), "dormant_release_burst_review_needed (was "+res.Label+")")
	return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelReviewNeeded), Reason: dormantBurstReason(b), Trace: trace, Signals: append(dormantBurstSignals(b), res.Signals...)}
}

// sig creates a Signal with Role=SignalUsed.
func sig(name, value string) Signal { return Signal{Name: name, Value: value, Role: SignalUsed} }

// sigAbsent creates a Signal with Role=SignalAbsent.
func sigAbsent(name string) Signal { return Signal{Name: name, Role: SignalAbsent} }

// assessInternal contains the decision tree producing an AssessmentResult for the lifecycle axis with trace.
func (s *LifecycleAssessorService) assessInternal(ctx context.Context, in AssessmentInput) (*AssessmentResult, error) {
	analysis := in.Analysis
	scores := in.Scores
	now := in.Now
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
		return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelEOLScheduled), Reason: reason, Trace: trace, Signals: signals}, nil
	}
	// 1. Primary-source EOL status override.
	// EOL-Confirmed is driven ONLY by an explicit primary-source signal, for example
	// npm deprecated, a PyPI classifier or yank, Packagist abandoned, NuGet deprecation
	// or Maven relocation. Evaluator.ensureRuleChain holds the rules that can set it;
	// this list is not that chain. Checked before the archive branch so
	// the EOL verdict and its reason are attributed to that signal, not to the archive flag.
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
		return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelEOLConfirmed), Reason: reason, Trace: trace, Signals: []Signal{sig(SignalEOLSource, signalSource)}}, nil
	}

	// 1.25 Package-level distribution withdrawal (every published release yanked).
	// Placed before the archive branch: Stalled's rationale below assumes the package
	// "remains installable under the same PURL", which this fact falsifies. It is not
	// EOL either — the project may be alive and distributed elsewhere. See ADR-0022.
	if analysis != nil && analysis.AllReleasesYanked() {
		rs := analysis.RegistryState
		reason := "All releases yanked on " + rs.Registry
		if rs.Reason != "" {
			reason += ": " + rs.Reason
		}
		signals := []Signal{sig(SignalAllReleasesYanked, rs.Registry)}
		if analysis.IsArchived() {
			signals = append(signals, sig(SignalRepoArchived, "true"))
		}
		if analysis.IsDisabled() {
			signals = append(signals, sig(SignalRepoDisabled, "true"))
		}
		trace = append(trace, "all_releases_yanked_review_needed")
		return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelReviewNeeded), Reason: reason, Trace: trace, Signals: signals}, nil
	}

	// 1.4 Advisory-database maintenance warning (RustSec's unmaintained marker).
	// A single branch owns the outcome so "a flagged package is never reported
	// Active and never Legacy-Safe" is a property of one place rather than of
	// every exit below remembering to check. Placed before the archive branch on
	// purpose: that branch returns Stalled unconditionally and would otherwise
	// mask the EOL-Effective case. Never EOL-Confirmed — see ADR-0025.
	if analysis != nil && analysis.AdvisoryDBUnmaintained() {
		ad := analysis.AdvisoryDBState
		reason := "Flagged unmaintained by " + ad.AdvisoryID
		if ad.Summary != "" {
			reason += ": " + ad.Summary
		}
		signals := []Signal{sig(SignalAdvisoryDBUnmaintained, ad.AdvisoryID), commitSignal(analysis)}
		signals = append(signals, s.collectAdvisorySignals(analysis)...)
		if analysis.IsArchived() {
			signals = append(signals, sig(SignalRepoArchived, "true"))
		}
		if analysis.IsDisabled() {
			signals = append(signals, sig(SignalRepoDisabled, "true"))
		}
		// AdvisoryID is passed alongside MarkerIDs, which already contains it for
		// any state ClassifyUnmaintained built: a caller of the library facade can
		// hand-build a state with only the evidence ID set, and that one must still
		// be excluded.
		label, t := string(LabelStalled), "advisory_db_unmaintained"
		switch s.advisorySeverityExcluding(analysis, append([]string{ad.AdvisoryID}, ad.MarkerIDs...)) {
		case severityOwnHigh:
			label, t = string(LabelEOLEffective), "advisory_db_unmaintained_unpatched_vulns"
			reason = "Unmaintained per " + ad.AdvisoryID + ", unpatched vulnerabilities"
		case severityDependencyOnlyHigh:
			res := s.dependencyHighOutcome(s.getStableOrMaxVersionDetail(analysis), now, reason, "advisory_db_unmaintained", signals)
			res.Trace = append(trace, res.Trace...)
			return res, nil
		case severityDependencyOnlyUnknown:
			t = "advisory_db_unmaintained_no_direct_high_severity"
			reason += "; dependency advisories of unknown severity"
		}
		trace = append(trace, t)
		return &AssessmentResult{Axis: LifecycleAxis, Label: label, Reason: reason, Trace: trace, Signals: signals}, nil
	}

	// 1.5 Archive/disable check (reached only when there is no primary-source EOL).
	// An archived/disabled repository is a "development ceased" signal, but archive alone is NOT
	// end-of-life: a monorepo-consolidated package can keep publishing (e.g. google-auth-library,
	// google-gax, k6deps), and even a long-dormant package remains installable under the same PURL.
	// So it is Stalled (frozen, but not declared EOL), never EOL-Confirmed on the archive flag alone.
	if analysis != nil && (analysis.IsArchived() || analysis.IsDisabled()) {
		var signals []Signal
		if analysis.IsArchived() {
			signals = append(signals, sig(SignalRepoArchived, "true"))
		}
		if analysis.IsDisabled() {
			signals = append(signals, sig(SignalRepoDisabled, "true"))
		}
		trace = append(trace, "repo_archived_or_disabled_not_eol")
		return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled), Reason: "Repository archived/disabled but not declared end-of-life", Trace: trace, Signals: signals}, nil
	}

	// 2. Data validity check — residual vulnerability override
	if len(scores) == 0 && analysis != nil {
		if res := s.residualVulnResult(analysis, now, "scorecard_missing", sigAbsent(SignalMaintainedScore), trace); res != nil {
			return res, nil
		}
	}

	maintainedScore := s.getScoreValue(scores, "Maintained")
	vulnScore := s.getScoreValue(scores, "Vulnerabilities")

	if (maintainedScore < 0 || vulnScore < 0) && analysis != nil {
		if res := s.residualVulnResult(analysis, now, "scorecard_incomplete", maintainedSignal(scores), trace); res != nil {
			return res, nil
		}
	}

	// 3. Activity level determination
	if analysis != nil {
		hasRecentStable := analysis.hasRecentStableReleaseAt(s.rules.RecentStableWindowDays, now)
		hasRecentPrerelease := analysis.hasRecentPrereleaseReleaseAt(s.rules.RecentPrereleaseWindowDays, now)
		hasRecentHumanCommit := analysis.hasRecentHumanCommitAt(s.rules.MaxHumanCommitGapDays, now)

		if hasRecentStable || hasRecentPrerelease || hasRecentHumanCommit {
			trace = append(trace, "active_path")
			res, _ := s.assessActiveState(analysis, scores, now)
			if res != nil {
				res.Trace = append(trace, res.Trace...)
			}
			return res, nil
		}

		// 3.5. Commit data validity check
		threshold := s.rules.RecentStableWindowDays * s.rules.LegacyFrozenYears * 10
		if analysis.GetDaysSinceLastCommit() >= threshold {
			trace = append(trace, "commit_data_missing_threshold")
			return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelReviewNeeded), Reason: "Commit data missing", Trace: trace, Signals: []Signal{sigAbsent(SignalLastHumanCommit)}}, nil
		}

		// 4. Detailed lifecycle classification of inactive state
		trace = append(trace, "inactive_path")
		res, _ := s.assessInactiveState(analysis, scores, now)
		if res != nil {
			res.Trace = append(trace, res.Trace...)
		}
		return res, nil
	}

	// Fallback when no analysis data available
	trace = append(trace, "no_analysis_data")
	return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelReviewNeeded), Reason: "Insufficient data for assessment", Trace: trace, Signals: []Signal{sigAbsent(SignalLastHumanCommit), sigAbsent(SignalMaintainedScore)}}, nil
}

// residualVulnOutcome is what the residual-vulnerability override found.
type residualVulnOutcome int

const (
	residualVulnNone residualVulnOutcome = iota
	// residualVulnOwn: the package's own advisories meet the condition.
	residualVulnOwn
	// residualVulnDependencyOnly: the condition is met only when dependency
	// advisories are counted too.
	residualVulnDependencyOnly
)

// residualVulnResult returns the assessment for the residual-vulnerability
// override, or nil when the override does not apply. prefix names the
// Scorecard state ("scorecard_missing" / "scorecard_incomplete").
//
// The dependency-only case returns Stalled here instead of falling through:
// a recent release would otherwise reach active_stable and turn the package
// ok. See ADR-0032.
func (s *LifecycleAssessorService) residualVulnResult(a *Analysis, now time.Time, prefix string, mSig Signal, trace []string) *AssessmentResult {
	outcome := s.residualVulnOverride(a, now)
	if outcome == residualVulnNone {
		return nil
	}
	signals := []Signal{commitSignal(a), mSig}
	signals = append(signals, s.collectAdvisorySignals(a)...)
	if outcome == residualVulnOwn {
		return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelEOLEffective),
			Reason: "Unmaintained, unpatched vulnerabilities", Trace: append(trace, prefix+" residual_vuln_override"), Signals: signals}
	}
	vd := s.getStableOrMaxVersionDetail(a)
	tracePrefix := prefix + " residual_vuln"
	var res *AssessmentResult
	if vd.HighSeverityAdvisoryCount(s.rules.HighSeverityCVSSThreshold) == 0 {
		res = &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled),
			Reason: "Unmaintained; dependency advisories of unknown severity", Trace: []string{tracePrefix + "_dependency_only"}, Signals: signals}
	} else {
		res = s.dependencyHighOutcome(vd, now, "Unmaintained", tracePrefix, signals)
	}
	// With a ResidualAdvisoryThreshold above 1, own advisories severe enough
	// can still fall short of the count; say so instead of blaming the
	// dependencies.
	if res.Label == string(LabelStalled) && s.isPotentiallyHigh(vd.DirectOnly()) {
		res.Reason = "Unmaintained; too few own advisories; dependency advisories reach the threshold"
	}
	res.Trace = append(trace, res.Trace...)
	return res
}

// dependencyHighOutcome decides a dormant package whose HIGH advisories are
// all in dependencies: EOL-Effective when one of them has aged, Stalled
// otherwise. prefix starts the reason and tracePrefix the trace name; the
// result carries the deciding advisory. See ADR-0032.
func (s *LifecycleAssessorService) dependencyHighOutcome(vd *VersionDetail, now time.Time, prefix, tracePrefix string, signals []Signal) *AssessmentResult {
	res := &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled), Signals: signals}
	age := ClassifyDependencyAdvisoryAge(vd, s.rules.HighSeverityCVSSThreshold, s.rules.EolInactivityDays, now)
	if age == nil {
		res.Reason = prefix + "; HIGH advisories only in dependencies"
		res.Trace = []string{tracePrefix + "_dependency_only"}
		return res
	}
	res.DependencyAdvisoryAge = age
	res.Reason = prefix + "; " + age.Reason(s.rules.EolInactivityDays)
	res.Trace = []string{tracePrefix + "_dependency_" + string(age.Status)}
	if age.Status == DependencyAdvisoryAged {
		res.Label = string(LabelEOLEffective)
		res.Signals = append(res.Signals, sig(SignalAgedDependencyAdvisory, fmt.Sprintf("%s (%s)", age.AdvisoryID, day(age.Published))))
	}
	return res
}

// residualVulnOverride reports whether Scorecard-less dormancy plus unresolved
// advisories on the latest stable version (or MaxSemver fallback) is grounds
// for EOL-Effective. Conditions:
//   - commit data present and the last human commit older than EolInactivityDays
//   - advisory count >= ResidualAdvisoryThreshold, and HIGH severity (or any
//     advisory of unknown severity)
//
// Only the package's own advisories make it residualVulnOwn.
func (s *LifecycleAssessorService) residualVulnOverride(a *Analysis, now time.Time) residualVulnOutcome {
	if a == nil || a.RepoState == nil {
		return residualVulnNone
	}
	// Require actual commit data to prove dormancy. When LatestHumanCommit is nil
	// (e.g., no GITHUB_TOKEN), daysSinceLastHumanCommitAt returns 9999 which
	// would falsely satisfy the dormancy threshold — that is absence of evidence,
	// not evidence of inactivity.
	if a.RepoState.LatestHumanCommit == nil {
		return residualVulnNone
	}
	if a.daysSinceLastHumanCommitAt(now) <= s.rules.EolInactivityDays {
		return residualVulnNone
	}
	vd := s.getStableOrMaxVersionDetail(a)
	if s.residualVulnsMeetThreshold(vd.DirectOnly()) {
		return residualVulnOwn
	}
	if s.residualVulnsMeetThreshold(vd) {
		return residualVulnDependencyOnly
	}
	return residualVulnNone
}

// residualVulnsMeetThreshold applies the count and severity conditions of the
// residual-vulnerability override to vd's advisories.
func (s *LifecycleAssessorService) residualVulnsMeetThreshold(vd *VersionDetail) bool {
	if vd == nil {
		return false
	}
	count := len(vd.Advisories)
	if count < s.rules.ResidualAdvisoryThreshold || count == 0 {
		return false
	}
	// Only use severity-based override logic when all advisories have known severity.
	// If any advisory severity is unknown, fall back to the count-based logic
	// because unknown advisories may still be HIGH/CRITICAL.
	if vd.UnknownSeverityAdvisoryCount() == 0 {
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

// advisorySeverity is what the HIGH-severity check found.
type advisorySeverity int

const (
	// severityLowOnly: no HIGH advisory, own or in dependencies.
	severityLowOnly advisorySeverity = iota
	// severityOwnHigh: the package's own advisories include a HIGH one.
	severityOwnHigh
	// severityDependencyOnlyHigh: HIGH advisories exist only in dependencies.
	severityDependencyOnlyHigh
	// severityDependencyOnlyUnknown: no HIGH advisory anywhere, but a
	// dependency advisory has no CVSS3 score.
	severityDependencyOnlyUnknown
)

// advisorySeverityExcluding classifies the latest version's advisories,
// ignoring the advisories named in excludeIDs (compared case-insensitively).
// An advisory of unknown severity counts as potentially HIGH.
//
// Why excludeIDs exists for branch 1.4: RustSec files its unmaintained marker
// as an advisory of its own, and deps.dev lists it on the version with no CVSS
// score. The conservative "unknown severity counts as potentially high"
// fallback would then read the branch's own evidence back as a vulnerability,
// so every flagged crate would reach EOL-Effective and the Stalled outcome
// would be unreachable. See ADR-0025.
func (s *LifecycleAssessorService) advisorySeverityExcluding(a *Analysis, excludeIDs []string) advisorySeverity {
	vd := s.getStableOrMaxVersionDetail(a).ExcludingAdvisories(excludeIDs)
	if s.isPotentiallyHigh(vd.DirectOnly()) {
		return severityOwnHigh
	}
	if s.isPotentiallyHigh(vd) {
		if vd.HighSeverityAdvisoryCount(s.rules.HighSeverityCVSSThreshold) > 0 {
			return severityDependencyOnlyHigh
		}
		return severityDependencyOnlyUnknown
	}
	return severityLowOnly
}

// isPotentiallyHigh reports whether vd has an advisory with CVSS3 >= threshold,
// or an advisory of unknown severity (conservative fallback).
func (s *LifecycleAssessorService) isPotentiallyHigh(vd *VersionDetail) bool {
	if vd == nil || len(vd.Advisories) == 0 {
		return false
	}
	if vd.UnknownSeverityAdvisoryCount() > 0 {
		return true
	}
	return vd.HighSeverityAdvisoryCount(s.rules.HighSeverityCVSSThreshold) > 0
}

// severityOutcome picks the label, reason and trace for the C2a / C3a
// branches. ownHighReason is used when the package's own advisories are HIGH;
// otherwise the label is Stalled and the reason starts with stalledPrefix and
// says what was found. highSuffix completes the EOL-Effective trace name.
func severityOutcome(sev advisorySeverity, tracePrefix, ownHighReason, stalledPrefix, highSuffix string) (string, string, string) {
	switch sev {
	case severityOwnHigh:
		return string(LabelEOLEffective), ownHighReason, tracePrefix + highSuffix
	case severityDependencyOnlyUnknown:
		return string(LabelStalled), stalledPrefix + "; dependency advisories of unknown severity", tracePrefix + "_no_direct_high_severity"
	default:
		return string(LabelStalled), stalledPrefix + "; only lower-severity advisories", tracePrefix + "_low_severity"
	}
}

// assessActiveState handles active repository states using domain models
func (s *LifecycleAssessorService) assessActiveState(analysis *Analysis, scores map[string]*ScoreEntity, now time.Time) (*AssessmentResult, error) {
	hasRecentStable := analysis.hasRecentStableReleaseAt(s.rules.RecentStableWindowDays, now)
	hasRecentPrerelease := analysis.hasRecentPrereleaseReleaseAt(s.rules.RecentPrereleaseWindowDays, now)
	isMaintenanceOk := analysis.IsMaintenanceOk()

	// A recent stable/prerelease publish is the strongest activity signal.
	if hasRecentStable {
		signals := []Signal{sig(SignalRecentStableRelease, "true"), commitSignal(analysis), maintainedSignal(scores)}
		return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelActive), Reason: "Actively maintained with recent releases", Trace: []string{"active_stable"}, Signals: signals}, nil
	} else if hasRecentPrerelease {
		signals := []Signal{sig(SignalRecentPreRelease, "true"), commitSignal(analysis), maintainedSignal(scores)}
		return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelActive), Reason: "Actively maintained with recent pre-release", Trace: []string{"active_prerelease"}, Signals: signals}, nil
	} else { // hasRecentCommit only
		if analysis.IsVCSDirectDelivery() {
			signals := []Signal{commitSignal(analysis), sig(SignalEcosystemDelivery, "vcs-direct"), maintainedSignal(scores)}
			return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelActive), Reason: "Active commits (VCS-direct delivery)", Trace: []string{"active_commits_only_vcs_direct"}, Signals: signals}, nil
		}
		hasMaintainedScore := s.getScoreValue(scores, "Maintained") >= 0
		if isMaintenanceOk {
			return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelActive), Reason: "Active commits, no recent publish", Trace: []string{"active_commits_only_maintenance_ok"}, Signals: []Signal{commitSignal(analysis), maintainedSignal(scores)}}, nil
		}
		if !hasMaintainedScore {
			return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelActive), Reason: "Active commits, scorecard unavailable", Trace: []string{"active_commits_only_maintenance_unknown"}, Signals: []Signal{commitSignal(analysis), sigAbsent(SignalMaintainedScore)}}, nil
		}
		return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled), Reason: "Active commits but low maintenance", Trace: []string{"active_commits_only_maintenance_low"}, Signals: []Signal{commitSignal(analysis), maintainedSignal(scores)}}, nil
	}
}

// assessInactiveState performs detailed lifecycle classification of inactive states using domain models.
// The function branches on HasCommitData() to prevent sentinel values (9999/999.0) from
// daysSinceLastHumanCommitAt/lastHumanCommitYearsAt leaking into commit-based comparisons
// when GITHUB_TOKEN is absent.
func (s *LifecycleAssessorService) assessInactiveState(analysis *Analysis, scores map[string]*ScoreEntity, now time.Time) (*AssessmentResult, error) {
	vulnScore := s.getScoreValue(scores, "Vulnerabilities")
	maintainedScore := s.getScoreValue(scores, "Maintained")
	hasVulnScore := vulnScore >= 0
	hasMaintainedScore := maintainedScore >= 0
	isMaintenanceOk := analysis.IsMaintenanceOk()
	// Note: Primary-source EOL is handled at entry (in.EOL). Do not re-evaluate here to avoid duplication.

	// ── Path A: Commit data available (GITHUB_TOKEN set) ──
	if analysis.HasCommitData() {
		daysSinceLastHumanCommit := analysis.daysSinceLastHumanCommitAt(now)
		lastHumanCommitYears := analysis.lastHumanCommitYearsAt(now)
		cSig := commitSignal(analysis)
		mSig := maintainedSignal(scores)

		// High vulnerability score (≥8): prioritize safety classification
		if hasVulnScore && vulnScore >= s.rules.VulnerabilityScoreGoodMin {
			advSignals := s.collectAdvisorySignals(analysis)
			if lastHumanCommitYears >= float64(s.rules.LegacyFrozenYears) {
				signals := append([]Signal{cSig}, advSignals...)
				return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelLegacySafe), Reason: "Dormant but no known vulnerabilities", Trace: []string{"inactive_legacy_safe_vuln_score_high"}, Signals: signals}, nil
			}
			signals := append([]Signal{cSig}, advSignals...)
			return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled), Reason: "Low vulnerability risk but inactive", Trace: []string{"inactive_stalled_vuln_score_high_recent"}, Signals: signals}, nil
		}

		advisoryCount, _ := s.getStableOrMaxAdvisory(analysis)
		if advisoryCount == 0 && daysSinceLastHumanCommit > s.rules.EolInactivityDays {
			return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelLegacySafe),
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
					return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelEOLEffective), Reason: "Unmaintained, unpatched vulnerabilities", Signals: signals}, nil
				}
				return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled), Reason: "Low maintenance, long-term inactive", Signals: []Signal{cSig, mSig}}, nil
			}
			return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled), Reason: "Low maintenance", Signals: []Signal{cSig, mSig}}, nil
		}

		if hasMaintainedScore && isMaintenanceOk {
			return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled),
				Reason:  "Maintained but no recent activity",
				Trace:   []string{"inactive_commit_maintenance_ok_partial_scores"},
				Signals: []Signal{cSig, mSig}}, nil
		}
		if analysis.hasPublishDataAt(now) {
			daysSincePublish := analysis.daysSinceLatestPublishAt(now)
			publishSig := sig(SignalDaysSinceRelease, fmt.Sprintf("%d", daysSincePublish))
			if daysSincePublish <= s.rules.EolInactivityDays {
				return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled),
					Reason:  "No recent activity, recent publish",
					Trace:   []string{"inactive_commit_no_scores_recent_publish"},
					Signals: []Signal{cSig, publishSig, mSig}}, nil
			}
			if daysSinceLastHumanCommit > s.rules.EolInactivityDays && daysSincePublish > s.rules.EolInactivityDays {
				return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled),
					Reason:  "Long-term inactive, no recent releases",
					Trace:   []string{"inactive_commit_no_scores_old_publish"},
					Signals: []Signal{cSig, publishSig, mSig}}, nil
			}
		}
		if !analysis.hasPublishDataAt(now) && !hasMaintainedScore {
			return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled),
				Reason:  "No recent activity, no registry data",
				Trace:   []string{"inactive_github_only_stalled"},
				Signals: []Signal{cSig, sigAbsent(SignalMaintainedScore)}}, nil
		}
		return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelReviewNeeded), Reason: "Insufficient data for assessment", Trace: []string{"inactive_commit_data_scores_inconclusive"}, Signals: []Signal{cSig, mSig}}, nil
	}

	// ── Path B: No commit data (no GITHUB_TOKEN) ──
	return s.assessInactiveNoCommitData(analysis, scores, maintainedScore, hasMaintainedScore, isMaintenanceOk, now)
}

// assessInactiveNoCommitData classifies inactive packages when commit data is unavailable.
// Decision tree (C1-C3) uses scorecard, publish recency, and advisory data from deps.dev.
func (s *LifecycleAssessorService) assessInactiveNoCommitData(
	analysis *Analysis,
	scores map[string]*ScoreEntity,
	maintainedScore float64,
	hasMaintainedScore, isMaintenanceOk bool,
	now time.Time,
) (*AssessmentResult, error) {
	daysSincePublish := analysis.daysSinceLatestPublishAt(now)
	advisoryCount, _ := s.getStableOrMaxAdvisory(analysis)
	hasAdvisories := advisoryCount >= s.rules.ResidualAdvisoryThreshold && advisoryCount > 0
	severity := s.advisorySeverityExcluding(analysis, nil)
	cSig := sigAbsent(SignalLastHumanCommit) // no commit data in this path
	mSig := maintainedSignal(scores)
	dSig := sigAbsent(SignalDaysSinceRelease)
	if analysis.hasPublishDataAt(now) {
		dSig = sig(SignalDaysSinceRelease, fmt.Sprintf("%d", daysSincePublish))
	}

	// C1: Scorecard Maintained ≥ threshold
	if hasMaintainedScore && isMaintenanceOk {
		return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled),
			Reason:  "Maintained but no commit data",
			Trace:   []string{"inactive_no_commit_C1_maintenance_ok"},
			Signals: []Signal{cSig, mSig}}, nil
	}

	// C2: Scorecard Maintained < threshold
	if hasMaintainedScore && !isMaintenanceOk {
		if hasAdvisories && analysis.hasPublishDataAt(now) && daysSincePublish > s.rules.EolInactivityDays {
			if severity == severityDependencyOnlyHigh {
				signals := append([]Signal{cSig, mSig, dSig}, s.collectAdvisorySignals(analysis)...)
				return s.dependencyHighOutcome(s.getStableOrMaxVersionDetail(analysis), now, "Unmaintained", "inactive_no_commit_C2a_low_maint_advisory", signals), nil
			}
			label, reason, trace := severityOutcome(severity, "inactive_no_commit_C2a_low_maint_advisory",
				"Unmaintained, unpatched vulnerabilities", "Unmaintained", "_old_publish")
			signals := append([]Signal{cSig, mSig, dSig}, s.collectAdvisorySignals(analysis)...)
			return &AssessmentResult{Axis: LifecycleAxis, Label: label,
				Reason:  reason,
				Trace:   []string{trace},
				Signals: signals}, nil
		}
		return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled),
			Reason:  "Low maintenance, no commit data",
			Trace:   []string{"inactive_no_commit_C2b_low_maint"},
			Signals: []Signal{cSig, mSig}}, nil
	}

	// C3: No scorecard — deps.dev signals only
	if hasAdvisories {
		if analysis.hasPublishDataAt(now) && daysSincePublish > s.rules.EolInactivityDays {
			if severity == severityDependencyOnlyHigh {
				signals := append([]Signal{cSig, mSig, dSig}, s.collectAdvisorySignals(analysis)...)
				return s.dependencyHighOutcome(s.getStableOrMaxVersionDetail(analysis), now, "No recent releases", "inactive_no_commit_C3a_advisory", signals), nil
			}
			label, reason, trace := severityOutcome(severity, "inactive_no_commit_C3a_advisory",
				"Unpatched vulnerabilities, no recent releases", "No recent releases", "_old_publish")
			signals := append([]Signal{cSig, mSig, dSig}, s.collectAdvisorySignals(analysis)...)
			return &AssessmentResult{Axis: LifecycleAxis, Label: label,
				Reason:  reason,
				Trace:   []string{trace},
				Signals: signals}, nil
		}
		signals := append([]Signal{cSig, mSig, dSig}, s.collectAdvisorySignals(analysis)...)
		return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled),
			Reason:  "Unpatched vulnerabilities, recent publish",
			Trace:   []string{"inactive_no_commit_C3b_advisory_recent_publish"},
			Signals: signals}, nil
	}

	// No advisories path — only use publish-age thresholds when publish data exists
	if analysis.hasPublishDataAt(now) {
		if daysSincePublish <= s.rules.RecentStableWindowDays {
			return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelActive),
				Reason:  "Recently published, no known vulnerabilities",
				Trace:   []string{"inactive_no_commit_C3c_no_advisory_recent_publish"},
				Signals: []Signal{dSig, sig(SignalAdvisoryCount, "0")}}, nil
		}
		if daysSincePublish <= s.rules.EolInactivityDays {
			return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelStalled),
				Reason:  "No vulnerabilities but no recent releases",
				Trace:   []string{"inactive_no_commit_C3d_no_advisory_mid_publish"},
				Signals: []Signal{dSig, sig(SignalAdvisoryCount, "0")}}, nil
		}
		return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelLegacySafe),
			Reason:  "Dormant but no known vulnerabilities",
			Trace:   []string{"inactive_no_commit_C3e_no_advisory_old_publish"},
			Signals: []Signal{dSig, sig(SignalAdvisoryCount, "0")}}, nil
	}

	return &AssessmentResult{Axis: LifecycleAxis, Label: string(LabelReviewNeeded), Reason: "Insufficient data for assessment", Trace: []string{"inactive_no_commit_fallback_review_needed"}, Signals: []Signal{cSig, mSig}}, nil
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
		if n := vd.TransitiveAdvisoryCount(); n > 0 {
			signals = append(signals, sig(SignalTransitiveAdvisoryCount, fmt.Sprintf("%d", n)))
		}
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

// dormantBurstReason states the risk (a possible hijacked release), the facts
// behind it, the harmless reading that looks the same, and what to do: check
// the publisher, or stay on the earlier release when the version was removed.
func dormantBurstReason(b *DormantBurst) string {
	lines := make([]string, len(b.Lines))
	for i, l := range b.Lines {
		lines[i] = l + ".x"
	}
	reason := fmt.Sprintf("Possible hijacked release. This version came out after %d days with no release, "+
		"as part of a set published within a day on the %s lines. Takeovers of npm packages that had "+
		"gone quiet looked like this (node-ipc 2026, rc 2021), but so does a maintainer who returns "+
		"and ships a last fix to an old line next to a new major.",
		b.SilentDays, strings.Join(lines, ", "))
	if b.Removed {
		// npm drops the manifest of a removed version, _npmUser included, so
		// the publisher check below is no longer possible.
		return reason + " npm has since removed this version, so its publisher can no longer be " +
			"looked up there; stay on the release before the silence unless the maintainer confirms this one."
	}
	return reason + " Before using it, check who published it (npm view <package>@<version> _npmUser) " +
		"and what changed since the previous release."
}

func dormantBurstSignals(b *DormantBurst) []Signal {
	signals := []Signal{
		sig(SignalDormantReleaseBurst, strings.Join(b.Versions, ", ")),
		sig(SignalDaysSilentBeforeRelease, fmt.Sprintf("%d", b.SilentDays)),
	}
	if b.Removed {
		signals = append(signals, sig(SignalVersionRemoved, "true"))
	}
	return signals
}
