package analysis

import "testing"

func TestSeverityFromCVSS3(t *testing.T) {
	tests := []struct {
		name  string
		score float64
		want  string
	}{
		{"zero_returns_empty", 0.0, ""},
		{"negative_returns_empty", -1.0, ""},
		{"low_boundary", 0.1, "LOW"},
		{"low_upper", 3.9, "LOW"},
		{"medium_lower", 4.0, "MEDIUM"},
		{"medium_upper", 6.9, "MEDIUM"},
		{"high_lower", 7.0, "HIGH"},
		{"high_upper", 8.9, "HIGH"},
		{"critical_lower", 9.0, "CRITICAL"},
		{"critical_max", 10.0, "CRITICAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SeverityFromCVSS3(tt.score)
			if got != tt.want {
				t.Errorf("SeverityFromCVSS3(%v) = %q, want %q", tt.score, got, tt.want)
			}
		})
	}
}

func TestVersionDetail_MaxCVSS3(t *testing.T) {
	tests := []struct {
		name       string
		advisories []Advisory
		want       float64
	}{
		{"no_advisories", nil, 0},
		{"all_unknown", []Advisory{{ID: "A", CVSS3Score: 0}}, 0},
		{"single", []Advisory{{ID: "A", CVSS3Score: 7.5}}, 7.5},
		{"multiple", []Advisory{{ID: "A", CVSS3Score: 3.1}, {ID: "B", CVSS3Score: 9.8}}, 9.8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vd := &VersionDetail{Advisories: tt.advisories}
			if got := vd.MaxCVSS3(); got != tt.want {
				t.Errorf("MaxCVSS3() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersionDetail_HighSeverityAdvisoryCount(t *testing.T) {
	vd := &VersionDetail{
		Advisories: []Advisory{
			{ID: "A", CVSS3Score: 3.1},  // LOW
			{ID: "B", CVSS3Score: 7.5},  // HIGH
			{ID: "C", CVSS3Score: 9.8},  // CRITICAL
			{ID: "D", CVSS3Score: 0},    // unknown
		},
	}
	if got := vd.HighSeverityAdvisoryCount(7.0); got != 2 {
		t.Errorf("HighSeverityAdvisoryCount(7.0) = %d, want 2", got)
	}
}

func TestVersionDetail_UnknownSeverityAdvisoryCount(t *testing.T) {
	vd := &VersionDetail{
		Advisories: []Advisory{
			{ID: "A", CVSS3Score: 7.5},
			{ID: "B", CVSS3Score: 0},
			{ID: "C", CVSS3Score: 0},
		},
	}
	if got := vd.UnknownSeverityAdvisoryCount(); got != 2 {
		t.Errorf("UnknownSeverityAdvisoryCount() = %d, want 2", got)
	}
}
