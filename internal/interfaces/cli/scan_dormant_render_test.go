package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

// TestDormantReleaseScanOutputs pins that the dormant-release marker reaches
// every output format while the lifecycle label stays Stalled. See ADR-0028.
func TestDormantReleaseScanOutputs(t *testing.T) {
	published := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	marked := audit.AuditEntry{PURL: "pkg:npm/quiet@2.0.1", Verdict: audit.VerdictReplace, Analysis: &analysis.Analysis{
		MaliciousState: &analysis.MaliciousState{Status: analysis.MaliciousStatusClean},
		DormantRelease: &analysis.DormantRelease{
			Kind:  analysis.DormantReleaseFresh,
			Burst: &analysis.DormantBurst{Version: "2.0.1", PublishedAt: published, SilentDays: 400, Versions: []string{"2.0.1"}, Lines: []string{"2"}},
			Until: published.Add(7 * 24 * time.Hour),
		},
		AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{analysis.LifecycleAxis: {Label: string(analysis.LabelStalled), Reason: "No release for a while"}},
	}}
	entries := []audit.AuditEntry{marked}

	var buf bytes.Buffer
	if err := renderScanJSON(&buf, entries, entries, false); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Packages []struct {
			Lifecycle      string `json:"lifecycle"`
			DormantRelease *struct {
				Kind       string `json:"kind"`
				SilentDays int    `json:"silent_days"`
			} `json:"dormant_release"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if got := out.Packages[0]; got.Lifecycle != "Stalled" || got.DormantRelease == nil || got.DormantRelease.Kind != "fresh" || got.DormantRelease.SilentDays != 400 {
		t.Fatalf("JSON: %s", buf.String())
	}

	buf.Reset()
	if err := renderScanCSV(&buf, entries); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	col := slices.Index(records[0], "dormant_release")
	if col < 0 || records[1][col] != "fresh" {
		t.Fatalf("CSV: %v", records)
	}

	buf.Reset()
	if err := renderScanTable(&buf, entries, entries, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Stalled ⚠ 400d silence") {
		t.Fatalf("table: %s", buf.String())
	}

	buf.Reset()
	if err := renderBoxEntry(&buf, &marked); err != nil {
		t.Fatal(err)
	}
	box := buf.String()
	// The notice is word-wrapped inside the box; compare it as one line.
	flat := strings.Join(strings.Fields(strings.ReplaceAll(box, "│", " ")), " ")
	for _, want := range []string{"Release after silence", "400 days", "No OSV malicious-package", "2026-10-08"} {
		if !strings.Contains(flat, want) {
			t.Errorf("box missing %q:\n%s", want, box)
		}
	}
}
