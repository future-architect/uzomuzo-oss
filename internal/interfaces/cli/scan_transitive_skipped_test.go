package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	scanapp "github.com/future-architect/uzomuzo-oss/internal/application/scan"
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
	var buf bytes.Buffer
	writeTransitiveSkippedNotice(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("nil wrote %q", buf.String())
	}
	writeTransitiveSkippedNotice(&buf, &scanapp.TransitiveSkipped{Count: 1, Via: map[string]int{"x": 1}})
	if want := "scan: 1 transitive dependency not evaluated (via x 1; use --show-transitive to evaluate them)\n"; buf.String() != want {
		t.Errorf("singular: got %q, want %q", buf.String(), want)
	}
	buf.Reset()
	writeTransitiveSkippedNotice(&buf, skippedFixture())
	want := "scan: 35 transitive dependencies not evaluated (via express 7, a 5, fastapi-cli 5, +2 more; use --show-transitive to evaluate them)\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}
