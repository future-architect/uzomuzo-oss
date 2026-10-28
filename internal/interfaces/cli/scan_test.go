package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

func TestEnrichedJSONDependents(t *testing.T) {
	zero := 0
	for _, tt := range []struct {
		name        string
		observation *analysis.DependentsObservation
		want        string
	}{
		{"measured zero", &analysis.DependentsObservation{Source: analysis.DependentsSourceDepsDev, Version: "1.0.0", Direct: &zero, Total: &zero}, `"dependents":{"source":"deps.dev","version":"1.0.0","direct":0,"total":0}`},
		{"unknown", nil, ""},
		{"registry", &analysis.DependentsObservation{Source: analysis.DependentsSourceRubyGems, Direct: &zero}, `"dependents":{"source":"RubyGems","direct":0}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			entry := &domainaudit.AuditEntry{Analysis: &analysis.Analysis{DependentCount: 9, Dependents: tt.observation}}
			b, err := json.Marshal(newEnrichedJSONEntry(entry))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), `"dependent_count":9`) {
				t.Fatalf("legacy count changed: %s", b)
			}
			if tt.want == "" {
				if strings.Contains(string(b), `"dependents"`) {
					t.Fatalf("unknown emitted: %s", b)
				}
			} else if !strings.Contains(string(b), tt.want) {
				t.Fatalf("want %s in %s", tt.want, b)
			}
		})
	}
}

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
