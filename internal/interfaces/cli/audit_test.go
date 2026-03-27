package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

func makeTestEntries() []domainaudit.AuditEntry {
	return []domainaudit.AuditEntry{
		{
			PURL:    "pkg:npm/express@4.18.2",
			Verdict: domainaudit.VerdictOK,
			Analysis: &analysis.Analysis{
				AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
					analysis.LifecycleAxis: {Label: analysis.LabelActive},
				},
			},
		},
		{
			PURL:    "pkg:npm/request@2.88.2",
			Verdict: domainaudit.VerdictReplace,
			Analysis: &analysis.Analysis{
				EOL: analysis.EOLStatus{State: analysis.EOLEndOfLife},
			},
		},
		{
			PURL:     "pkg:npm/unknown@1.0.0",
			Verdict:  domainaudit.VerdictReview,
			Analysis: nil,
		},
	}
}

func TestRenderTable(t *testing.T) {
	var buf bytes.Buffer
	entries := makeTestEntries()
	if err := renderTable(&buf, entries); err != nil {
		t.Fatalf("renderTable() error = %v", err)
	}

	output := buf.String()

	// Check header
	if !strings.Contains(output, "VERDICT") {
		t.Error("table output missing VERDICT header")
	}
	if !strings.Contains(output, "PURL") {
		t.Error("table output missing PURL header")
	}

	// Check entries present
	if !strings.Contains(output, "pkg:npm/express@4.18.2") {
		t.Error("table output missing express entry")
	}
	if !strings.Contains(output, "replace") {
		t.Error("table output missing replace verdict")
	}

	// Check summary
	if !strings.Contains(output, "Summary:") {
		t.Error("table output missing summary")
	}
	if !strings.Contains(output, "1 ok") {
		t.Error("summary missing ok count")
	}
	if !strings.Contains(output, "1 replace") {
		t.Error("summary missing replace count")
	}
}

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	entries := makeTestEntries()
	if err := renderJSON(&buf, entries); err != nil {
		t.Fatalf("renderJSON() error = %v", err)
	}

	var out jsonOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("JSON unmarshal error = %v", err)
	}

	if out.Summary.Total != 3 {
		t.Errorf("summary.total = %d, want 3", out.Summary.Total)
	}
	if out.Summary.OK != 1 {
		t.Errorf("summary.ok = %d, want 1", out.Summary.OK)
	}
	if out.Summary.Replace != 1 {
		t.Errorf("summary.replace = %d, want 1", out.Summary.Replace)
	}
	if len(out.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(out.Entries))
	}
	if out.Entries[0].Verdict != "ok" {
		t.Errorf("entries[0].verdict = %q, want %q", out.Entries[0].Verdict, "ok")
	}
}

func TestRenderCSV(t *testing.T) {
	var buf bytes.Buffer
	entries := makeTestEntries()
	if err := renderCSV(&buf, entries); err != nil {
		t.Fatalf("renderCSV() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	// Header + 3 data rows
	if len(lines) != 4 {
		t.Fatalf("got %d CSV lines, want 4 (header + 3 rows)", len(lines))
	}
	if !strings.HasPrefix(lines[0], "verdict,") {
		t.Errorf("CSV header = %q, want to start with 'verdict,'", lines[0])
	}
}

func TestRenderAuditOutput_UnsupportedFormat(t *testing.T) {
	var buf bytes.Buffer
	err := renderAuditOutput(&buf, nil, "yaml")
	if err == nil {
		t.Error("expected error for unsupported format, got nil")
	}
}
