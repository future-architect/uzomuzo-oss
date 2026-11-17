package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/application"
	scanapp "github.com/future-architect/uzomuzo-oss/internal/application/scan"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"github.com/future-architect/uzomuzo-oss/internal/domain/scan"
)

func skippedFixture() *scanapp.TransitiveSkipped {
	return &scanapp.TransitiveSkipped{
		Count: 35,
		Via:   map[string]int{"express": 7, "fastapi-cli": 5, "a": 5, "b": 3, "c": 1},
	}
}

func TestFormatTransitiveSkipped(t *testing.T) {
	tests := []struct {
		name string
		in   *scanapp.TransitiveSkipped
		want string
	}{
		{"top three by count then name, rest folded", skippedFixture(),
			"35 transitive not evaluated (via express 7, a 5, fastapi-cli 5, +2 more; use --show-transitive)"},
		{"no parent info", &scanapp.TransitiveSkipped{Count: 4},
			"4 transitive not evaluated (use --show-transitive)"},
		{"fewer parents than the cap", &scanapp.TransitiveSkipped{Count: 2, Via: map[string]int{"x": 2}},
			"2 transitive not evaluated (via x 2; use --show-transitive)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTransitiveSkipped(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderScanOutput_TransitiveSkipped(t *testing.T) {
	opts := scanRenderOptions{TransitiveSkipped: skippedFixture()}

	for _, format := range []string{FormatTable, FormatDetailed} {
		t.Run(format+" summary line", func(t *testing.T) {
			var buf bytes.Buffer
			if err := renderScanOutput(&buf, nil, nil, format, false, nil, opts); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(buf.String(), "0 dependencies |") ||
				!strings.Contains(buf.String(), "| 35 transitive not evaluated (via express 7, a 5, fastapi-cli 5, +2 more; use --show-transitive)") {
				t.Errorf("summary hint missing:\n%s", buf.String())
			}
		})
		t.Run(format+" no hint when nothing skipped", func(t *testing.T) {
			var buf bytes.Buffer
			if err := renderScanOutput(&buf, nil, nil, format, false, nil, scanRenderOptions{}); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(buf.String(), "transitive not evaluated") {
				t.Errorf("unexpected hint:\n%s", buf.String())
			}
		})
	}

	t.Run("json includes transitive_skipped", func(t *testing.T) {
		var buf bytes.Buffer
		if err := renderScanOutput(&buf, nil, nil, FormatJSON, false, nil, opts); err != nil {
			t.Fatal(err)
		}
		var got struct {
			Skipped *struct {
				Count int            `json:"count"`
				Via   map[string]int `json:"via"`
			} `json:"transitive_skipped"`
		}
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Skipped == nil || got.Skipped.Count != 35 || got.Skipped.Via["express"] != 7 || len(got.Skipped.Via) != 5 {
			t.Errorf("transitive_skipped = %+v", got.Skipped)
		}
	})

	t.Run("json omits transitive_skipped when nothing skipped", func(t *testing.T) {
		var buf bytes.Buffer
		if err := renderScanOutput(&buf, nil, nil, FormatJSON, false, nil, scanRenderOptions{}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(buf.String(), "transitive_skipped") {
			t.Errorf("key present:\n%s", buf.String())
		}
	})

	t.Run("json via is an object even without parent info", func(t *testing.T) {
		var buf bytes.Buffer
		o := scanRenderOptions{TransitiveSkipped: &scanapp.TransitiveSkipped{Count: 2}}
		if err := renderScanOutput(&buf, nil, nil, FormatJSON, false, nil, o); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), `"via": {}`) {
			t.Errorf("want empty via object:\n%s", buf.String())
		}
	})
}

func TestWriteTransitiveSkippedNotice(t *testing.T) {
	tests := []struct {
		name string
		in   *scanapp.TransitiveSkipped
		want string
	}{
		{"nil writes nothing", nil, ""},
		{"singular", &scanapp.TransitiveSkipped{Count: 1, Via: map[string]int{"x": 1}},
			"scan: 1 transitive dependency not evaluated (via x 1; use --show-transitive to evaluate them)\n"},
		{"plural with fold", skippedFixture(),
			"scan: 35 transitive dependencies not evaluated (via express 7, a 5, fastapi-cli 5, +2 more; use --show-transitive to evaluate them)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writeTransitiveSkippedNotice(&buf, tt.in)
			if buf.String() != tt.want {
				t.Errorf("got %q, want %q", buf.String(), tt.want)
			}
		})
	}
}

