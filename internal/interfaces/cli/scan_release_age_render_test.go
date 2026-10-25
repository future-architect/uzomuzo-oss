package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

func TestReleaseAgeDetailedAndJSONOnly(t *testing.T) {
	published := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	entry := audit.AuditEntry{PURL: "pkg:npm/sample@1.0.0", Verdict: audit.VerdictOK, Analysis: &analysis.Analysis{
		ReleaseAge: &analysis.ReleaseAge{Status: analysis.ReleaseAgeYoung, PublishedAt: published, Source: analysis.ReleaseAgeSourceNpm, WindowDays: 7, Until: published.Add(7 * 24 * time.Hour), AgeDays: 2},
	}}
	var detailed bytes.Buffer
	if err := renderBoxEntry(&detailed, &entry); err != nil {
		t.Fatal(err)
	}
	flat := strings.Join(strings.Fields(strings.ReplaceAll(detailed.String(), "│", " ")), " ")
	for _, want := range []string{"Release age:", "2026-10-20", "2 days", "2026-10-27"} {
		if !strings.Contains(flat, want) {
			t.Fatalf("missing %q in %s", want, detailed.String())
		}
	}
	var output bytes.Buffer
	if err := renderScanJSON(&output, []audit.AuditEntry{entry}, []audit.AuditEntry{entry}, false, nil); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Packages []struct {
			ReleaseAge *struct {
				Status      string    `json:"status"`
				PublishedAt time.Time `json:"published_at"`
				Source      string    `json:"source"`
				WindowDays  int       `json:"window_days"`
				Until       time.Time `json:"until"`
			} `json:"release_age"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Packages) != 1 || decoded.Packages[0].ReleaseAge == nil {
		t.Fatalf("JSON: %s", output.String())
	}
	got := decoded.Packages[0].ReleaseAge
	if got.Status != "young" || got.Source != "npm" || got.WindowDays != 7 || !got.PublishedAt.Equal(published) || !got.Until.Equal(published.Add(7*24*time.Hour)) {
		t.Fatalf("release_age: %+v", got)
	}
	var table, csv bytes.Buffer
	if err := renderScanTable(&table, []audit.AuditEntry{entry}, []audit.AuditEntry{entry}, false); err != nil {
		t.Fatal(err)
	}
	if err := renderScanCSV(&csv, []audit.AuditEntry{entry}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(table.String(), "Release age") || strings.Contains(csv.String(), "release_age") {
		t.Fatalf("table or CSV changed: %s / %s", table.String(), csv.String())
	}
}

func TestReleaseAgeUnknownJSONHasNoDetailedNotice(t *testing.T) {
	entry := audit.AuditEntry{PURL: "pkg:maven/example/lib@1.0", Verdict: audit.VerdictReview, Analysis: &analysis.Analysis{
		ReleaseAge: &analysis.ReleaseAge{Status: analysis.ReleaseAgeUnknown, WindowDays: 7},
	}}
	var detailed bytes.Buffer
	if err := renderBoxEntry(&detailed, &entry); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(detailed.String(), "Release age:") {
		t.Fatalf("unknown notice: %s", detailed.String())
	}
	var output bytes.Buffer
	if err := renderScanJSON(&output, []audit.AuditEntry{entry}, []audit.AuditEntry{entry}, false, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"release_age"`) || !strings.Contains(output.String(), `"status": "unknown"`) {
		t.Fatalf("unknown JSON: %s", output.String())
	}
	if strings.Contains(output.String(), `"published_at"`) || strings.Contains(output.String(), `"until"`) || strings.Contains(output.String(), `"source"`) {
		t.Fatalf("unknown JSON has time/source: %s", output.String())
	}
}

func TestOldReleaseOutputIsByteIdentical(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	entry := audit.AuditEntry{PURL: "pkg:npm/old@1.0.0", Verdict: audit.VerdictOK, Analysis: &analysis.Analysis{
		OriginalPURL:   "pkg:npm/old@1.0.0",
		Package:        &analysis.Package{PURL: "pkg:npm/old@1.0.0", Ecosystem: "npm", Version: "1.0.0"},
		ReleaseHistory: &analysis.ReleaseHistory{Registry: analysis.RegistryNpm, PublishedAt: map[string]time.Time{"1.0.0": now.AddDate(-1, 0, 0)}},
	}}
	encode := func() (string, string) {
		var jsonOut, boxOut bytes.Buffer
		if err := renderScanJSON(&jsonOut, []audit.AuditEntry{entry}, []audit.AuditEntry{entry}, false, nil); err != nil {
			t.Fatal(err)
		}
		if err := renderBoxEntry(&boxOut, &entry); err != nil {
			t.Fatal(err)
		}
		return jsonOut.String(), boxOut.String()
	}
	beforeJSON, beforeBox := encode()
	entry.Analysis.ReleaseAge = entry.Analysis.EvaluateReleaseAge(now, 7*24*time.Hour)
	afterJSON, afterBox := encode()
	if beforeJSON != afterJSON || beforeBox != afterBox {
		t.Fatalf("old release output changed: JSON=%v detailed=%v", beforeJSON != afterJSON, beforeBox != afterBox)
	}
}
