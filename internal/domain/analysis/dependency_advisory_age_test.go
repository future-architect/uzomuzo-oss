package analysis

import (
	"testing"
	"time"
)

// TestClassifyDependencyAdvisoryAge_DayBoundary pins that age is counted in
// whole UTC days: the same instant gives the same answer in any time zone, and
// an advisory exactly eolDays old is not aged yet.
func TestClassifyDependencyAdvisoryAge_DayBoundary(t *testing.T) {
	t.Parallel()
	const eolDays = 730
	published := time.Date(2024, 10, 28, 23, 30, 0, 0, time.UTC)
	jst := time.FixedZone("JST", 9*60*60)
	tests := []struct {
		name string
		now  time.Time
		want DependencyAdvisoryAgeStatus
	}{
		{"730 days later, UTC", time.Date(2026, 10, 28, 1, 0, 0, 0, time.UTC), DependencyAdvisoryNotAged},
		{"730 days later in UTC, already the next day in Tokyo", time.Date(2026, 10, 28, 20, 0, 0, 0, time.UTC).In(jst), DependencyAdvisoryNotAged},
		{"731 days later", time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), DependencyAdvisoryAged},
	}
	vd := &VersionDetail{Advisories: []Advisory{{ID: "GHSA-a", Relation: AdvisoryRelationTransitive, CVSS3Score: 7.5,
		RecordLookup: AdvisoryRecordFound, Published: published}}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyDependencyAdvisoryAge(vd, 7.0, eolDays, tt.now)
			if got == nil {
				t.Fatal("got nil")
			}
			if got.Status != tt.want {
				t.Errorf("Status: got %q, want %q", got.Status, tt.want)
			}
			if tt.want == DependencyAdvisoryNotAged && !got.EligibleOn.Equal(time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)) {
				t.Errorf("EligibleOn: got %v, want 2026-10-29", got.EligibleOn)
			}
		})
	}
}

// TestClassifyDependencyAdvisoryAge_Selection pins which advisories are
// considered (dependency, known score at or above the threshold) and that the
// pick does not depend on slice order.
func TestClassifyDependencyAdvisoryAge_Selection(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	old := now.AddDate(-5, 0, 0)
	own := Advisory{ID: "GHSA-own", Relation: AdvisoryRelationDirect, CVSS3Score: 9.8, RecordLookup: AdvisoryRecordFound, Published: old}
	unrated := Advisory{ID: "GHSA-unrated", Relation: AdvisoryRelationTransitive, RecordLookup: AdvisoryRecordFound, Published: old}
	low := Advisory{ID: "GHSA-low", Relation: AdvisoryRelationTransitive, CVSS3Score: 6.5, RecordLookup: AdvisoryRecordFound, Published: old}
	b := Advisory{ID: "GHSA-b", Relation: AdvisoryRelationTransitive, CVSS3Score: 7.5, RecordLookup: AdvisoryRecordLookupFailed}
	a := Advisory{ID: "GHSA-a", Relation: AdvisoryRelationTransitive, CVSS3Score: 7.5, RecordLookup: AdvisoryRecordLookupFailed}

	if got := ClassifyDependencyAdvisoryAge(&VersionDetail{Advisories: []Advisory{own, unrated, low}}, 7.0, 730, now); got != nil {
		t.Errorf("own, unrated and below-threshold advisories: got %+v, want nil", got)
	}
	got := ClassifyDependencyAdvisoryAge(&VersionDetail{Advisories: []Advisory{own, unrated, low}}, 6.0, 730, now)
	if got == nil || got.AdvisoryID != "GHSA-low" || got.Status != DependencyAdvisoryAged {
		t.Errorf("threshold 6.0: got %+v, want GHSA-low aged", got)
	}
	for _, order := range [][]Advisory{{a, b}, {b, a}} {
		if got := ClassifyDependencyAdvisoryAge(&VersionDetail{Advisories: order}, 7.0, 730, now); got == nil || got.AdvisoryID != "GHSA-a" {
			t.Errorf("order %s,%s: got %+v, want GHSA-a", order[0].ID, order[1].ID, got)
		}
	}
	if got := ClassifyDependencyAdvisoryAge(nil, 7.0, 730, now); got != nil {
		t.Errorf("nil VersionDetail: got %+v, want nil", got)
	}
}