// --show-only narrows the rows but the summary hint still describes the whole scan.
func TestRenderScanTable_TransitiveSkippedWithShowOnly(t *testing.T) {
	var buf bytes.Buffer
	if err := renderScanTable(&buf, nil, nil, true, skippedFixture()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "showing 0 of 0 | 35 transitive not evaluated (via express 7") {
		t.Errorf("hint missing next to the filter note:\n%s", buf.String())
	}
}

type skippedFlowParser struct{}

func (skippedFlowParser) FormatName() string { return "CycloneDX SBOM" }
func (skippedFlowParser) Parse(context.Context, []byte) ([]depparser.ParsedDependency, error) {
	return []depparser.ParsedDependency{
		{PURL: "pkg:npm/express@1", Relation: depparser.RelationDirect},
		{PURL: "pkg:npm/debug@1", Relation: depparser.RelationTransitive, ViaParents: []string{"express"}},
	}, nil
}

// captureRunOutput runs fn with os.Stdout and os.Stderr redirected to files.
func captureRunOutput(t *testing.T, fn func() error) (stdout, stderr string, err error) {
	t.Helper()
	outF, e := os.CreateTemp(t.TempDir(), "stdout")
	if e != nil {
		t.Fatal(e)
	}
	errF, e := os.CreateTemp(t.TempDir(), "stderr")
	if e != nil {
		t.Fatal(e)
	}
	prevOut, prevErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outF, errF
	t.Cleanup(func() { os.Stdout, os.Stderr = prevOut, prevErr; _ = outF.Close(); _ = errF.Close() })
	err = fn()
	os.Stdout, os.Stderr = prevOut, prevErr
	o, e := os.ReadFile(outF.Name())
	if e != nil {
		t.Fatal(e)
	}
	r, e := os.ReadFile(errF.Name())
	if e != nil {
		t.Fatal(e)
	}
	return string(o), string(r), err
}

// Drives the real runScanSBOM path so the wiring from the scan result to the
// renderers and to stderr is covered end to end.
func TestRunScanSBOM_TransitiveSkippedReachesOutput(t *testing.T) {
	svc, err := scanapp.NewService(application.NewAnalysisService(&sarifInputSource{}))
	if err != nil {
		t.Fatal(err)
	}
	sbom := filepath.Join(t.TempDir(), "sbom.json")
	if err := os.WriteFile(sbom, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	parsers := map[string]depparser.DependencyParser{"sbom": skippedFlowParser{}}

	tests := []struct {
		name           string
		format         string
		showTransitive bool
		wantStdout     []string
		wantStderr     string
	}{
		{"table default", FormatTable, false,
			[]string{"| 1 transitive not evaluated (via express 1; use --show-transitive)"},
			"scan: 1 transitive dependency not evaluated (via express 1; use --show-transitive to evaluate them)\n"},
		{"json default", FormatJSON, false,
			[]string{`"transitive_skipped": {`, `"count": 1`, `"express": 1`},
			"scan: 1 transitive dependency not evaluated (via express 1; use --show-transitive to evaluate them)\n"},
		{"table with --show-transitive", FormatTable, true, nil, ""},
		{"json with --show-transitive", FormatJSON, true, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := ScanOptions{Format: tt.format, SBOMPath: sbom, ShowTransitive: tt.showTransitive}
			stdout, stderr, err := captureRunOutput(t, func() error {
				return runScanSBOM(context.Background(), svc, opts, parsers, scan.FailPolicy{})
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.wantStdout {
				if !strings.Contains(stdout, w) {
					t.Errorf("stdout missing %q:\n%s", w, stdout)
				}
			}
			if tt.wantStdout == nil && (strings.Contains(stdout, "transitive not evaluated") || strings.Contains(stdout, "transitive_skipped")) {
				t.Errorf("unexpected hint on stdout:\n%s", stdout)
			}
			if got := skippedNoticeLine(stderr); got != tt.wantStderr {
				t.Errorf("stderr notice = %q, want %q", got, tt.wantStderr)
			}
		})
	}
}

// skippedNoticeLine returns the "scan: ... transitive ..." line (with newline) from stderr, or "".
func skippedNoticeLine(stderr string) string {
	for _, line := range strings.SplitAfter(stderr, "\n") {
		if strings.HasPrefix(line, "scan: ") && strings.Contains(line, "transitive") {
			return line
		}
	}
	return ""
}
