package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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

func TestEnrichedJSONDependencies(t *testing.T) {
	npm := &analysis.Package{Ecosystem: "npm"}
	for _, tt := range []struct {
		name string
		a    *analysis.Analysis
		want string
	}{
		{"counts", &analysis.Analysis{Package: npm, HasDependencyGraph: true, DependencyGraphVersion: "5.1.0", DirectDepsCount: 28, TransitiveDepsCount: 39}, `"dependencies":{"source":"deps.dev","version":"5.1.0","direct":28,"transitive":39}`},
		{"measured zero", &analysis.Analysis{Package: npm, HasDependencyGraph: true, DependencyGraphVersion: "1.0.0"}, `"dependencies":{"source":"deps.dev","version":"1.0.0","direct":0,"transitive":0}`},
		{"no graph", &analysis.Analysis{Package: npm, DirectDepsCount: 3}, ""},
		{"graph without version", &analysis.Analysis{Package: npm, HasDependencyGraph: true, DirectDepsCount: 3}, ""},
		{"cargo", &analysis.Analysis{Package: &analysis.Package{Ecosystem: "cargo"}, HasDependencyGraph: true, DependencyGraphVersion: "1.0.140", DirectDepsCount: 13}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(newEnrichedJSONEntry(&domainaudit.AuditEntry{Analysis: tt.a}))
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if strings.Contains(string(b), `"dependencies"`) {
					t.Fatalf("unreported counts emitted: %s", b)
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

// TestNewEnrichedJSONEntry_DependencyAdvisoryAge pins the JSON object that
// names the dependency advisory deciding a dormant package (ADR-0032), and
// that the key is absent when no dependency advisory decided the label.
func TestNewEnrichedJSONEntry_DependencyAdvisoryAge(t *testing.T) {
	published := time.Date(2026, 2, 26, 22, 7, 15, 0, time.UTC)
	eligible := time.Date(2028, 2, 27, 0, 0, 0, 0, time.UTC) // UTC day of published + 731
	tests := []struct {
		name string
		age  *analysis.DependencyAdvisoryAge
		want string // the dependency_advisory_age value; empty means the key is absent
	}{
		{
			name: "not aged, with the day it counts",
			age: &analysis.DependencyAdvisoryAge{Status: analysis.DependencyAdvisoryNotAged, AdvisoryID: "GHSA-23c5-xmqv-rm74",
				Dependency: "minimatch@3.0.8", Published: published, EligibleOn: eligible, ReviewedSeverity: "HIGH"},
			want: `{"status":"not_aged","advisory_id":"GHSA-23c5-xmqv-rm74","dependency":"minimatch@3.0.8","published":"2026-02-26T22:07:15Z","eligible_on":"2028-02-27T00:00:00Z","reviewed_severity":"HIGH"}`,
		},
		{
			name: "unknown has no dates",
			age:  &analysis.DependencyAdvisoryAge{Status: analysis.DependencyAdvisoryUnknown, AdvisoryID: "GHSA-w5hq-g745-h8pq", Dependency: "uuid@8.3.2"},
			want: `{"status":"unknown","advisory_id":"GHSA-w5hq-g745-h8pq","dependency":"uuid@8.3.2"}`,
		},
		{name: "own data decided"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &analysis.Analysis{AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
				analysis.LifecycleAxis: {Label: string(analysis.LabelStalled), DependencyAdvisoryAge: tt.age},
			}}
			raw, err := json.Marshal(newEnrichedJSONEntry(&domainaudit.AuditEntry{PURL: "pkg:npm/x@1.0.0", Verdict: domainaudit.VerdictCaution, Analysis: a}))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var entry map[string]json.RawMessage
			if err := json.Unmarshal(raw, &entry); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			got, present := entry["dependency_advisory_age"]
			if tt.want == "" {
				if present {
					t.Errorf("dependency_advisory_age: got %s, want the key absent", got)
				}
				return
			}
			if string(got) != tt.want {
				t.Errorf("dependency_advisory_age:\n got %s\nwant %s", got, tt.want)
			}
		})
	}
}
