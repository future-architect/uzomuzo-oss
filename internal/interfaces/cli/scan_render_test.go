package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
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

func TestRenderScanTable(t *testing.T) {
	var buf bytes.Buffer
	entries := makeTestEntries()
	if err := renderScanTable(&buf, entries, entries); err != nil {
		t.Fatalf("renderScanTable() error = %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "STATUS") {
		t.Error("table output missing STATUS header")
	}
	if !strings.Contains(output, "PURL") {
		t.Error("table output missing PURL header")
	}
	if !strings.Contains(output, "pkg:npm/express@4.18.2") {
		t.Error("table output missing express entry")
	}
	if !strings.Contains(output, "replace") {
		t.Error("table output missing replace verdict")
	}
	if !strings.Contains(output, "── Summary") {
		t.Error("table output missing summary box")
	}
	if !strings.Contains(output, "1 ok") {
		t.Error("summary missing ok count")
	}
	if !strings.Contains(output, "1 replace") {
		t.Error("summary missing replace count")
	}
	// Verdict emoji in table rows (scoped to rows containing PURLs, not summary box)
	hasEmojiRow := func(emoji, purl string) bool {
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, emoji) && strings.Contains(line, purl) {
				return true
			}
		}
		return false
	}
	if !hasEmojiRow("✅", "pkg:npm/express@4.18.2") {
		t.Error("table row for express missing OK verdict icon")
	}
	if !hasEmojiRow("🔴", "pkg:npm/request@2.88.2") {
		t.Error("table row for request missing Replace verdict icon")
	}
}

