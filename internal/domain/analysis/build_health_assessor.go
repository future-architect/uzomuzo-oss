package analysis

import (
	"context"
	"fmt"
	"math"
)

// Build integrity signal names (machine-friendly identifiers).
const (
	SignalDangerousWorkflow    = "dangerous_workflow"
	SignalBranchProtection     = "branch_protection"
	SignalCodeReview           = "code_review"
	SignalTokenPermissions     = "token_permissions"
	SignalBinaryArtifacts      = "binary_artifacts"
	SignalSignedReleases       = "signed_releases"
	SignalSAST                 = "sast"
	SignalPackaging            = "packaging"
	SignalPinnedDependencies   = "pinned_dependencies"
	SignalSLSAVerified         = "slsa_verified"
	SignalAttestationVerified  = "attestation_verified"
)

// buildSignalDef maps a signal to its Scorecard check name and weight.
type buildSignalDef struct {
	SignalName     string
	ScorecardCheck string // empty for SLSA/Attestation
	Weight         float64
}

// buildSignals defines all 11 signals ordered by weight (Critical > High > Medium).
// Weights are aligned with OpenSSF Scorecard risk-level weights (ADR-0013).
var buildSignals = []buildSignalDef{
	{SignalDangerousWorkflow, "Dangerous-Workflow", 10.0},   // Critical
	{SignalBranchProtection, "Branch-Protection", 7.5},      // High
	{SignalCodeReview, "Code-Review", 7.5},                  // High
	{SignalTokenPermissions, "Token-Permissions", 7.5},      // High
	{SignalBinaryArtifacts, "Binary-Artifacts", 7.5},        // High
	{SignalSignedReleases, "Signed-Releases", 7.5},          // High
	{SignalSLSAVerified, "", 7.5},                           // High (editorial)
	{SignalSAST, "SAST", 5.0},                               // Medium
	{SignalPackaging, "Packaging", 5.0},                     // Medium
	{SignalPinnedDependencies, "Pinned-Dependencies", 5.0},  // Medium
	{SignalAttestationVerified, "", 5.0},                     // Medium (editorial)
}

// BuildHealthAssessorService evaluates build pipeline tamper resistance
// using OpenSSF Scorecard checks and SLSA provenance data.
//
// DDD Layer: Domain (pure business rules)
type BuildHealthAssessorService struct{}

// NewBuildHealthAssessorService constructs a new build health assessor.
func NewBuildHealthAssessorService() *BuildHealthAssessorService {
	return &BuildHealthAssessorService{}
}

// Assess computes the build integrity score and label from Scorecard checks
// and SLSA/Attestation signals available in the Analysis.
func (s *BuildHealthAssessorService) Assess(ctx context.Context, in AssessmentInput) (*AssessmentResult, error) {
	scores := in.Scores
	a := in.Analysis
	trace := []string{"start build integrity assessment"}

	var signals []Signal
	var weightedSum, totalWeight float64

	for _, def := range buildSignals {
		switch {
		case def.ScorecardCheck != "":
			sig, weight, score := s.evaluateScorecardSignal(def, scores)
			signals = append(signals, sig)
			if sig.Role == SignalUsed {
				weightedSum += weight * score
				totalWeight += weight
				trace = append(trace, fmt.Sprintf("%s: %.0f/10 (w=%.1f)", def.SignalName, score, weight))
			} else {
				trace = append(trace, fmt.Sprintf("%s: absent", def.SignalName))
			}

		case def.SignalName == SignalSLSAVerified:
			sig, used := s.evaluateSLSASignal(a)
			signals = append(signals, sig)
			if used {
				weightedSum += def.Weight * 10.0
				totalWeight += def.Weight
				trace = append(trace, fmt.Sprintf("%s: verified (w=%.1f)", def.SignalName, def.Weight))
			} else {
				trace = append(trace, fmt.Sprintf("%s: absent", def.SignalName))
			}

		case def.SignalName == SignalAttestationVerified:
			sig, used := s.evaluateAttestationSignal(a)
			signals = append(signals, sig)
			if used {
				weightedSum += def.Weight * 10.0
				totalWeight += def.Weight
				trace = append(trace, fmt.Sprintf("%s: verified (w=%.1f)", def.SignalName, def.Weight))
			} else {
				trace = append(trace, fmt.Sprintf("%s: absent", def.SignalName))
			}
		}
	}

	if totalWeight == 0 {
		trace = append(trace, "no build signals available -> Ungraded")
		return &AssessmentResult{
			Axis:    BuildHealthAxis,
			Label:   string(BuildLabelUngraded),
			Reason:  "No build integrity data available",
			Trace:   trace,
			Signals: signals,
			Meta:    map[string]string{"score": "-1"},
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

// evaluateSLSASignal checks SLSA provenance verification.
func (s *BuildHealthAssessorService) evaluateSLSASignal(a *Analysis) (Signal, bool) {
	if a != nil && a.SLSAVerified {
		return Signal{Name: SignalSLSAVerified, Value: "verified", Role: SignalUsed}, true
	}
	return Signal{Name: SignalSLSAVerified, Role: SignalAbsent}, false
}

// evaluateAttestationSignal checks attestation verification.
func (s *BuildHealthAssessorService) evaluateAttestationSignal(a *Analysis) (Signal, bool) {
	if a != nil && a.AttestationVerified {
		return Signal{Name: SignalAttestationVerified, Value: "verified", Role: SignalUsed}, true
	}
	return Signal{Name: SignalAttestationVerified, Role: SignalAbsent}, false
}