// TestClassifyDependencyAdvisoryAge_SameDayAndFacts pins that two advisories
// published on the same UTC day are ordered by ID, whatever their times, and
// that the fetched date and rating stay on the result for every status.
func TestClassifyDependencyAdvisoryAge_SameDayAndFacts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 1, 10, 1, 0, 0, 0, time.UTC)
	z := Advisory{ID: "GHSA-z", Relation: AdvisoryRelationTransitive, CVSS3Score: 7.5, RecordLookup: AdvisoryRecordFound, Published: recent}
	a := Advisory{ID: "GHSA-a", Relation: AdvisoryRelationTransitive, CVSS3Score: 7.5, RecordLookup: AdvisoryRecordFound, Published: recent.Add(22 * time.Hour)}
	for _, order := range [][]Advisory{{z, a}, {a, z}} {
		got := ClassifyDependencyAdvisoryAge(&VersionDetail{Advisories: order}, 7.0, 730, now)
		if got == nil || got.AdvisoryID != "GHSA-a" || got.Status != DependencyAdvisoryNotAged {
			t.Errorf("order %s,%s: got %+v, want GHSA-a not_aged", order[0].ID, order[1].ID, got)
		}
	}

	old := now.AddDate(-4, 0, 0)
	for _, tc := range []struct {
		lookup AdvisoryRecordLookup
		rating string
		want   DependencyAdvisoryAgeStatus
	}{
		{AdvisoryRecordFound, "HIGH", DependencyAdvisoryAged},
		{AdvisoryRecordFound, "MODERATE", DependencyAdvisoryRatedBelowHigh},
		{AdvisoryRecordFound, "IMPORTANT", DependencyAdvisoryRatingUnrecognized},
		{AdvisoryRecordWithdrawn, "HIGH", DependencyAdvisoryWithdrawn},
	} {
		adv := Advisory{ID: "GHSA-x", Relation: AdvisoryRelationTransitive, CVSS3Score: 7.5, RecordLookup: tc.lookup, Published: old, ReviewedSeverity: tc.rating}
		got := ClassifyDependencyAdvisoryAge(&VersionDetail{Advisories: []Advisory{adv}}, 7.0, 730, now)
		if got == nil || got.Status != tc.want || !got.Published.Equal(old) || got.ReviewedSeverity != tc.rating {
			t.Errorf("%s %s: got %+v, want %s with the fetched date and rating", tc.lookup, tc.rating, got, tc.want)
		}
	}
	notLooked := Advisory{ID: "GHSA-x", Relation: AdvisoryRelationTransitive, CVSS3Score: 7.5, Published: old, ReviewedSeverity: "HIGH"}
	if got := ClassifyDependencyAdvisoryAge(&VersionDetail{Advisories: []Advisory{notLooked}}, 7.0, 730, now); got == nil || !got.Published.IsZero() || got.ReviewedSeverity != "" {
		t.Errorf("not looked up: got %+v, want no date or rating (not a fetched fact)", got)
	}
}

// TestCountDependencyAdvisoryAgeUnknown counts only failed lookups that decided.
func TestCountDependencyAdvisoryAgeUnknown(t *testing.T) {
	t.Parallel()
	with := func(s DependencyAdvisoryAgeStatus) *Analysis {
		return &Analysis{AxisResults: map[AssessmentAxis]*AssessmentResult{LifecycleAxis: {DependencyAdvisoryAge: &DependencyAdvisoryAge{Status: s}}}}
	}
	got := CountDependencyAdvisoryAgeUnknown(map[string]*Analysis{
		"a": with(DependencyAdvisoryUnknown), "b": with(DependencyAdvisoryAged), "c": {}, "d": nil,
	})
	if got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}
