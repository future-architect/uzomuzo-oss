package analysis

import "testing"

func TestAdvisoryEffectiveSeverityScore(t *testing.T) {
	tests := []struct {
		name  string
		adv   Advisory
		score float64
		known bool
		band  string
	}{
		{"unknown", Advisory{}, 0, false, ""},
		{"v3 wins", Advisory{CVSS3Score: 9.8, CVSS4Score: 6.6, CVSS4Known: true}, 9.8, true, "CRITICAL"},
		{"v4 medium", Advisory{CVSS4Score: 6.6, CVSS4Known: true}, 6.6, true, "MEDIUM"},
		{"v4 high", Advisory{CVSS4Score: 7.7, CVSS4Known: true}, 7.7, true, "HIGH"},
		{"v4 none", Advisory{CVSS4Known: true}, 0, true, "NONE"},
		{"v4 low boundary", Advisory{CVSS4Score: 3.9, CVSS4Known: true}, 3.9, true, "LOW"},
		{"v4 medium boundary", Advisory{CVSS4Score: 4.0, CVSS4Known: true}, 4.0, true, "MEDIUM"},
		{"v4 high boundary", Advisory{CVSS4Score: 7.0, CVSS4Known: true}, 7.0, true, "HIGH"},
		{"v4 critical boundary", Advisory{CVSS4Score: 9.0, CVSS4Known: true}, 9.0, true, "CRITICAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, known := tt.adv.EffectiveSeverityScore()
			if score != tt.score || known != tt.known || SeverityFromScore(score, known) != tt.band {
				t.Fatalf("score=%v known=%v band=%q, want %v %v %q", score, known, SeverityFromScore(score, known), tt.score, tt.known, tt.band)
			}
		})
	}
}

func TestVersionDetailEffectiveSeverity(t *testing.T) {
	vd := &VersionDetail{Advisories: []Advisory{
		{Relation: AdvisoryRelationDirect, CVSS3Score: 9.8},
		{Relation: AdvisoryRelationTransitive, CVSS4Score: 7.7, CVSS4Known: true},
		{Relation: AdvisoryRelationTransitive, CVSS4Known: true},
	}}
	if got := vd.UnknownSeverityAdvisoryCount(); got != 0 {
		t.Fatalf("unknown=%d", got)
	}
	if got := vd.HighSeverityAdvisoryCount(7); got != 2 {
		t.Fatalf("high=%d", got)
	}
	if got := vd.MaxCVSS3(); got != 9.8 {
		t.Fatalf("max v3=%v", got)
	}
	if score, known := vd.MaxCVSS4(); !known || score != 7.7 {
		t.Fatalf("max v4=%v known=%v", score, known)
	}
	if score, known := vd.MaxEffectiveSeverityScore(); !known || score != 9.8 {
		t.Fatalf("max effective=%v known=%v", score, known)
	}
	if score, known := vd.MaxTransitiveEffectiveSeverityScore(); !known || score != 7.7 {
		t.Fatalf("max transitive=%v known=%v", score, known)
	}
}

func TestCountCVSS4LookupFailures(t *testing.T) {
	analyses := map[string]*Analysis{
		"one":  {ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Advisories: []Advisory{{CVSS4LookupFailed: true}}}}},
		"two":  {ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Advisories: []Advisory{{CVSS4LookupFailed: true}, {CVSS4LookupFailed: true}}}}},
		"none": {ReleaseInfo: &ReleaseInfo{StableVersion: &VersionDetail{Advisories: []Advisory{{CVSS4Known: true}}}}},
	}
	if got := CountCVSS4LookupFailures(analyses); got != 2 {
		t.Fatalf("failed analyses=%d, want 2", got)
	}
}
