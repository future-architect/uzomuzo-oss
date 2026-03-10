package analysis

import (
	"testing"
)

func TestLifecycleLabel_String(t *testing.T) {
	tests := []struct {
		name  string
		label LifecycleLabel
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
				t.Errorf("LifecycleLabel.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLifecycleLabelConstants(t *testing.T) {
	tests := []struct {
		name     string
		label    LifecycleLabel
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
				t.Errorf("LifecycleLabel constant = %v, want %v", string(tt.label), tt.expected)
			}
		})
	}
}

func TestLifecycleLabelEquality(t *testing.T) {
	tests := []struct {
		name   string
		label1 LifecycleLabel
		label2 LifecycleLabel
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
				t.Errorf("LifecycleLabel equality = %v, want %v", result, tt.equal)
			}
		})
	}
}

func TestLifecycleLabelTypeConversion(t *testing.T) {
	tests := []struct {
		name       string
		label      LifecycleLabel
		stringRep  string
		backToType LifecycleLabel
	}{
		{
			name:       "active_conversion",
			label:      LabelActive,
			stringRep:  "Active",
			backToType: LifecycleLabel("Active"),
		},
		{
			name:       "stalled_conversion",
			label:      LabelStalled,
			stringRep:  "Stalled",
			backToType: LifecycleLabel("Stalled"),
		},
		{
			name:       "legacy_safe_conversion",
			label:      LabelLegacySafe,
			stringRep:  "Legacy-Safe",
			backToType: LifecycleLabel("Legacy-Safe"),
		},
		{
			name:       "eol_confirmed_conversion",
			label:      LabelEOLConfirmed,
			stringRep:  "EOL-Confirmed",
			backToType: LifecycleLabel("EOL-Confirmed"),
		},
		{
			name:       "eol_effective_conversion",
			label:      LabelEOLEffective,
			stringRep:  "EOL-Effective",
			backToType: LifecycleLabel("EOL-Effective"),
		},
		{
			name:       "eol_scheduled_conversion",
			label:      LabelEOLScheduled,
			stringRep:  "EOL-Scheduled",
			backToType: LifecycleLabel("EOL-Scheduled"),
		},
		{
			name:       "review_needed_conversion",
			label:      LabelReviewNeeded,
			stringRep:  "Review Needed",
			backToType: LifecycleLabel("Review Needed"),
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
