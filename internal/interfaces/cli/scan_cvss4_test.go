package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

func TestScanCVSS4Output(t *testing.T) {
	for _, tc := range []struct {
		name     string
		adv      analysis.Advisory
		band, v4 string
	}{
		{"medium", analysis.Advisory{CVSS4Score: 6.6, CVSS4Known: true}, "MEDIUM", "6.6"},
		{"none", analysis.Advisory{CVSS4Known: true}, "NONE", "0.0"},
		{"unknown", analysis.Advisory{}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := domainaudit.AuditEntry{PURL: "pkg:npm/test@1.0.0", Analysis: &analysis.Analysis{ReleaseInfo: &analysis.ReleaseInfo{StableVersion: &analysis.VersionDetail{Advisories: []analysis.Advisory{tc.adv}}}}}
			entry := newEnrichedJSONEntry(&e)
			if entry.MaxAdvisorySeverity != tc.band {
				t.Fatalf("JSON severity=%q", entry.MaxAdvisorySeverity)
			}
			data, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			var obj map[string]any
			if err := json.Unmarshal(data, &obj); err != nil {
				t.Fatal(err)
			}
			_, present := obj["max_cvss4_score"]
			if present != (tc.v4 != "") {
				t.Fatalf("JSON v4 presence=%v: %s", present, data)
			}
			if tc.v4 == "0.0" && obj["max_cvss4_score"] != float64(0) {
				t.Fatalf("JSON NONE=%v", obj["max_cvss4_score"])
			}
			if _, present := obj["max_transitive_cvss4_score"]; present {
				t.Fatalf("direct-only JSON has transitive v4 score: %s", data)
			}
			var buf bytes.Buffer
			if err := renderScanCSV(&buf, []domainaudit.AuditEntry{e}); err != nil {
				t.Fatal(err)
			}
			records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			header, row := records[0], records[1]
			if header[len(header)-2] != "max_cvss4_score" || header[len(header)-1] != "max_transitive_cvss4_score" {
				t.Fatalf("CSV tail=%v", header[len(header)-2:])
			}
			if row[len(row)-2] != tc.v4 {
				t.Fatalf("CSV v4=%q want %q", row[len(row)-2], tc.v4)
			}
			if row[len(row)-1] != "" {
				t.Fatalf("direct-only CSV transitive v4=%q", row[len(row)-1])
			}
		})
	}
}

func TestScanCVSS4MixedVersionMaxima(t *testing.T) {
	advisories := []analysis.Advisory{
		{ID: "v3", CVSS3Score: 9.8, CVSS4Score: 10, CVSS4Known: true, Relation: analysis.AdvisoryRelationDirect},
		{ID: "v4", CVSS4Score: 7.7, CVSS4Known: true, Relation: analysis.AdvisoryRelationTransitive},
	}
	e := domainaudit.AuditEntry{PURL: "pkg:npm/test@1.0.0", Analysis: &analysis.Analysis{ReleaseInfo: &analysis.ReleaseInfo{StableVersion: &analysis.VersionDetail{Advisories: advisories}}}}
	entry := newEnrichedJSONEntry(&e)
	if entry.MaxCVSS3Score != 9.8 || entry.MaxCVSS4Score == nil || *entry.MaxCVSS4Score != 7.7 || entry.MaxTransitiveCVSS4Score == nil || *entry.MaxTransitiveCVSS4Score != 7.7 || entry.MaxAdvisorySeverity != "CRITICAL" || entry.MaxTransitiveAdvisorySeverity != "HIGH" {
		t.Fatalf("mixed maxima=%+v", entry)
	}
	var buf bytes.Buffer
	if err := renderScanCSV(&buf, []domainaudit.AuditEntry{e}); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if got := records[1]; got[len(got)-2] != "7.7" || got[len(got)-1] != "7.7" {
		t.Fatalf("CSV maxima=%v", got)
	}
}

func TestScanCVSS4TableSeverityOrder(t *testing.T) {
	lines := formatAdvisoryLines([]analysis.Advisory{
		{ID: "v4-high", CVSS4Score: 7.7, CVSS4Known: true},
		{ID: "v3-medium", CVSS3Score: 6.5},
		{ID: "v4-none", CVSS4Known: true},
	})
	want := []string{"v4-high", "v3-medium", "v4-none"}
	for i, id := range want {
		if !strings.Contains(lines[i], id) {
			t.Fatalf("line %d=%q want %q", i, lines[i], id)
		}
	}
	if !strings.Contains(lines[2], "NONE") || !strings.Contains(lines[2], "(0.0)") {
		t.Fatalf("known zero line=%q", lines[2])
	}
}
