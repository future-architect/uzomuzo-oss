package analysis

// LifecycleLabel represents the result of a lifecycle assessment.
type LifecycleLabel string

// Predefined lifecycle assessment labels (expanded EOL taxonomy).
const (
	LabelActive       LifecycleLabel = "Active"
	LabelStalled      LifecycleLabel = "Stalled"
	LabelLegacySafe   LifecycleLabel = "Legacy-Safe"
	LabelEOLConfirmed LifecycleLabel = "EOL-Confirmed"
	LabelEOLEffective LifecycleLabel = "EOL-Effective"
	LabelEOLScheduled LifecycleLabel = "EOL-Scheduled"
	LabelReviewNeeded LifecycleLabel = "Review Needed"
)

// String returns the string representation.
func (j LifecycleLabel) String() string { return string(j) }
