package analysis

import "testing"

func TestClassifyBuildIntegrity(t *testing.T) {
	tests := []struct {
		name  string
		score float64
		want  BuildIntegrityLabel
	}{
		{"negative_score", -1.0, BuildLabelUngraded},
		{"zero", 0.0, BuildLabelWeak},
		{"just_below_moderate", 2.49, BuildLabelWeak},
		{"exactly_moderate", 2.5, BuildLabelModerate},
		{"mid_moderate", 5.0, BuildLabelModerate},
		{"just_below_hardened", 7.49, BuildLabelModerate},
		{"exactly_hardened", 7.5, BuildLabelHardened},
		{"max_score", 10.0, BuildLabelHardened},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyBuildIntegrity(tt.score)
			if got != tt.want {
				t.Errorf("ClassifyBuildIntegrity(%.2f) = %q, want %q", tt.score, got, tt.want)
			}
		})
	}
}
