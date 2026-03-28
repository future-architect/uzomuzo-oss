package analysis

// MaintenanceStatus represents the maintenance status result of a lifecycle assessment.
type MaintenanceStatus string

// Predefined lifecycle assessment labels (expanded EOL taxonomy).
const (
	LabelActive       MaintenanceStatus = "Active"
	LabelStalled      MaintenanceStatus = "Stalled"
	LabelLegacySafe   MaintenanceStatus = "Legacy-Safe"
	LabelEOLConfirmed MaintenanceStatus = "EOL-Confirmed"
	LabelEOLEffective MaintenanceStatus = "EOL-Effective"
	LabelEOLScheduled MaintenanceStatus = "EOL-Scheduled"
	LabelReviewNeeded MaintenanceStatus = "Review Needed"
)

// String returns the string representation.
func (j MaintenanceStatus) String() string { return string(j) }
