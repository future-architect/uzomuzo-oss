package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

func TestMaliciousScanOutputs(t *testing.T) {
	flagged := audit.AuditEntry{PURL: "pkg:npm/chalk@5.6.1", Verdict: audit.VerdictReplace, Analysis: &analysis.Analysis{MaliciousState: &analysis.MaliciousState{Status: analysis.MaliciousStatusFlagged, Malicious: true, AdvisoryID: "MAL-1", Summary: "Bad release", Scope: analysis.MaliciousScopeVersion}, AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{analysis.LifecycleAxis: {Label: string(analysis.LabelActive)}}}}
	unknown := audit.AuditEntry{PURL: "pkg:npm/other@1.0.0", Verdict: audit.VerdictReview, Analysis: &analysis.Analysis{MaliciousState: &analysis.MaliciousState{Status: analysis.MaliciousStatusLookupFailed}}}
	entries := []audit.AuditEntry{flagged, unknown}
	var buf bytes.Buffer
	if err := renderScanJSON(&buf, entries, entries, false); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Packages []map[string]json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if string(out.Packages[0]["malicious"]) == "" || !bytes.Contains(out.Packages[0]["malicious"], []byte("MAL-1")) {
		t.Fatalf("flagged JSON: %s", buf.String())
	}
	if string(out.Packages[1]["malicious_check"]) != `"unknown"` {
		t.Fatalf("unknown JSON: %s", buf.String())
	}
	buf.Reset()
	if err := renderScanCSV(&buf, entries); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if records[0][len(records[0])-1] != "malicious_advisory" || records[1][len(records[1])-1] != "MAL-1" {
		t.Fatalf("CSV: %v", records)
	}
	buf.Reset()
	if err := renderScanTable(&buf, entries, entries, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Active ☠ MAL-1") {
		t.Fatalf("table: %s", buf.String())
	}
	buf.Reset()
	flagged.Analysis.Error = errors.New("not found")
	if err := renderBoxEntry(&buf, &flagged); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Malicious version: MAL-1") {
		t.Fatalf("box: %s", buf.String())
	}
}
