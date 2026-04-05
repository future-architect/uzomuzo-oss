package cli

import (
	"testing"

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
		ProjectLicense: analysis.ResolvedLicense{Identifier: "MIT"},
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
