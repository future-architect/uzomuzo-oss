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

func TestMaintenanceStatusConstants(t *testing.T) {
	tests := []struct {
		name     string
		label    MaintenanceStatus
		expected string
	}{
		{
			name:     "active_constant",
			label:    LabelActive,
			expected: "Active",
		},
		{
			name:     "stalled_constant",
			label:    LabelStalled,
			expected: "Stalled",
		},
		{
			name:     "legacy_safe_constant",
			label:    LabelLegacySafe,
			expected: "Legacy-Safe",
		},
		{
			name:     "eol_confirmed_constant",
			label:    LabelEOLConfirmed,
			expected: "EOL-Confirmed",
		},
		{
			name:     "eol_effective_constant",
			label:    LabelEOLEffective,
			expected: "EOL-Effective",
		},
		{
			name:     "eol_scheduled_constant",
			label:    LabelEOLScheduled,
			expected: "EOL-Scheduled",
		},
		{
			name:     "review_needed_constant",
			label:    LabelReviewNeeded,
			expected: "Review Needed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.label) != tt.expected {
				t.Errorf("MaintenanceStatus constant = %v, want %v", string(tt.label), tt.expected)
			}
		})
	}
}

func TestMaintenanceStatusEquality(t *testing.T) {
	tests := []struct {
		name   string
		label1 MaintenanceStatus
		label2 MaintenanceStatus
		equal  bool
	}{
		{
			name:   "same_active_labels",
			label1: LabelActive,
			label2: LabelActive,
			equal:  true,
		},
		{
			name:   "different_labels",
			label1: LabelActive,
			label2: LabelStalled,
			equal:  false,
		},
		{
			name:   "same_review_needed_labels",
			label1: LabelReviewNeeded,
			label2: LabelReviewNeeded,
			equal:  true,
		},
		{
			name:   "active_vs_legacy_safe",
			label1: LabelActive,
			label2: LabelLegacySafe,
			equal:  false,
		},
		{
			name:   "eol_confirmed_vs_stalled",
			label1: LabelEOLConfirmed,
			label2: LabelStalled,
			equal:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.label1 == tt.label2
			if result != tt.equal {
				t.Errorf("MaintenanceStatus equality = %v, want %v", result, tt.equal)
			}
		})
	}
}

func TestMaintenanceStatusTypeConversion(t *testing.T) {
	tests := []struct {
		name       string
		label      MaintenanceStatus
		stringRep  string
		backToType MaintenanceStatus
	}{
		{
			name:       "active_conversion",
			label:      LabelActive,
			stringRep:  "Active",
			backToType: MaintenanceStatus("Active"),
		},
		{
			name:       "stalled_conversion",
			label:      LabelStalled,
			stringRep:  "Stalled",
			backToType: MaintenanceStatus("Stalled"),
		},
		{
			name:       "legacy_safe_conversion",
			label:      LabelLegacySafe,
			stringRep:  "Legacy-Safe",
			backToType: MaintenanceStatus("Legacy-Safe"),
		},
		{
			name:       "eol_confirmed_conversion",
			label:      LabelEOLConfirmed,
			stringRep:  "EOL-Confirmed",
			backToType: MaintenanceStatus("EOL-Confirmed"),
		},
		{
			name:       "eol_effective_conversion",
			label:      LabelEOLEffective,
			stringRep:  "EOL-Effective",
			backToType: MaintenanceStatus("EOL-Effective"),
		},
		{
			name:       "eol_scheduled_conversion",
			label:      LabelEOLScheduled,
			stringRep:  "EOL-Scheduled",
			backToType: MaintenanceStatus("EOL-Scheduled"),
		},
		{
			name:       "review_needed_conversion",
			label:      LabelReviewNeeded,
			stringRep:  "Review Needed",
			backToType: MaintenanceStatus("Review Needed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test label to string
			if tt.label.String() != tt.stringRep {
				t.Errorf("Label to string = %v, want %v", tt.label.String(), tt.stringRep)
			}

			// Test string to label and back
			if tt.backToType != tt.label {
				t.Errorf("String to label back conversion = %v, want %v", tt.backToType, tt.label)
			}
		})
	}
}
