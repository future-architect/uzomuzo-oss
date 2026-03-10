package analysis

import "testing"

// TestNewScoreEntity validates creation and accessor methods for ScoreEntity using a table-driven set.
func TestNewScoreEntity(t *testing.T) {
	cases := []struct {
		name      string
		scoreName string
		value     int
		maxValue  int
		reason    string
	}{
		{"security_policy", "Security-Policy", 8, 10, "Security policy file found"},
		{"binary_artifacts", "Binary-Artifacts", 0, 10, "Binary artifacts detected"},
		{"maximum", "Maintained", 10, 10, "Actively maintained"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			s := NewScoreEntity(c.scoreName, c.value, c.maxValue, c.reason)
			if s.Name() != c.scoreName {
				t.Errorf("Name() = %s, want %s", s.Name(), c.scoreName)
			}
			if s.Value() != c.value {
				t.Errorf("Value() = %d, want %d", s.Value(), c.value)
			}
			if s.MaxValue() != c.maxValue {
				t.Errorf("MaxValue() = %d, want %d", s.MaxValue(), c.maxValue)
			}
			if s.Reason() != c.reason {
				t.Errorf("Reason() = %s, want %s", s.Reason(), c.reason)
			}
		})
	}
}
