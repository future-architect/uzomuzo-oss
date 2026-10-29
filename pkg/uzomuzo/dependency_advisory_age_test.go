package uzomuzo_test

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/pkg/uzomuzo"
)

// TestDependencyAdvisoryAgeFacade checks that a library caller can read the
// deciding dependency advisory with facade names only (ADR-0032).
func TestDependencyAdvisoryAgeFacade(t *testing.T) {
	t.Parallel()
	res := uzomuzo.AssessmentResult{DependencyAdvisoryAge: &uzomuzo.DependencyAdvisoryAge{Status: uzomuzo.DependencyAdvisoryUnknown}}
	adv := uzomuzo.Advisory{RecordLookup: uzomuzo.AdvisoryRecordLookupFailed}
	failed := func(s uzomuzo.DependencyAdvisoryAgeStatus, l uzomuzo.AdvisoryRecordLookup) bool {
		return s == uzomuzo.DependencyAdvisoryUnknown && l == uzomuzo.AdvisoryRecordLookupFailed
	}
	if !failed(res.DependencyAdvisoryAge.Status, adv.RecordLookup) {
		t.Errorf("got %q %q", res.DependencyAdvisoryAge.Status, adv.RecordLookup)
	}
	if uzomuzo.SignalAgedDependencyAdvisory == "" || uzomuzo.SignalTransitiveAdvisoryCount == "" {
		t.Error("signal names are empty")
	}
}
