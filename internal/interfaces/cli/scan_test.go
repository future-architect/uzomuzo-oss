package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

func TestNewEnrichedJSONEntry_NilAnalysis(t *testing.T) {
	e := &domainaudit.AuditEntry{
		PURL:     "pkg:npm/unknown@1.0.0",
		Verdict:  domainaudit.VerdictReview,
		ErrorMsg: "fetch failed",
	}

	je := newEnrichedJSONEntry(e)

	if je.PURL != e.PURL {
		t.Errorf("PURL = %q, want %q", je.PURL, e.PURL)
	}
	if je.Verdict != "review" {
		t.Errorf("Verdict = %q, want %q", je.Verdict, "review")
	}
	if je.Error != "fetch failed" {
		t.Errorf("Error = %q, want %q", je.Error, "fetch failed")
	}
	if je.RepoURL != "" {
		t.Errorf("RepoURL should be empty for nil Analysis, got %q", je.RepoURL)
	}
}

func TestNewEnrichedJSONEntry_WithAnalysis(t *testing.T) {
	a := &analysis.Analysis{
		RepoURL:        "https://github.com/expressjs/express",
		OverallScore:   85.5,
		DependentCount: 1000,
		EOL:            analysis.EOLStatus{State: analysis.EOLEndOfLife, Successor: "pkg:npm/fastify"},
		ProjectLicense: analysis.ResolvedLicense{Expression: "MIT"},
		AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
			analysis.LifecycleAxis: {Label: string(analysis.LabelEOLConfirmed), Reason: "EOL confirmed"},
		},
	}

	e := &domainaudit.AuditEntry{
		PURL:     "pkg:npm/express@4.18.2",
		Verdict:  domainaudit.VerdictReplace,
		Analysis: a,
	}

	je := newEnrichedJSONEntry(e)

	if je.RepoURL != a.RepoURL {
		t.Errorf("RepoURL = %q, want %q", je.RepoURL, a.RepoURL)
	}
	if je.OverallScore != 85.5 {
		t.Errorf("OverallScore = %f, want 85.5", je.OverallScore)
	}
	if je.DependentCount != 1000 {
		t.Errorf("DependentCount = %d, want 1000", je.DependentCount)
	}
	if je.Successor != "pkg:npm/fastify" {
		t.Errorf("Successor = %q, want %q", je.Successor, "pkg:npm/fastify")
	}
	if je.ProjectLicense != "MIT" {
		t.Errorf("ProjectLicense = %q, want %q", je.ProjectLicense, "MIT")
	}
	if je.Reason != "EOL confirmed" {
		t.Errorf("Reason = %q, want %q", je.Reason, "EOL confirmed")
	}
}

// TestNewEnrichedJSONEntry_DependencyAdvisoryAge pins the JSON object that
// names the dependency advisory deciding a dormant package (ADR-0032), and
// that it is absent when no dependency advisory decided the label.
func TestNewEnrichedJSONEntry_DependencyAdvisoryAge(t *testing.T) {
	published := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)
	eligible := time.Date(2028, 4, 23, 0, 0, 0, 0, time.UTC)
	withAge := &analysis.Analysis{AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
		analysis.LifecycleAxis: {Label: string(analysis.LabelStalled), DependencyAdvisoryAge: &analysis.DependencyAdvisoryAge{
			Status: analysis.DependencyAdvisoryNotAged, AdvisoryID: "GHSA-w5hq-g745-h8pq", Dependency: "uuid@8.3.2",
			Published: published, EligibleOn: eligible}},
	}}
	je := newEnrichedJSONEntry(&domainaudit.AuditEntry{PURL: "pkg:npm/exceljs@4.4.0", Verdict: domainaudit.VerdictCaution, Analysis: withAge})
	got := je.DependencyAdvisoryAge
	if got == nil {
		t.Fatal("DependencyAdvisoryAge: got nil")
	}
	want := dependencyAdvisoryAgeJSON{Status: analysis.DependencyAdvisoryNotAged, AdvisoryID: "GHSA-w5hq-g745-h8pq", Dependency: "uuid@8.3.2",
		Published: published, EligibleOn: eligible}
	if *got != want {
		t.Errorf("DependencyAdvisoryAge: got %+v, want %+v", *got, want)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if wantJSON := `{"status":"not_aged","advisory_id":"GHSA-w5hq-g745-h8pq","dependency":"uuid@8.3.2","published":"2026-04-22T00:00:00Z","eligible_on":"2028-04-23T00:00:00Z"}`; string(raw) != wantJSON {
		t.Errorf("JSON:\n got %s\nwant %s", raw, wantJSON)
	}

	without := &analysis.Analysis{AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
		analysis.LifecycleAxis: {Label: string(analysis.LabelEOLEffective)},
	}}
	if je := newEnrichedJSONEntry(&domainaudit.AuditEntry{PURL: "pkg:npm/x@1.0.0", Analysis: without}); je.DependencyAdvisoryAge != nil {
		t.Errorf("DependencyAdvisoryAge without a dependency decision: got %+v, want nil", je.DependencyAdvisoryAge)
	}
}
