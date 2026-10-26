package analysis

import (
	"testing"
)

func TestMaintenanceStatus_String(t *testing.T) {
	tests := []struct {
		name  string
		label MaintenanceStatus
		want  string
	}{
		{
			name:  "active_label",
			label: LabelActive,
			want:  "Active",
		},
		{
			name:  "stalled_label",
			label: LabelStalled,
			want:  "Stalled",
		},
		{
			name:  "legacy_safe_label",
			label: LabelLegacySafe,
			want:  "Legacy-Safe",
		},
		{
			name:  "eol_confirmed_label",
			label: LabelEOLConfirmed,
			want:  "EOL-Confirmed",
		},
		{
			name:  "eol_effective_label",
			label: LabelEOLEffective,
			want:  "EOL-Effective",
		},
		{
			name:  "eol_scheduled_label",
			label: LabelEOLScheduled,
			want:  "EOL-Scheduled",
		},
		{
			name:  "review_needed_label",
			label: LabelReviewNeeded,
			want:  "Review Needed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.label.String(); got != tt.want {
				t.Errorf("MaintenanceStatus.String() = %v, want %v", got, tt.want)
			}
		})
	}
}
