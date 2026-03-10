package analysis

import (
	"context"
)

// BuildHealthAssessorService is a placeholder assessor for future build / CI health evaluation.
//
// DDD Layer: Domain (pure business rules; current implementation is a stub)
// Extension Strategy:
//   - In future, incorporate signals such as: presence of CI workflows, release cadence quality,
//     code review density, test coverage heuristics (if obtainable), or build badge parsing.
//   - Keep external API calls out of this layer; inject pre-computed signals via AssessmentInput.Analysis fields.
//   - Populate Trace with step rationale; CLI can expose Trace in debug mode.
//
// Zero-value safe: Can be instantiated directly via NewBuildHealthAssessorService().
type BuildHealthAssessorService struct{}

// NewBuildHealthAssessorService constructs a new build health assessor.
func NewBuildHealthAssessorService() *BuildHealthAssessorService {
	return &BuildHealthAssessorService{}
}

// Assess performs a placeholder evaluation returning ReviewNeeded until real logic is added.
// Always returns a non-nil AssessmentResult (never errors for now) so callers can rely on axis presence.
func (s *BuildHealthAssessorService) Assess(ctx context.Context, in AssessmentInput) (*AssessmentResult, error) {
	trace := []string{"build_health: stub assessor executed", "no real build signals implemented yet"}
	return &AssessmentResult{
		Axis:   BuildHealthAxis,
		Label:  LabelReviewNeeded, // Reuse lifecycle labels for now; a dedicated label set could be introduced later.
		Reason: "Build health assessment not yet implemented",
		Trace:  trace,
		Meta:   map[string]string{"version": "0"},
	}, nil
}
