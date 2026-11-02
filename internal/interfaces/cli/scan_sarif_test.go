package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"reflect"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"github.com/future-architect/uzomuzo-oss/internal/domain/scan"
)

func sarifTestEntry(purl string, label analysis.MaintenanceStatus, verdict audit.Verdict) audit.AuditEntry {
	return audit.AuditEntry{PURL: purl, Verdict: verdict, Analysis: &analysis.Analysis{AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{analysis.LifecycleAxis: {Label: string(label), Reason: "Fixture evidence"}}}}
}

func TestSARIFGolden(t *testing.T) {
	entries := []audit.AuditEntry{
		{PURL: "pkg:npm/missing@1", Verdict: audit.VerdictReview, ErrorMsg: "lookup failed"},
		sarifTestEntry("pkg:npm/archived@1", analysis.LabelStalled, audit.VerdictReplace),
		sarifTestEntry("pkg:npm/stalled@1", analysis.LabelStalled, audit.VerdictCaution),
		sarifTestEntry("pkg:npm/legacy@1", analysis.LabelLegacySafe, audit.VerdictOK),
		sarifTestEntry("pkg:npm/disabled@1", analysis.LabelActive, audit.VerdictOK),
		sarifTestEntry("pkg:npm/healthy@1", analysis.LabelActive, audit.VerdictOK),
	}
	entries[1].Analysis.RepoState = &analysis.RepoState{IsArchived: true}
	entries[1].Locations = []depparser.SourceLocation{{Line: 3}, {Line: 8}}
	entries[1].Analysis.EOL.Successor = "pkg:npm/successor"
	entries[2].Locations = []depparser.SourceLocation{{Line: 5}}
	entries[4].Analysis.RepoState = &analysis.RepoState{IsDisabled: true}
	policy, err := scan.ParseFailPolicy("legacy-safe,disabled")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := renderScanSARIF(&buf, entries, scanRenderOptions{Policy: policy, ArtifactURI: "./go.mod", ToolVersion: "1.2.3"}); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/scan_sarif.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if buf.String() != string(want) {
		t.Fatalf("SARIF differs from golden:\n%s", buf.String())
	}
}

func TestSARIFLocationsAndPolicyIdentity(t *testing.T) {
	entry := sarifTestEntry("pkg:npm/%40scope/legacy@1.0.0?arch=x#src", analysis.LabelLegacySafe, audit.VerdictCaution)
	entry.Locations = []depparser.SourceLocation{{Line: 7}, {Line: 12}}
	policy, err := scan.ParseFailPolicy("legacy-safe")
	if err != nil {
		t.Fatal(err)
	}
	var fingerprint, rule string
	for _, tt := range []struct {
		name, uri     string
		sbom          bool
		source        audit.EntrySource
		wantLocations int
		wantRegion    bool
	}{
		{"file", "./go.mod", false, audit.SourceDirect, 2, true},
		{"sbom", "bom.json", true, audit.SourceDirect, 1, false},
		{"stdin", "", false, audit.SourceDirect, 0, false},
		{"discovered", "purls.txt", false, audit.SourceActions, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			entry.Source = tt.source
			for i, p := range []scan.FailPolicy{{}, policy} {
				var buf bytes.Buffer
				if err := renderScanSARIF(&buf, []audit.AuditEntry{entry}, scanRenderOptions{Policy: p, ArtifactURI: tt.uri, SBOM: tt.sbom}); err != nil {
					t.Fatal(err)
				}
				var log sarifLog
				if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
					t.Fatal(err)
				}
				result := log.Runs[0].Results[0]
				if len(result.Locations) != tt.wantLocations {
					t.Fatalf("locations = %v", result.Locations)
				}
				for _, loc := range result.Locations {
					if (loc.PhysicalLocation.Region != nil) != tt.wantRegion {
						t.Fatalf("region = %v", loc.PhysicalLocation.Region)
					}
				}
				if i == 0 {
					fingerprint = result.PartialFingerprints["uzomuzoFinding/v1"]
					rule = result.RuleID
					if result.Level != "note" {
						t.Fatalf("level=%s", result.Level)
					}
				} else {
					if result.RuleID != rule || result.PartialFingerprints["uzomuzoFinding/v1"] != fingerprint || result.Level != "error" {
						t.Fatalf("policy changed identity or wrong level: %+v", result)
					}
				}
				if log.Runs[0].Tool.Driver.Rules[result.RuleIndex].ID != result.RuleID {
					t.Fatal("incorrect rule index")
				}
			}
		})
	}
}