func TestRenderScanJSON(t *testing.T) {
	var buf bytes.Buffer
	entries := makeTestEntries()
	if err := renderScanJSON(&buf, entries, entries); err != nil {
		t.Fatalf("renderScanJSON() error = %v", err)
	}

	var out enrichedJSONOutput
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

func TestRenderScanCSV(t *testing.T) {
	var buf bytes.Buffer
	entries := makeTestEntries()
	if err := renderScanCSV(&buf, entries); err != nil {
		t.Fatalf("renderScanCSV() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	// Header + 3 data rows
	if len(lines) != 4 {
		t.Fatalf("got %d CSV lines, want 4 (header + 3 rows)", len(lines))
	}
	if !strings.HasPrefix(lines[0], "verdict,") {
		t.Errorf("CSV header = %q, want to start with 'verdict,'", lines[0])
	}
	if !strings.Contains(lines[0], "lifecycle") {
		t.Errorf("CSV header = %q, want to contain 'lifecycle'", lines[0])
	}
	for _, removed := range []string{"eol", "eol_reason"} {
		for _, col := range strings.Split(lines[0], ",") {
			if col == removed {
				t.Errorf("CSV header should not contain removed column %q, got header: %s", removed, lines[0])
			}
		}
	}
}

func TestRenderScanOutput_UnsupportedFormat(t *testing.T) {
	var buf bytes.Buffer
	err := renderScanOutput(&buf, nil, nil, "yaml")
	if err == nil {
		t.Error("expected error for unsupported format, got nil")
	}
}

func TestRenderScanTable_WithSourceColumn(t *testing.T) {
	entries := []domainaudit.AuditEntry{
		{
			PURL:    "https://github.com/owner/repo",
			Verdict: domainaudit.VerdictOK,
			Source:  domainaudit.SourceDirect,
		},
		{
			PURL:    "https://github.com/actions/checkout",
			Verdict: domainaudit.VerdictOK,
			Source:  domainaudit.SourceActions,
		},
		{
			PURL:    "https://github.com/some/transitive",
			Verdict: domainaudit.VerdictReview,
			Source:  domainaudit.SourceActionsTransitive,
		},
	}

	var buf bytes.Buffer
	if err := renderScanTable(&buf, entries, entries); err != nil {
		t.Fatalf("renderScanTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "SOURCE") {
		t.Error("table output missing SOURCE header when multiple sources exist")
	}
	if !strings.Contains(output, "direct") {
		t.Error("table output missing 'direct' source")
	}
	if !strings.Contains(output, "action-transitive") {
		t.Error("table output missing 'action-transitive' source")
	}
}

func TestRenderScanTable_NoSourceColumnForSingleSource(t *testing.T) {
	entries := makeTestEntries() // all SourceDirect
	var buf bytes.Buffer
	if err := renderScanTable(&buf, entries, entries); err != nil {
		t.Fatalf("renderScanTable() error = %v", err)
	}
	if strings.Contains(buf.String(), "SOURCE") {
		t.Error("table output should not contain SOURCE column when all entries have same source")
	}
}

func TestRenderScanJSON_WithSource(t *testing.T) {
	entries := []domainaudit.AuditEntry{
		{
			PURL:    "https://github.com/owner/repo",
			Verdict: domainaudit.VerdictOK,
			Source:  domainaudit.SourceDirect,
		},
		{
			PURL:    "https://github.com/actions/checkout",
			Verdict: domainaudit.VerdictOK,
			Source:  domainaudit.SourceActions,
		},
		{
			PURL:    "https://github.com/some/transitive",
			Verdict: domainaudit.VerdictReview,
			Source:  domainaudit.SourceActionsTransitive,
		},
	}

	var buf bytes.Buffer
	if err := renderScanJSON(&buf, entries, entries); err != nil {
		t.Fatalf("renderScanJSON() error = %v", err)
	}

	var out enrichedJSONOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("JSON unmarshal error = %v", err)
	}

	if len(out.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(out.Entries))
	}
	// Direct entry should have empty source (omitempty).
	if out.Entries[0].Source != "" {
		t.Errorf("direct entry source = %q, want empty", out.Entries[0].Source)
	}
	// Actions entry should have "actions" source.
	if out.Entries[1].Source != "actions" {
		t.Errorf("actions entry source = %q, want %q", out.Entries[1].Source, "actions")
	}
	// Transitive entry should have "actions-transitive" source.
	if out.Entries[2].Source != "actions-transitive" {
		t.Errorf("transitive entry source = %q, want %q", out.Entries[2].Source, "actions-transitive")
	}
}

func TestRenderScanCSV_WithSource(t *testing.T) {
	entries := []domainaudit.AuditEntry{
		{
			PURL:    "https://github.com/actions/checkout",
			Verdict: domainaudit.VerdictOK,
			Source:  domainaudit.SourceActions,
		},
	}

	var buf bytes.Buffer
	if err := renderScanCSV(&buf, entries); err != nil {
		t.Fatalf("renderScanCSV() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d CSV lines, want 2 (header + 1 row)", len(lines))
	}
	if !strings.Contains(lines[0], "source") {
		t.Error("CSV header missing 'source' column")
	}
	if !strings.Contains(lines[0], "via") {
		t.Error("CSV header missing 'via' column")
	}
	if !strings.Contains(lines[1], "actions") {
		t.Errorf("CSV row should contain 'actions', got %q", lines[1])
	}
}

func makeRelationEntries() []domainaudit.AuditEntry {
	return []domainaudit.AuditEntry{
		{
			PURL:     "pkg:npm/express@4.18.2",
			Verdict:  domainaudit.VerdictOK,
			Relation: depparser.RelationDirect,
			Analysis: &analysis.Analysis{
				AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
					analysis.LifecycleAxis: {Label: analysis.LabelActive},
				},
			},
		},
		{
			PURL:       "pkg:npm/body-parser@1.20.0",
			Verdict:    domainaudit.VerdictCaution,
			Relation:   depparser.RelationTransitive,
			ViaParents: []string{"express"},
			Analysis: &analysis.Analysis{
				AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
					analysis.LifecycleAxis: {Label: analysis.LabelStalled},
				},
			},
		},
	}
}

