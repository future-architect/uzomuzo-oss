package cli

import (
	"bytes"
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
		{"zero quick wins", 0, "Quick wins:          0"},
		{"nonzero quick wins", 3, "Quick wins:          3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &domaindiet.DietPlan{
				Summary: domaindiet.DietSummary{
					TotalDirect: 10,
					EasyWins:    tt.easyWins,
				},
			}
			var buf bytes.Buffer
			if err := renderDietTable(&buf, plan); err != nil {
				t.Fatalf("renderDietTable() error: %v", err)
			}
			output := buf.String()
			if !strings.Contains(output, tt.want) {
				t.Errorf("expected output to contain %q, got:\n%s", tt.want, output)
			}
		})
	}
}
