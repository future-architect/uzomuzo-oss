package analysis

import (
	"context"
	"fmt"
	"math"
)

// Build integrity signal names (machine-friendly identifiers).
const (
	SignalDangerousWorkflow  = "dangerous_workflow"
	SignalBranchProtection   = "branch_protection"
	SignalCodeReview         = "code_review"
	SignalTokenPermissions   = "token_permissions"
	SignalBinaryArtifacts    = "binary_artifacts"
	SignalPinnedDependencies = "pinned_dependencies"

	// ScoreUngraded is the sentinel value stored in Meta["score"] for Ungraded results.
	ScoreUngraded = "-1"
)

// buildSignalDef maps a signal to its Scorecard check name and weight.
type buildSignalDef struct {
	SignalName     string
	ScorecardCheck string // OpenSSF Scorecard check name for this signal
	Weight         float64
}

// minEvaluatedSignals is the minimum number of evaluated signals required
// for a grade. Below this threshold, the result is Ungraded to prevent
// inflated scores from a small number of "easy" checks.
const minEvaluatedSignals = 3

// buildSignals defines the 6 signals with sufficient real-world availability.
// Weights are aligned with OpenSSF Scorecard risk-level weights (ADR-0013).
//
// Excluded due to insufficient availability (100-project survey, 2026-04):
//   - Signed-Releases (12% available) — nearly all N/A
//   - Packaging (26%, all 10/10 when present) — no discriminating power
//   - SLSA Provenance (3%) — ecosystem adoption too low
//   - Attestation (3%) — ecosystem adoption too low
//   - SAST (code quality, not build tamper resistance)
//
// These can be re-added in Phase 4 as ecosystem adoption increases.
var buildSignals = []buildSignalDef{
	{SignalDangerousWorkflow, "Dangerous-Workflow", 10.0},  // Critical
	{SignalBranchProtection, "Branch-Protection", 7.5},     // High
	{SignalCodeReview, "Code-Review", 7.5},                 // High
	{SignalTokenPermissions, "Token-Permissions", 7.5},     // High
	{SignalBinaryArtifacts, "Binary-Artifacts", 7.5},       // High
	{SignalPinnedDependencies, "Pinned-Dependencies", 5.0}, // Medium
}

// BuildHealthAssessorService evaluates build pipeline tamper resistance
// using OpenSSF Scorecard checks.
//
// DDD Layer: Domain (pure business rules)
type BuildHealthAssessorService struct{}

// NewBuildHealthAssessorService constructs a new build health assessor.
func NewBuildHealthAssessorService() *BuildHealthAssessorService {
	return &BuildHealthAssessorService{}
}

// Assess computes the build integrity score and label from Scorecard checks.
func (s *BuildHealthAssessorService) Assess(ctx context.Context, in AssessmentInput) (*AssessmentResult, error) {
	scores := in.Scores
	trace := []string{"start build integrity assessment"}

	var signals []Signal
	var weightedSum, totalWeight float64

	for _, def := range buildSignals {
		sig, weight, score := s.evaluateScorecardSignal(def, scores)
		signals = append(signals, sig)
		if sig.Role == SignalUsed {
			weightedSum += weight * score
			totalWeight += weight
			trace = append(trace, fmt.Sprintf("%s: %.0f/10 (w=%.1f)", def.SignalName, score, weight))
		} else {
			trace = append(trace, fmt.Sprintf("%s: absent", def.SignalName))
		}
	}

	// Count evaluated signals for minimum threshold check.
	var evaluatedCount int
	for _, sig := range signals {
		if sig.Role == SignalUsed {
			evaluatedCount++
		}
	}

	if evaluatedCount == 0 {
		trace = append(trace, "no build signals available -> Ungraded")
		return &AssessmentResult{
			Axis:    BuildHealthAxis,
			Label:   string(BuildLabelUngraded),
			Reason:  "No build integrity data available",
			Trace:   trace,
			Signals: signals,
			Meta:    map[string]string{"score": ScoreUngraded},
		}, nil
	}

	if evaluatedCount < minEvaluatedSignals {
		trace = append(trace, fmt.Sprintf("only %d/%d signals evaluated (min %d) -> Ungraded", evaluatedCount, len(signals), minEvaluatedSignals))
		return &AssessmentResult{
			Axis:    BuildHealthAxis,
			Label:   string(BuildLabelUngraded),
			Reason:  fmt.Sprintf("Too few signals evaluated (%d/%d, min %d)", evaluatedCount, len(signals), minEvaluatedSignals),
			Trace:   trace,
			Signals: signals,
			Meta:    map[string]string{"score": ScoreUngraded},
		}, nil
	}

	score := math.Round(weightedSum/totalWeight*10) / 10
	label := ClassifyBuildIntegrity(score)
	trace = append(trace, fmt.Sprintf("score=%.1f -> %s", score, label))

	return &AssessmentResult{
		Axis:    BuildHealthAxis,
		Label:   string(label),
		Reason:  fmt.Sprintf("Build integrity score: %.1f/10", score),
		Trace:   trace,
		Signals: signals,
		Meta:    map[string]string{"score": fmt.Sprintf("%.1f", score)},
	}, nil
}

// evaluateScorecardSignal checks a Scorecard-based signal.
func (s *BuildHealthAssessorService) evaluateScorecardSignal(def buildSignalDef, scores map[string]*ScoreEntity) (Signal, float64, float64) {
	se, exists := scores[def.ScorecardCheck]
	if !exists || se == nil {
		return Signal{Name: def.SignalName, Role: SignalAbsent}, 0, 0
	}
	val := se.Value()
	if val < 0 {
		return Signal{Name: def.SignalName, Role: SignalAbsent}, 0, 0
	}
	return Signal{
		Name:  def.SignalName,
		Value: fmt.Sprintf("%d/10", val),
		Role:  SignalUsed,
	}, def.Weight, float64(val)
}
