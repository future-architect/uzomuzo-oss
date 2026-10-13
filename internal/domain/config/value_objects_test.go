package config

import (
	"testing"
	"time"
)

// TestNormalizeLifecycleConfig_DormantWindows pins that zero and negative
// dormant windows fall back to the defaults while a positive value is kept,
// and that DormantWindows converts the days. See ADR-0028.
func TestNormalizeLifecycleConfig_DormantWindows(t *testing.T) {
	t.Parallel()

	def := DefaultValues.Lifecycle
	tests := []struct {
		name        string
		silence     int
		flag        int
		wantSilence int
		wantFlag    int
	}{
		{"zero uses the defaults", 0, 0, def.DormantSilenceDays, def.DormantFlagDays},
		{"negative uses the defaults", -1, -1, def.DormantSilenceDays, def.DormantFlagDays},
		{"positive is kept", 30, 5, 30, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := LifecycleAssessmentConfig{DormantSilenceDays: tt.silence, DormantFlagDays: tt.flag}
			NormalizeLifecycleConfig(&c)
			if c.DormantSilenceDays != tt.wantSilence || c.DormantFlagDays != tt.wantFlag {
				t.Fatalf("got silence=%d flag=%d, want %d and %d", c.DormantSilenceDays, c.DormantFlagDays, tt.wantSilence, tt.wantFlag)
			}
			silence, flag := c.DormantWindows()
			if want := time.Duration(tt.wantSilence) * 24 * time.Hour; silence != want {
				t.Errorf("silence window = %v, want %v", silence, want)
			}
			if want := time.Duration(tt.wantFlag) * 24 * time.Hour; flag != want {
				t.Errorf("flag window = %v, want %v", flag, want)
			}
		})
	}
}