func TestRenderScanTable_WithRelationColumn(t *testing.T) {
	var buf bytes.Buffer
	entries := makeRelationEntries()
	if err := renderScanTable(&buf, entries, entries); err != nil {
		t.Fatalf("renderScanTable() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "RELATION") {
		t.Error("table output missing RELATION header when relation info present")
	}
	if !strings.Contains(output, "direct") {
		t.Error("table output missing 'direct' relation")
	}
	if !strings.Contains(output, "transitive (express)") {
		t.Error("table output missing 'transitive (express)' relation")
	}
}

func TestRenderScanTable_NoRelationColumnForUnknown(t *testing.T) {
	entries := makeTestEntries() // all RelationUnknown
	var buf bytes.Buffer
	if err := renderScanTable(&buf, entries, entries); err != nil {
		t.Fatalf("renderScanTable() error = %v", err)
	}
	if strings.Contains(buf.String(), "RELATION") {
		t.Error("table output should not contain RELATION column when all entries have unknown relation")
	}
}

func TestRenderScanJSON_WithRelation(t *testing.T) {
	var buf bytes.Buffer
	entries := makeRelationEntries()
	if err := renderScanJSON(&buf, entries, entries); err != nil {
		t.Fatalf("renderScanJSON() error = %v", err)
	}

	var out enrichedJSONOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("JSON unmarshal error = %v", err)
	}
	if out.Entries[0].Relation != "direct" {
		t.Errorf("direct entry relation = %q, want %q", out.Entries[0].Relation, "direct")
	}
	if out.Entries[1].Relation != "transitive" {
		t.Errorf("transitive entry relation = %q, want %q", out.Entries[1].Relation, "transitive")
	}
	if len(out.Entries[1].RelationVia) != 1 || out.Entries[1].RelationVia[0] != "express" {
		t.Errorf("transitive entry relation_via = %v, want [express]", out.Entries[1].RelationVia)
	}
}

func TestRenderScanJSON_OmitsRelationWhenUnknown(t *testing.T) {
	var buf bytes.Buffer
	entries := makeTestEntries()
	if err := renderScanJSON(&buf, entries, entries); err != nil {
		t.Fatalf("renderScanJSON() error = %v", err)
	}
	// Relation with empty string should be omitted via omitempty.
	if strings.Contains(buf.String(), `"relation"`) {
		t.Error("JSON output should omit relation field when RelationUnknown")
	}
}