func TestSARIFRawPolicyAndDisplayStatus(t *testing.T) {
	e := sarifTestEntry("pkg:npm/eol@1", analysis.LabelStalled, audit.VerdictReplace)
	e.Analysis.EOL = analysis.EOLStatus{State: analysis.EOLEndOfLife}
	policy, err := scan.ParseFailPolicy("eol-confirmed")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := renderScanSARIF(&buf, []audit.AuditEntry{e}, scanRenderOptions{Policy: policy}); err != nil {
		t.Fatal(err)
	}
	var log sarifLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatal(err)
	}
	r := log.Runs[0].Results[0]
	if r.RuleID != "uzomuzo/eol-confirmed" || r.Level != "warning" {
		t.Fatalf("result=%+v", r)
	}
}

func TestSARIFAllRules(t *testing.T) {
	tests := []struct {
		entry audit.AuditEntry
		rule  string
	}{
		{audit.AuditEntry{}, "analysis-incomplete"},
		{audit.AuditEntry{Analysis: &analysis.Analysis{Error: errors.New("failed")}}, "analysis-incomplete"},
		{audit.AuditEntry{Analysis: &analysis.Analysis{MaliciousState: &analysis.MaliciousState{Status: analysis.MaliciousStatusFlagged}}}, "malicious"},
		{audit.AuditEntry{Analysis: &analysis.Analysis{RepoState: &analysis.RepoState{IsArchived: true}}}, "archived"},
		{audit.AuditEntry{Analysis: &analysis.Analysis{RepoState: &analysis.RepoState{IsDisabled: true}}}, "disabled"},
		{audit.AuditEntry{Analysis: &analysis.Analysis{DormantRelease: &analysis.DormantRelease{Burst: &analysis.DormantBurst{}}, AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{analysis.LifecycleAxis: {Label: string(analysis.LabelActive)}}}}, "dormant-release"},
	}
	for _, pair := range []struct {
		label analysis.MaintenanceStatus
		rule  string
	}{{analysis.LabelEOLConfirmed, "eol-confirmed"}, {analysis.LabelEOLEffective, "eol-effective"}, {analysis.LabelEOLScheduled, "eol-scheduled"}, {analysis.LabelStalled, "stalled"}, {analysis.LabelLegacySafe, "legacy-safe"}, {analysis.LabelReviewNeeded, "review-needed"}, {analysis.LabelActive, "active"}} {
		tests = append(tests, struct {
			entry audit.AuditEntry
			rule  string
		}{sarifTestEntry("pkg:npm/a", pair.label, audit.VerdictReview), pair.rule})
	}
	for _, tt := range tests {
		t.Run(tt.rule, func(t *testing.T) {
			if got := sarifRuleID(&tt.entry); got != "uzomuzo/"+tt.rule {
				t.Fatalf("rule=%s", got)
			}
		})
	}
}

func TestSARIFShowOnlyAndEmpty(t *testing.T) {
	entries := []audit.AuditEntry{sarifTestEntry("pkg:npm/a", analysis.LabelActive, audit.VerdictOK), sarifTestEntry("pkg:npm/b", analysis.LabelStalled, audit.VerdictCaution)}
	allowed, err := ParseShowOnly("ok")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := renderScanOutput(&buf, entries, filterEntriesByVerdict(entries, allowed), FormatSARIF, true, nil, scanRenderOptions{}); err != nil {
		t.Fatal(err)
	}
	var log sarifLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(log.Runs[0].Results, []sarifResult{}) {
		t.Fatalf("results=%+v", log.Runs[0].Results)
	}
}

