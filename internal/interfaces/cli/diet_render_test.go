package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	domaindiet "github.com/future-architect/uzomuzo-oss/internal/domain/diet"
)

func TestRenderDietTable_QuickWinsAlwaysShown(t *testing.T) {
	tests := []struct {
		name     string
		easyWins int
		want     string
	}{
		{
			name:     "quick wins zero",
			easyWins: 0,
			want:     "Quick wins:          0  (trivial/easy + high impact)",
		},
		{
			name:     "quick wins positive",
			easyWins: 5,
			want:     "Quick wins:          5  (trivial/easy + high impact)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &domaindiet.DietPlan{
				Summary: domaindiet.DietSummary{
					TotalDirect:  10,
					UnusedDirect: 3,
					EasyWins:     tt.easyWins,
				},
			}

			var buf bytes.Buffer
			if err := renderDietTable(&buf, plan); err != nil {
				t.Fatalf("renderDietTable returned error: %v", err)
			}

			output := buf.String()
			if !strings.Contains(output, tt.want) {
				t.Errorf("expected output to contain %q, got:\n%s", tt.want, output)
			}
		})
	}
}

// TestRenderDietJSON_AlwaysSerializesZeroValueFields pins the intentional
// removal of `omitempty` from has_vulnerabilities / vulnerability_count /
// max_cvss_score / overall_score: these keys must appear in every entry even at
// their zero value, so consumers can distinguish "absent" from "false/zero".
// Asserts on raw JSON bytes because struct unmarshalling cannot observe key presence.
func TestRenderDietJSON_AlwaysSerializesZeroValueFields(t *testing.T) {
	plan := &domaindiet.DietPlan{
		Entries: []domaindiet.DietEntry{
			{PURL: "pkg:npm/left-pad@1.3.0"}, // zero-value Health: no vulns, zero scores
		},
	}

	var buf bytes.Buffer
	if err := renderDietJSON(&buf, plan); err != nil {
		t.Fatalf("renderDietJSON() error = %v", err)
	}

	var probe struct {
		Dependencies []map[string]json.RawMessage `json:"dependencies"`
	}
	if err := json.Unmarshal(buf.Bytes(), &probe); err != nil {
		t.Fatalf("JSON unmarshal error = %v", err)
	}
	if len(probe.Dependencies) == 0 {
		t.Fatal("got 0 dependencies, want at least 1")
	}

	first := probe.Dependencies[0]
	for key, want := range map[string]string{
		"has_vulnerabilities": "false",
		"vulnerability_count": "0",
		"max_cvss_score":      "0",
		"overall_score":       "0",
	} {
		raw, ok := first[key]
		if !ok {
			t.Errorf("entry[0] missing key %q (omitempty must not be set)", key)
			continue
		}
		if got := string(raw); got != want {
			t.Errorf("entry[0].%s = %s, want %s", key, got, want)
		}
	}
}