func TestRenderScanCSV_WithRelation(t *testing.T) {
	var buf bytes.Buffer
	entries := makeRelationEntries()
	if err := renderScanCSV(&buf, entries); err != nil {
		t.Fatalf("renderScanCSV() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if !strings.Contains(lines[0], "relation") {
		t.Error("CSV header missing 'relation' column when relation info present")
	}
	if !strings.Contains(lines[0], "relation_via") {
		t.Error("CSV header missing 'relation_via' column when relation info present")
	}
}

func TestRenderScanCSV_NoRelationColumnForUnknown(t *testing.T) {
	var buf bytes.Buffer
	entries := makeTestEntries()
	if err := renderScanCSV(&buf, entries); err != nil {
		t.Fatalf("renderScanCSV() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if strings.Contains(lines[0], "relation") {
		t.Error("CSV header should not contain 'relation' column when all entries have unknown relation")
	}
}

func TestResolveFormat(t *testing.T) {
	tests := []struct {
		name       string
		explicit   string
		inputCount int
		want       string
		wantErr    bool
	}{
		{name: "explicit json", explicit: "json", inputCount: 1, want: "json"},
		{name: "explicit table", explicit: "table", inputCount: 1, want: "table"},
		{name: "explicit csv", explicit: "csv", inputCount: 100, want: "csv"},
		{name: "explicit detailed", explicit: "detailed", inputCount: 100, want: "detailed"},
		{name: "invalid format", explicit: "yaml", wantErr: true},
		{name: "auto 1 input", inputCount: 1, want: "detailed"},
		{name: "auto 3 inputs", inputCount: 3, want: "detailed"},
		{name: "auto 4 inputs", inputCount: 4, want: "table"},
		{name: "auto 100 inputs", inputCount: 100, want: "table"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveFormat(tt.explicit, tt.inputCount)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderScanJSON_TransitiveAdvisories(t *testing.T) {
	entries := []domainaudit.AuditEntry{
		{
			PURL:    "pkg:npm/request@2.88.2",
			Verdict: domainaudit.VerdictCaution,
			Analysis: &analysis.Analysis{
				EffectivePURL: "pkg:npm/request@2.88.2",
				Package:       &analysis.Package{Ecosystem: "npm", PURL: "pkg:npm/request", Version: "2.88.2"},
				ReleaseInfo: &analysis.ReleaseInfo{
					StableVersion: &analysis.VersionDetail{
						Version: "2.88.2",
						Advisories: []analysis.Advisory{
							{ID: "CVE-1", CVSS3Score: 9.8, Severity: "CRITICAL", Relation: analysis.AdvisoryRelationDirect},
							{ID: "CVE-2", CVSS3Score: 7.2, Severity: "HIGH", Relation: analysis.AdvisoryRelationTransitive, DependencyName: "qs@6.5.5"},
							{ID: "CVE-3", CVSS3Score: 4.3, Severity: "MEDIUM", Relation: analysis.AdvisoryRelationTransitive, DependencyName: "lodash@4.17.15"},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := renderScanJSON(&buf, entries, entries); err != nil {
		t.Fatalf("renderScanJSON() error = %v", err)
	}

	var out enrichedJSONOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("JSON unmarshal error = %v", err)
	}

	e := out.Entries[0]
	// Total (direct + transitive)
	if e.AdvisoryCount != 3 {
		t.Errorf("advisory_count = %d, want 3 (total)", e.AdvisoryCount)
	}
	// Direct
	if e.DirectAdvisoryCount != 1 {
		t.Errorf("direct_advisory_count = %d, want 1", e.DirectAdvisoryCount)
	}
	if e.MaxCVSS3Score != 9.8 {
		t.Errorf("max_cvss3_score = %f, want 9.8", e.MaxCVSS3Score)
	}
	if e.MaxAdvisorySeverity != "CRITICAL" {
		t.Errorf("max_advisory_severity = %q, want CRITICAL", e.MaxAdvisorySeverity)
	}
	// Transitive
	if e.TransitiveAdvisoryCount != 2 {
		t.Errorf("transitive_advisory_count = %d, want 2", e.TransitiveAdvisoryCount)
	}
	if e.MaxTransitiveCVSS3Score != 7.2 {
		t.Errorf("max_transitive_cvss3_score = %f, want 7.2", e.MaxTransitiveCVSS3Score)
	}
	if e.MaxTransitiveAdvisorySeverity != "HIGH" {
		t.Errorf("max_transitive_advisory_severity = %q, want HIGH", e.MaxTransitiveAdvisorySeverity)
	}
}

func TestRenderScanCSV_TransitiveAdvisories(t *testing.T) {
	entries := []domainaudit.AuditEntry{
		{
			PURL:    "pkg:npm/request@2.88.2",
			Verdict: domainaudit.VerdictCaution,
			Analysis: &analysis.Analysis{
				EffectivePURL: "pkg:npm/request@2.88.2",
				Package:       &analysis.Package{Ecosystem: "npm", PURL: "pkg:npm/request", Version: "2.88.2"},
				ReleaseInfo: &analysis.ReleaseInfo{
					StableVersion: &analysis.VersionDetail{
						Version: "2.88.2",
						Advisories: []analysis.Advisory{
							{ID: "CVE-1", CVSS3Score: 9.8, Severity: "CRITICAL", Relation: analysis.AdvisoryRelationDirect},
							{ID: "CVE-2", CVSS3Score: 7.2, Severity: "HIGH", Relation: analysis.AdvisoryRelationTransitive, DependencyName: "qs@6.5.5"},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := renderScanCSV(&buf, entries); err != nil {
		t.Fatalf("renderScanCSV() error = %v", err)
	}

	r := csv.NewReader(strings.NewReader(buf.String()))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("CSV parse error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d CSV records, want 2 (header + 1 row)", len(records))
	}

	header := records[0]
	row := records[1]

	// Build column index for lookup
	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[h] = i
	}

	// Verify transitive columns exist and have correct values
	checks := map[string]string{
		"advisory_count":                 "2",   // total: 1 direct + 1 transitive
		"max_advisory_severity":          "CRITICAL",
		"max_cvss3_score":               "9.8",
		"direct_advisory_count":          "1",
		"transitive_advisory_count":      "1",
		"max_transitive_advisory_severity": "HIGH",
		"max_transitive_cvss3_score":     "7.2",
	}
	for col, want := range checks {
		idx, ok := colIdx[col]
		if !ok {
			t.Errorf("CSV header missing column %q", col)
			continue
		}
		if got := row[idx]; got != want {
			t.Errorf("CSV column %q = %q, want %q", col, got, want)
		}
	}
}

func TestParseShowOnly(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int // expected map size, -1 for nil
		wantErr string
	}{
		{name: "empty", raw: "", want: -1},
		{name: "single", raw: "replace", want: 1},
		{name: "multiple", raw: "ok,caution", want: 2},
		{name: "case insensitive", raw: "REPLACE,OK", want: 2},
		{name: "with spaces", raw: " replace , caution ", want: 2},
		{name: "invalid", raw: "bad", wantErr: "invalid --show-only verdict"},
		{name: "partial invalid", raw: "ok,bad", wantErr: "invalid --show-only verdict"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseShowOnly(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseShowOnly(%q) error = %v, want containing %q", tt.raw, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseShowOnly(%q) unexpected error: %v", tt.raw, err)
			}
			if tt.want == -1 {
				if got != nil {
					t.Errorf("ParseShowOnly(%q) = %v, want nil", tt.raw, got)
				}
			} else if len(got) != tt.want {
				t.Errorf("ParseShowOnly(%q) returned %d verdicts, want %d", tt.raw, len(got), tt.want)
			}
		})
	}
}

func TestFilterEntriesByVerdict(t *testing.T) {
	entries := makeTestEntries() // ok, replace, review

	t.Run("nil filter returns all", func(t *testing.T) {
		got := filterEntriesByVerdict(entries, nil)
		if len(got) != len(entries) {
			t.Errorf("got %d entries, want %d", len(got), len(entries))
		}
	})

	t.Run("filter replace only", func(t *testing.T) {
		filter := map[domainaudit.Verdict]struct{}{domainaudit.VerdictReplace: {}}
		got := filterEntriesByVerdict(entries, filter)
		if len(got) != 1 {
			t.Fatalf("got %d entries, want 1", len(got))
		}
		if got[0].Verdict != domainaudit.VerdictReplace {
			t.Errorf("got verdict %q, want replace", got[0].Verdict)
		}
	})

	t.Run("filter with no matches", func(t *testing.T) {
		filter := map[domainaudit.Verdict]struct{}{domainaudit.VerdictCaution: {}}
		got := filterEntriesByVerdict(entries, filter)
		if len(got) != 0 {
			t.Errorf("got %d entries, want 0", len(got))
		}
	})
}

func TestRenderScanTable_WithShowOnly(t *testing.T) {
	allEntries := makeTestEntries()
	filter := map[domainaudit.Verdict]struct{}{domainaudit.VerdictReplace: {}}
	displayEntries := filterEntriesByVerdict(allEntries, filter)

	var buf bytes.Buffer
	if err := renderScanTable(&buf, allEntries, displayEntries); err != nil {
		t.Fatalf("renderScanTable() error = %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "pkg:npm/request@2.88.2") {
		t.Error("filtered output missing replace entry")
	}
	if strings.Contains(output, "pkg:npm/express@4.18.2") {
		t.Error("filtered output should not contain ok entry")
	}
	if !strings.Contains(output, "3 dependencies") {
		t.Error("summary should count all entries, not just filtered")
	}
	if !strings.Contains(output, "showing 1 of 3") {
		t.Error("summary should show 'showing 1 of 3'")
	}
}

func TestRenderScanJSON_WithShowOnly(t *testing.T) {
	allEntries := makeTestEntries()
	filter := map[domainaudit.Verdict]struct{}{domainaudit.VerdictReplace: {}}
	displayEntries := filterEntriesByVerdict(allEntries, filter)

	var buf bytes.Buffer
	if err := renderScanJSON(&buf, allEntries, displayEntries); err != nil {
		t.Fatalf("renderScanJSON() error = %v", err)
	}

	var out enrichedJSONOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}
	if out.Summary.Total != 3 {
		t.Errorf("summary.total = %d, want 3", out.Summary.Total)
	}
	if len(out.Entries) != 1 {
		t.Fatalf("packages count = %d, want 1", len(out.Entries))
	}
	if out.Entries[0].Verdict != "replace" {
		t.Errorf("packages[0].verdict = %q, want replace", out.Entries[0].Verdict)
	}
	if !out.Filtered {
		t.Error("filtered should be true")
	}
	if out.Shown != 1 {
		t.Errorf("shown = %d, want 1", out.Shown)
	}
}
