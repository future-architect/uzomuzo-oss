package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

// TestArchivedDisabledScanOutputs pins that the archived and disabled facts
// reach the table, JSON and CSV while the lifecycle label stays Stalled. See
// ADR-0029.
func TestArchivedDisabledScanOutputs(t *testing.T) {
	stalled := func(rs *analysis.RepoState) *analysis.Analysis {
		return &analysis.Analysis{
			RepoState:      rs,
			MaliciousState: &analysis.MaliciousState{Status: analysis.MaliciousStatusClean},
			AxisResults:    map[analysis.AssessmentAxis]*analysis.AssessmentResult{analysis.LifecycleAxis: {Label: string(analysis.LabelStalled)}},
		}
	}
	archived := audit.AuditEntry{PURL: "pkg:github/tibdex/github-app-token", Verdict: audit.VerdictReplace, Analysis: stalled(&analysis.RepoState{IsArchived: true})}
	quiet := audit.AuditEntry{PURL: "pkg:github/actions/github-script", Verdict: audit.VerdictCaution, Analysis: stalled(&analysis.RepoState{})}
	disabled := audit.AuditEntry{PURL: "pkg:github/owner/gone", Verdict: audit.VerdictCaution, Analysis: stalled(&analysis.RepoState{IsDisabled: true})}
	errored := audit.AuditEntry{PURL: "pkg:npm/unknown@1.0.0", Verdict: audit.VerdictReview, ErrorMsg: "lookup failed"}
	entries := []audit.AuditEntry{archived, quiet, disabled, errored}

	var buf bytes.Buffer
	if err := renderScanTable(&buf, entries, entries, false); err != nil {
		t.Fatal(err)
	}
	rows := map[string]string{}
	for _, line := range strings.Split(buf.String(), "\n") {
		for _, e := range entries {
			if strings.Contains(line, e.PURL) {
				rows[e.PURL] = line
			}
		}
	}
	if r := rows[archived.PURL]; !strings.Contains(r, "Stalled · archived") || strings.Contains(r, "disabled") {
		t.Errorf("archived row: %q", r)
	}
	if r := rows[quiet.PURL]; !strings.Contains(r, "Stalled") || strings.Contains(r, "·") {
		t.Errorf("quiet row has a marker: %q", r)
	}
	if r := rows[disabled.PURL]; !strings.Contains(r, "Stalled · disabled") || strings.Contains(r, "archived") {
		t.Errorf("disabled row: %q", r)
	}
	if r := rows[errored.PURL]; r == "" || strings.Contains(r, "·") {
		t.Errorf("errored row: %q", r)
	}

	buf.Reset()
	if err := renderScanJSON(&buf, entries, entries, false, nil); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Packages []struct {
			PURL      string `json:"purl"`
			Lifecycle string `json:"lifecycle"`
			Archived  *bool  `json:"archived"`
			Disabled  *bool  `json:"disabled"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Packages) != len(entries) {
		t.Fatalf("JSON: want %d packages, got %d", len(entries), len(out.Packages))
	}
	wantJSON := []struct {
		lifecycle          string
		archived, disabled bool
	}{{"Stalled", true, false}, {"Stalled", false, false}, {"Stalled", false, true}, {"—", false, false}}
	for i, w := range wantJSON {
		p := out.Packages[i]
		// Both fields are always serialized, so a missing key is a regression.
		if p.Archived == nil || p.Disabled == nil {
			t.Fatalf("JSON %s: archived/disabled not serialized: %s", p.PURL, buf.String())
		}
		if p.Lifecycle != w.lifecycle || *p.Archived != w.archived || *p.Disabled != w.disabled {
			t.Errorf("JSON %s = {%q %v %v}, want %+v", p.PURL, p.Lifecycle, *p.Archived, *p.Disabled, w)
		}
	}

	buf.Reset()
	if err := renderScanCSV(&buf, entries); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != len(entries)+1 {
		t.Fatalf("CSV: want header + %d rows, got %d", len(entries), len(records))
	}
	if len(records[0]) > maxCSVColumns {
		t.Errorf("CSV header has %d columns, maxCSVColumns is %d", len(records[0]), maxCSVColumns)
	}
	aCol, dCol, lCol := slices.Index(records[0], "archived"), slices.Index(records[0], "disabled"), slices.Index(records[0], "lifecycle")
	if aCol < 0 || dCol < 0 {
		t.Fatalf("CSV header: %v", records[0])
	}
	wantCSV := [][2]string{{"true", "false"}, {"false", "false"}, {"false", "true"}, {"false", "false"}}
	for i, w := range wantCSV {
		r := records[i+1]
		if r[aCol] != w[0] || r[dCol] != w[1] {
			t.Errorf("CSV row %d archived,disabled = %s,%s, want %s,%s", i, r[aCol], r[dCol], w[0], w[1])
		}
		if strings.Contains(r[lCol], "archived") || strings.Contains(r[lCol], "disabled") {
			t.Errorf("CSV row %d lifecycle carries a marker: %q", i, r[lCol])
		}
	}
}

// TestArchivedMarkerFollowsOtherMarkers pins that the archived marker is
// appended after a malicious or dormant-release marker instead of replacing it.
func TestArchivedMarkerFollowsOtherMarkers(t *testing.T) {
	base := func() *analysis.Analysis {
		return &analysis.Analysis{
			RepoState:   &analysis.RepoState{IsArchived: true},
			AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{analysis.LifecycleAxis: {Label: string(analysis.LabelStalled)}},
		}
	}
	dormant := base()
	dormant.MaliciousState = &analysis.MaliciousState{Status: analysis.MaliciousStatusClean}
	dormant.DormantRelease = &analysis.DormantRelease{Kind: analysis.DormantReleaseFresh, Burst: &analysis.DormantBurst{Version: "2.0.1", SilentDays: 12}}
	malicious := base()
	malicious.MaliciousState = &analysis.MaliciousState{Status: analysis.MaliciousStatusFlagged, AdvisoryID: "MAL-1"}

	for _, tt := range []struct {
		name string
		a    *analysis.Analysis
		want string
	}{
		{"dormant", dormant, "Stalled ⚠ 12d silence · archived"},
		{"malicious", malicious, "Stalled ☠ MAL-1 · archived"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := []audit.AuditEntry{{PURL: "pkg:npm/x@2.0.1", Verdict: audit.VerdictReplace, Analysis: tt.a}}
			var buf bytes.Buffer
			if err := renderScanTable(&buf, e, e, false); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("table missing %q:\n%s", tt.want, buf.String())
			}
		})
	}
}
