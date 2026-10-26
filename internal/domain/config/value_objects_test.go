package config

import (
	"math"
	"testing"
	"time"
)

// TestNormalizeLifecycleConfig_DormantWindows pins that zero and negative
// dormant windows fall back to the defaults while a positive value is kept,
// and that DormantWindows converts the days, saturating a count too large for
// time.Duration instead of wrapping to a negative window. See ADR-0028.
func TestNormalizeLifecycleConfig_DormantWindows(t *testing.T) {
	t.Parallel()

	def := DefaultValues.Lifecycle
	tests := []struct {
		name        string
		silence     int
		flag        int
		wantSilence int
		wantFlag    int
		// The expected windows are literals, not computed from the days, so an
		// overflow in the conversion cannot also hide in the expectation.
		wantSilenceWindow time.Duration
		wantFlagWindow    time.Duration
	}{
		{"zero uses the defaults", 0, 0, def.DormantSilenceDays, def.DormantFlagDays, 365 * 24 * time.Hour, 7 * 24 * time.Hour},
		{"negative uses the defaults", -1, -1, def.DormantSilenceDays, def.DormantFlagDays, 365 * 24 * time.Hour, 7 * 24 * time.Hour},
		{"positive is kept", 30, 5, 30, 5, 720 * time.Hour, 120 * time.Hour},
		{"largest day count that fits", 106751, 106751, 106751, 106751, 2562024 * time.Hour, 2562024 * time.Hour},
		{"too large saturates instead of going negative", math.MaxInt, 106752, math.MaxInt, 106752, time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)},
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
			if silence != tt.wantSilenceWindow {
				t.Errorf("silence window = %v, want %v", silence, tt.wantSilenceWindow)
			}
			if flag != tt.wantFlagWindow {
				t.Errorf("flag window = %v, want %v", flag, tt.wantFlagWindow)
			}
		})
	}
}