func TestSARIFArtifactURI(t *testing.T) {
	tests := []struct {
		name, path, want, wantPath, wantScheme string
	}{
		{name: "plain path", path: "go.mod", want: "go.mod", wantPath: "go.mod"},
		{name: "leading dot slash removed", path: "./sub/go.mod", want: "sub/go.mod", wantPath: "sub/go.mod"},
		{name: "hash is not a fragment", path: "deps#old.txt", want: "deps%23old.txt", wantPath: "deps#old.txt"},
		{name: "question mark is not a query", path: "deps?old.txt", want: "deps%3Fold.txt", wantPath: "deps?old.txt"},
		{name: "literal percent stays literal", path: "a%20b.txt", want: "a%2520b.txt", wantPath: "a%20b.txt"},
		{name: "space", path: "my deps.txt", want: "my%20deps.txt", wantPath: "my deps.txt"},
		{name: "colon is not a scheme", path: "a:b.txt", want: "./a:b.txt", wantPath: "./a:b.txt"},
		{name: "windows absolute path", path: "C:/my repo/go.mod", want: "file:///C:/my%20repo/go.mod", wantPath: "/C:/my repo/go.mod", wantScheme: "file"},
		{name: "unix absolute path", path: "/repo/go.mod", want: "/repo/go.mod", wantPath: "/repo/go.mod"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			e := sarifTestEntry("pkg:npm/a@1", analysis.LabelStalled, audit.VerdictCaution)
			e.Source = audit.SourceDirect
			e.Locations = []depparser.SourceLocation{{Line: 3}}
			if err := renderScanSARIF(&buf, []audit.AuditEntry{e}, scanRenderOptions{ArtifactURI: tt.path}); err != nil {
				t.Fatal(err)
			}
			var log sarifLog
			if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
				t.Fatal(err)
			}
			locations := log.Runs[0].Results[0].Locations
			if len(locations) != 1 {
				t.Fatalf("locations = %+v, want one", locations)
			}
			got := locations[0].PhysicalLocation.ArtifactLocation.URI
			if got != tt.want {
				t.Errorf("uri = %q, want %q", got, tt.want)
			}
			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatalf("parse %q: %v", got, err)
			}
			if parsed.Path != tt.wantPath || parsed.Fragment != "" || parsed.RawQuery != "" || parsed.Scheme != tt.wantScheme {
				t.Errorf("uri %q reads as %+v, want path %q, scheme %q", got, parsed, tt.wantPath, tt.wantScheme)
			}
		})
	}
}

func TestSARIFFingerprintVersionAndArtifact(t *testing.T) {
	fingerprints := make([]string, 0, 3)
	for _, tt := range []struct{ purl, uri string }{{"pkg:npm/%40scope/a@1?arch=x#src", "./go.mod"}, {"pkg:npm/%40scope/a@2?arch=x#src", "go.mod"}, {"pkg:npm/%40scope/a@2?arch=x#src", "other/go.mod"}} {
		e := sarifTestEntry(tt.purl, analysis.LabelStalled, audit.VerdictCaution)
		var buf bytes.Buffer
		if err := renderScanSARIF(&buf, []audit.AuditEntry{e}, scanRenderOptions{ArtifactURI: tt.uri}); err != nil {
			t.Fatal(err)
		}
		var log sarifLog
		if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
			t.Fatal(err)
		}
		fingerprints = append(fingerprints, log.Runs[0].Results[0].PartialFingerprints["uzomuzoFinding/v1"])
	}
	if fingerprints[0] != fingerprints[1] || fingerprints[1] == fingerprints[2] {
		t.Fatalf("fingerprints=%v", fingerprints)
	}
}

type sarifFailWriter struct{}

func (sarifFailWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestSARIFWriteError(t *testing.T) {
	if err := renderScanSARIF(sarifFailWriter{}, nil, scanRenderOptions{}); err == nil {
		t.Fatal("expected write error")
	}
}
