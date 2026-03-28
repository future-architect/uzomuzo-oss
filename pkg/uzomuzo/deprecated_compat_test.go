package uzomuzo_test

import (
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/pkg/uzomuzo"
)

// TestDeprecatedLifecycleLabelAlias verifies that the deprecated LifecycleLabel
// type alias remains usable and interchangeable with MaintenanceStatus.
func TestDeprecatedLifecycleLabelAlias(t *testing.T) {
	var label uzomuzo.LifecycleLabel = uzomuzo.LabelActive

	// LifecycleLabel must be assignable to MaintenanceStatus (same type).
	var status uzomuzo.MaintenanceStatus = label
	if status != uzomuzo.LabelActive {
		t.Errorf("LifecycleLabel→MaintenanceStatus: got %q, want %q", status, uzomuzo.LabelActive)
	}
}

// TestDeprecatedFinalLifecycleLabelFunc verifies that the deprecated
// FinalLifecycleLabel function returns the same result as FinalMaintenanceStatus.
func TestDeprecatedFinalLifecycleLabelFunc(t *testing.T) {
	// nil Analysis should return "Review Needed" for both.
	got := uzomuzo.FinalLifecycleLabel(nil)
	want := uzomuzo.FinalMaintenanceStatus(nil)
	if got != want {
		t.Errorf("FinalLifecycleLabel(nil) = %q, want %q (same as FinalMaintenanceStatus)", got, want)
	}
}

// TestDeprecatedFinalLifecycleLabelMethod verifies that the deprecated
// (*Analysis).FinalLifecycleLabel() method returns the same result as FinalMaintenanceStatus().
func TestDeprecatedFinalLifecycleLabelMethod(t *testing.T) {
	var a uzomuzo.Analysis
	got := a.FinalLifecycleLabel()
	want := a.FinalMaintenanceStatus()
	if got != want {
		t.Errorf("(*Analysis).FinalLifecycleLabel() = %q, want %q (same as FinalMaintenanceStatus)", got, want)
	}
}

// TestDeprecatedLifecycleSummaryField verifies that the deprecated
// LifecycleSummary.LifecycleLabel field stays in sync with MaintenanceStatus.
func TestDeprecatedLifecycleSummaryField(t *testing.T) {
	// Use a non-nil Analysis with a lifecycle result to exercise the sync path.
	a := &uzomuzo.Analysis{
		AxisResults: map[domain.AssessmentAxis]*uzomuzo.AssessmentResult{
			domain.LifecycleAxis: {Axis: domain.LifecycleAxis, Label: uzomuzo.LabelStalled, Reason: "test"},
		},
	}
	summary := uzomuzo.BuildLifecycleSummary(a)
	if summary.LifecycleLabel != summary.MaintenanceStatus {
		t.Errorf("LifecycleLabel=%q != MaintenanceStatus=%q; deprecated field must stay in sync",
			summary.LifecycleLabel, summary.MaintenanceStatus)
	}
	if summary.MaintenanceStatus != string(uzomuzo.LabelStalled) {
		t.Errorf("MaintenanceStatus=%q, want %q", summary.MaintenanceStatus, uzomuzo.LabelStalled)
	}
}
