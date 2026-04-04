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

// SignalRole classifies how a signal contributed to the assessment decision.
type SignalRole string

const (
	// SignalUsed means the signal was evaluated and influenced the verdict.
	SignalUsed SignalRole = "used"
	// SignalAbsent means the signal was checked but data was unavailable.
	SignalAbsent SignalRole = "absent"
)

// Signal records a single data point evaluated during lifecycle assessment.
// Signals make the verdict transparent: users can see exactly which inputs
// led to the classification.
type Signal struct {
	Name  string     // machine-friendly identifier (e.g., "maintained_score")
	Value string     // human-readable value (e.g., "0/10", "2023-07-15")
	Role  SignalRole // used or absent
}

// Well-known signal names used by the lifecycle assessor.
const (
	SignalEOLSource           = "eol_source"
	SignalEOLScheduledDate    = "eol_scheduled_date"
	SignalRepoArchived        = "repo_archived"
	SignalRepoDisabled        = "repo_disabled"
	SignalMaintainedScore     = "maintained_score"
	SignalLastHumanCommit     = "last_human_commit"
	SignalRecentStableRelease = "recent_stable_release"
	SignalRecentPreRelease    = "recent_pre_release"
	SignalAdvisoryCount       = "advisory_count"
	SignalMaxAdvisorySeverity = "max_advisory_severity"
	SignalDaysSinceRelease    = "days_since_release"
	SignalEcosystemDelivery   = "ecosystem_delivery"
)

// AssessmentResult is the normalized output for a single axis assessment.
// For lifecycle we project existing lifecycle assessment entity fields into this structure.
type AssessmentResult struct {
	Axis    AssessmentAxis
	Label   MaintenanceStatus
	Reason  string
	Trace   []string          // debug-only explanatory steps (not printed in normal CLI)
	Meta    map[string]string // future extensibility (key/value lightweight diagnostics)
	Signals []Signal          // data points evaluated for this verdict
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
