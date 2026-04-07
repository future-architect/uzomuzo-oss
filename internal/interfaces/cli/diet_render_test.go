package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

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
				AnalyzedAt: time.Now(),
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
