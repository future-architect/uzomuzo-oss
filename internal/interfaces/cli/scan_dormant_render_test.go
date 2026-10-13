package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
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
				Kind          string    `json:"kind"`
				PublishedAt   time.Time `json:"published_at"`
				SilentDays    int       `json:"silent_days"`
				BurstVersions []string  `json:"burst_versions"`
				Lines         []string  `json:"lines"`
				Removed       bool      `json:"removed"`
				Until         time.Time `json:"until"`
			} `json:"dormant_release"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Packages) != 1 {
		t.Fatalf("JSON: want 1 package, got %d: %s", len(out.Packages), buf.String())
	}
	got := out.Packages[0]
	if got.Lifecycle != "Stalled" || got.DormantRelease == nil {
		t.Fatalf("JSON: %s", buf.String())
	}
	d := got.DormantRelease
	if d.Kind != "fresh" || !d.PublishedAt.Equal(published) || d.SilentDays != 400 ||
		!slices.Equal(d.BurstVersions, []string{"2.0.1"}) || !slices.Equal(d.Lines, []string{"2"}) ||
		d.Removed || !d.Until.Equal(published.Add(7*24*time.Hour)) {
		t.Fatalf("JSON dormant_release = %+v", *d)
	}

	buf.Reset()
	if err := renderScanCSV(&buf, entries); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("CSV: want header + 1 row, got %d: %v", len(records), records)
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

	// When an advisory also names the version, the table cell shows the
	// advisory, not the marker.
	both := marked
	flagged := *marked.Analysis
	flagged.MaliciousState = &analysis.MaliciousState{Status: analysis.MaliciousStatusFlagged, AdvisoryID: "MAL-1", Scope: analysis.MaliciousScopeVersion}
	both.Analysis = &flagged
	buf.Reset()
	if err := renderScanTable(&buf, []audit.AuditEntry{both}, []audit.AuditEntry{both}, false); err != nil {
		t.Fatal(err)
	}
	if table := buf.String(); !strings.Contains(table, "Stalled ☠ MAL-1") || strings.Contains(table, "silence") {
		t.Fatalf("table with both: %s", table)
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

	// A deps.dev failure still carries the marker, since the release history
	// comes from the registry: the error box shows it next to the error.
	failed := marked
	errored := *marked.Analysis
	errored.Error = errors.New("deps.dev lookup failed")
	errored.AxisResults = nil
	failed.Analysis = &errored
	failed.ErrorMsg = "deps.dev lookup failed"
	buf.Reset()
	if err := renderBoxEntry(&buf, &failed); err != nil {
		t.Fatal(err)
	}
	errBox := buf.String()
	flat = strings.Join(strings.Fields(strings.ReplaceAll(errBox, "│", " ")), " ")
	for _, want := range []string{"Release after silence", "400 days", "error: deps.dev lookup failed"} {
		if !strings.Contains(flat, want) {
			t.Errorf("error box missing %q:\n%s", want, errBox)
		}
	}
}
