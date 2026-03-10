package analysis

import "context"

// AssessmentAxis enumerates supported assessment dimensions (extensible).
type AssessmentAxis string

const (
	// LifecycleAxis represents lifecycle (activity/EOL) dimension.
	LifecycleAxis AssessmentAxis = "lifecycle"
	// BuildHealthAxis represents build / release workflow health (future expansion).
	BuildHealthAxis AssessmentAxis = "build_health"
)

// AssessmentInput consolidates inputs required to perform an assessment pass.
type AssessmentInput struct {
	Analysis *Analysis
	Scores   map[string]*ScoreEntity
	EOL      EOLStatus
}

// AssessmentResult is the normalized output for a single axis assessment.
// For lifecycle we project existing LifecycleAssessment entity fields into this structure.
type AssessmentResult struct {
	Axis   AssessmentAxis
	Label  LifecycleLabel
	Reason string
	Trace  []string          // debug-only explanatory steps (not printed in normal CLI)
	Meta   map[string]string // future extensibility (key/value lightweight diagnostics)
}

// AssessmentService defines contract for assessment services.
type AssessmentService interface {
	Assess(ctx context.Context, in AssessmentInput) (*AssessmentResult, error)
}

// CompositeAssessor executes multiple AssessmentService instances and collates results.
type CompositeAssessor struct{ services []AssessmentService }

// NewCompositeAssessor constructs a composite assessor.
func NewCompositeAssessor(services ...AssessmentService) *CompositeAssessor {
	return &CompositeAssessor{services: services}
}

// AssessAll runs all services sequentially (can be parallelized later in infra layer) and returns axis keyed results.
func (c *CompositeAssessor) AssessAll(ctx context.Context, in AssessmentInput) (map[AssessmentAxis]*AssessmentResult, error) {
	out := make(map[AssessmentAxis]*AssessmentResult, len(c.services))
	for _, svc := range c.services {
		res, err := svc.Assess(ctx, in)
		if err != nil {
			return nil, err
		}
		if res != nil {
			out[res.Axis] = res
		}
	}
	return out, nil
}
