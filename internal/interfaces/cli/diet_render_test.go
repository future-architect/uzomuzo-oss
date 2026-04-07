package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	domaindiet "github.com/future-architect/uzomuzo-oss/internal/domain/diet"
)

func TestRenderDietTable_QuickWinsShownWhenZero(t *testing.T) {
	plan := &domaindiet.DietPlan{
		Summary: domaindiet.DietSummary{
			TotalDirect:     5,
			TotalTransitive: 20,
			EasyWins:        0,
		},
		SBOMPath:   "testdata/bom.json",
		AnalyzedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	var buf bytes.Buffer
	if err := renderDietTable(&buf, plan); err != nil {
		t.Fatalf("renderDietTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Quick wins:") {
		t.Error("table output missing 'Quick wins:' line when EasyWins == 0")
	}
	if !strings.Contains(output, "Quick wins:          0") {
		t.Error("table output should show 'Quick wins: 0' when EasyWins == 0")
	}
}

func TestRenderDietTable_QuickWinsShownWhenNonZero(t *testing.T) {
	plan := &domaindiet.DietPlan{
		Summary: domaindiet.DietSummary{
			TotalDirect:     5,
			TotalTransitive: 20,
			EasyWins:        3,
		},
		SBOMPath:   "testdata/bom.json",
		AnalyzedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	var buf bytes.Buffer
	if err := renderDietTable(&buf, plan); err != nil {
		t.Fatalf("renderDietTable() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Quick wins:          3") {
		t.Error("table output should show 'Quick wins: 3' when EasyWins == 3")
	}
}
